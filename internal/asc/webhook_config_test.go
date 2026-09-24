package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF7C_WebhookCreatePayloadAndPoisonedErrorAreSecretSafe(t *testing.T) {
	secret := "plain-poison-secret"
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprintf(w, `{"errors":[{"status":"422","title":%q,"detail":%q,"source":{"pointer":%q},"meta":{"echo":%q}}]}`, secret, secret, secret, secret)
	}))
	defer srv.Close()
	enabled := true
	name, endpoint := "Builds", "https://hooks.example.test/build"
	events := []string{"BUILD_UPLOAD_STATE_UPDATED"}
	_, err := CreateAppWebhook(context.Background(), fixtureClient(t, srv), "A1", WebhookWriteAttributes{
		Name: &name, URL: &endpoint, EventTypes: &events, Enabled: &enabled, Secret: &secret,
	})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error=%v", err)
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil || strings.Contains(string(encoded), secret) {
		t.Fatalf("json error=%s marshal=%v", encoded, marshalErr)
	}
	data, _ := request["data"].(map[string]any)
	attrs, _ := data["attributes"].(map[string]any)
	rels, _ := data["relationships"].(map[string]any)
	app, _ := rels["app"].(map[string]any)
	ref, _ := app["data"].(map[string]any)
	if attrs["secret"] != secret || ref["type"] != "apps" || ref["id"] != "A1" {
		t.Fatalf("request missing expected attributes/relationship")
	}
}

func TestF7C_WebhookReadPagesAndRedactsUnsafeExistingURL(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "next" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W2","attributes":{"name":"Two","enabled":false,"eventTypes":[],"url":"https://second.example.test/path"}}]}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"One","enabled":true,"eventTypes":["BUILD_UPLOAD_STATE_UPDATED"],"url":"https://user:private@hooks.example.test/path?token=hidden#frag"}}],"links":{"next":%q}}`, srv.URL+"/v1/apps/A1/webhooks?cursor=next")
	}))
	defer srv.Close()
	rows, err := ListAppWebhooks(context.Background(), fixtureClient(t, srv), "A1")
	if err != nil || len(rows) != 2 || rows[0].Endpoint != "https://hooks.example.test" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	encoded, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "hidden") || strings.Contains(string(encoded), "/path") {
		t.Fatalf("unsafe JSON=%s err=%v", encoded, err)
	}
}

func TestF7C_WebhookURLValidation(t *testing.T) {
	for _, value := range []string{"http://example.test", "https://user:pw@example.test", "https://example.test/?token=x", "https://example.test/#x"} {
		if err := ValidateWebhookURL(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if err := ValidateWebhookURL("https://hooks.example.test/path"); err != nil {
		t.Fatal(err)
	}
	if got := SanitizedWebhookOrigin("https://user:secret@[::1]:8443/private?token=x"); got != "https://[::1]:8443" {
		t.Fatalf("sanitized IPv6 origin=%q", got)
	}
	if got := SanitizedWebhookOrigin("https://example.test:9443/path"); got != "https://example.test:9443" {
		t.Fatalf("sanitized origin=%q", got)
	}
}

func TestF7C_WebhookReadErrorDropsPoisonedServerText(t *testing.T) {
	secret := "poison-read-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"errors":[{"title":%q,"detail":%q,"source":{"pointer":%q},"meta":{"echo":%q}}]}`, secret, secret, secret, secret)
	}))
	defer srv.Close()
	_, err := ListAppWebhooks(context.Background(), fixtureClient(t, srv), "A1")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error=%v", err)
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil || strings.Contains(string(encoded), secret) {
		t.Fatalf("JSON error=%s marshal=%v", encoded, marshalErr)
	}
}
