package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF6C_ScreenshotOrderCommandReplacesOnlyFreshCompleteMembership(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"app-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/app-1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"version-1","attributes":{"versionString":"1.0","platform":"IOS"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appStoreVersions/version-1/appStoreVersionLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersionLocalizations","id":"loc-1","attributes":{"locale":"en-US"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appStoreVersionLocalizations/loc-1/appScreenshotSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshotSets","id":"set-1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appScreenshotSets/set-1/appScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"shot-2"},{"type":"appScreenshots","id":"shot-1"}],"links":{}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshotSets/set-1/relationships/appScreenshots":
			patches++
			var body struct {
				Data []struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PATCH: %v", err)
			}
			if got := []string{body.Data[0].ID, body.Data[1].ID}; !reflect.DeepEqual(got, []string{"shot-1", "shot-2"}) {
				t.Errorf("PATCH membership = %v", got)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	command := newScreenshotOrderCommand()
	command.SetContext(context.Background())
	var output bytes.Buffer
	command.SetOut(&output)
	input := screenshotOrderInput{
		version: "1.0", platform: "IOS", locale: "en-US", deviceSet: "APP_IPHONE_67", screenshotIDs: []string{"shot-1", "shot-2"},
	}
	if err := runScreenshotOrderWithClient(command, []string{"com.example.app"}, fixtureASCClient(t, srv), input, "json"); err != nil {
		t.Fatalf("runScreenshotOrderWithClient: %v", err)
	}
	if patches != 1 || !strings.Contains(output.String(), `"changed": true`) {
		t.Fatalf("patches=%d output=%s", patches, output.String())
	}
}

func TestF6C_ScreenshotOrderRejectsForeignOrPartialIDBeforePatch(t *testing.T) {
	members := []asc.AppScreenshot{{ID: "shot-1"}, {ID: "shot-2"}}
	if err := validateScreenshotOrderMembership([]string{"shot-1"}, members); err == nil {
		t.Fatal("partial order accepted")
	}
	if err := validateScreenshotOrderMembership([]string{"shot-1", "foreign"}, members); err == nil {
		t.Fatal("foreign screenshot accepted")
	}
	if err := validateScreenshotOrderMembership([]string{"shot-1", "shot-1"}, members); err == nil {
		t.Fatal("duplicate screenshot accepted")
	}
}

func TestF6C_ScreenshotOrderConstructorIsUnregisteredAndHasBothTargets(t *testing.T) {
	cmd := newScreenshotOrderCommand()
	if cmd.Name() != "reorder" || cmd.Parent() != nil {
		t.Fatalf("command registration = name=%q parent=%v", cmd.Name(), cmd.Parent())
	}
	for _, name := range []string{"version", "locale", "device-set", "custom-product-page", "custom-product-page-version", "screenshot"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
}
