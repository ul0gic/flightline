package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestF7C_WebhookSecretInputRejectsUnsafeFilesAndMutualSources(t *testing.T) {
	file := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(file, []byte("my-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readWebhookSecret("", file); err == nil {
		t.Fatal("accepted group-readable secret")
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := readWebhookSecret("", file)
	if err != nil || value != "my-secret" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), webhookSecretMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWebhookSecret("", file); err == nil {
		t.Fatal("accepted oversized secret file")
	}
	link := filepath.Join(t.TempDir(), "secret-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readWebhookSecret("", link); err == nil {
		t.Fatal("accepted secret symlink")
	}
	if _, err := readWebhookSecret("env:HOOK_SECRET", file); err == nil {
		t.Fatal("accepted two secret sources")
	}
	if err := os.Setenv("HOOK_SECRET_F7C", "env-secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("HOOK_SECRET_F7C") })
	value, err = readWebhookSecret("env:HOOK_SECRET_F7C", "")
	if err != nil || value != "env-secret" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestF7C_WebhookCreateOutputHidesSecretAndSendsCanonicalPayload(t *testing.T) {
	var request map[string]any
	srv := webhookCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/webhooks":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprint(w, `{"data":{"type":"webhooks","id":"W1"}}`)
		case "/v1/apps/A1/webhooks":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"Builds","enabled":true,"eventTypes":["BUILD_UPLOAD_STATE_UPDATED"],"url":"https://hooks.example.test/build"}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "secret")
	secret := "plain-secret-F7C"
	if err := os.WriteFile(file, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var flags webhookFlags
	bindWebhookFlags(cmd, &flags)
	if err := cmd.Flags().Set("enabled", "true"); err != nil {
		t.Fatal(err)
	}
	flags.name, flags.endpoint, flags.events, flags.secretFile, flags.confirm = "Builds", "https://hooks.example.test/build", []string{"BUILD_UPLOAD_STATE_UPDATED"}, file, true
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runWebhooksCreate(cmd, fixtureASCClient(t, srv), "com.example.app", flags, "json"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secret) || strings.Contains(output.String(), `"url"`) {
		t.Fatalf("secret or raw URL leaked: %s", output.String())
	}
	data, _ := request["data"].(map[string]any)
	attrs, _ := data["attributes"].(map[string]any)
	if attrs["secret"] != secret || attrs["url"] != flags.endpoint {
		t.Fatalf("create request incomplete")
	}
}

func TestF7C_WebhookReadAndUnchangedUpdateMakeNoActions(t *testing.T) {
	actions := 0
	srv := webhookCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			actions++
		}
		if r.URL.Path == "/v1/apps/A1/webhooks" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"Builds","enabled":true,"eventTypes":["BUILD_UPLOAD_STATE_UPDATED"],"url":"https://hooks.example.test/build"}}]}`)
			return
		}
		http.NotFound(w, r)
	})
	defer srv.Close()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var flags webhookFlags
	bindWebhookFlags(cmd, &flags)
	flags.confirm = true
	var output bytes.Buffer
	cmd.SetOut(&output)
	c := fixtureASCClient(t, srv)
	if err := runWebhooksList(cmd, c, "com.example.app", "json"); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runWebhooksUpdate(cmd, c, "com.example.app", "W1", flags, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"noOp": true`) || actions != 0 {
		t.Fatalf("output=%s actions=%d", output.String(), actions)
	}
}

func TestF7C_WebhookRotationErrorNeverExposesPoisonedSecret(t *testing.T) {
	secret := "rotation-plain-secret-F7C"
	if err := os.Setenv("HOOK_ROTATION_F7C", secret); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("HOOK_ROTATION_F7C") })
	srv := webhookCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/A1/webhooks":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"Builds","enabled":true,"eventTypes":["BUILD_UPLOAD_STATE_UPDATED"],"url":"https://hooks.example.test/build"}}]}`)
		case "/v1/webhooks/W1":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprintf(w, `{"errors":[{"status":"422","title":%q,"detail":%q,"source":{"pointer":%q},"meta":{"echo":%q}}]}`, secret, secret, secret, secret)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var flags webhookFlags
	bindWebhookFlags(cmd, &flags)
	if err := cmd.Flags().Set("secret-ref", "env:HOOK_ROTATION_F7C"); err != nil {
		t.Fatal(err)
	}
	flags.confirm = true
	err := runWebhooksUpdate(cmd, fixtureASCClient(t, srv), "com.example.app", "W1", flags, "json")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe error=%v", err)
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil || strings.Contains(string(encoded), secret) {
		t.Fatalf("unsafe JSON error=%s marshal=%v", encoded, marshalErr)
	}
}

func TestF7C_WebhookGetRejectsForeignIDBeforeDetailRead(t *testing.T) {
	foreignReads := 0
	srv := webhookCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/apps/A1/webhooks" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"Builds","enabled":true,"eventTypes":[],"url":"https://hooks.example.test"}}]}`)
			return
		}
		foreignReads++
		http.NotFound(w, r)
	})
	defer srv.Close()
	cmd := newWebhooksCommand()
	cmd.SetContext(context.Background())
	err := runWebhooksGet(cmd, fixtureASCClient(t, srv), "com.example.app", "FOREIGN", "json")
	if err == nil || foreignReads != 0 {
		t.Fatalf("err=%v foreignReads=%d", err, foreignReads)
	}
}

func webhookCommandFixture(t *testing.T, next http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1":
			_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}}`)
		default:
			next(w, r)
		}
	}))
}
