package asc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestKey creates an ephemeral P-256 key as PKCS8 PEM at mode 0600.
// The key never leaves the test process.
func writeTestKey(t *testing.T) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "AuthKey_TESTABCDEF.p8")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	return path
}

// newTestClient returns a Client whose http.Client and baseURL point at the
// supplied test server, plus an ephemeral .p8 so auth.Mint succeeds.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	keyPath := writeTestKey(t)
	c, err := New(Options{
		KeyID:      "TESTABCDEF",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: srv.Client(),
		UserAgent:  "skipper-test/1.0",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.baseURL = srv.URL
	return c
}

func TestNew_RequiredFields(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"missing KeyID", Options{IssuerID: "i", KeyPath: "/tmp/x"}, "KeyID"},
		{"missing IssuerID", Options{KeyID: "k", KeyPath: "/tmp/x"}, "IssuerID"},
		{"missing KeyPath", Options{KeyID: "k", IssuerID: "i"}, "KeyPath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New(%+v) err = %v, want substring %q", tt.opts, err, tt.want)
			}
		})
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	c, err := New(Options{KeyID: "k", IssuerID: "i", KeyPath: "/tmp/x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.userAgent != "skipper/dev" {
		t.Errorf("UserAgent default = %q, want skipper/dev", c.userAgent)
	}
	if c.http == nil {
		t.Error("HTTPClient should default to non-nil")
	}
	if c.baseURL != baseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, baseURL)
	}
}

type appAttrs struct {
	BundleID string `json:"bundleId"`
	Name     string `json:"name"`
}

func TestGet_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header is present and well-formed.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ey") {
			t.Errorf("Authorization = %q, want Bearer ey...", auth)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		if r.URL.Path != "/v1/apps" {
			t.Errorf("path = %q, want /v1/apps", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("limit = %q, want 10", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "apps", "id": "1", "attributes": map[string]any{"bundleId": "com.example.app", "name": "Example"}},
			},
			"links": map[string]any{"self": "https://api.appstoreconnect.apple.com/v1/apps"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/apps", url.Values{"limit": {"10"}})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("data len = %d, want 1", len(got.Data))
	}
	if got.Data[0].Attributes.BundleID != "com.example.app" {
		t.Errorf("bundleId = %q", got.Data[0].Attributes.BundleID)
	}
}

func TestGet_Error401Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{
				{"id": "abc", "status": "401", "code": "NOT_AUTHORIZED", "title": "Authentication credentials are missing or invalid.", "detail": "JWT verification failed."},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/apps", nil)
	if err == nil {
		t.Fatal("Get: want error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError via errors.As", err)
	}
	if apiErr.HTTPStatus != 401 {
		t.Errorf("HTTPStatus = %d, want 401", apiErr.HTTPStatus)
	}
	if got := err.Error(); !strings.Contains(got, "NOT_AUTHORIZED") {
		t.Errorf("err.Error() = %q, missing code", got)
	}
}

func TestGet_Error403MapsForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"code":"FORBIDDEN","title":"Forbidden","detail":"Insufficient scope","status":"403"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/apps", nil)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestGet_NonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream timeout"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/apps", nil)
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.HTTPStatus != 502 {
		t.Errorf("HTTPStatus = %d, want 502", apiErr.HTTPStatus)
	}
}

func TestDelete_2xxIsSuccess(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Delete(context.Background(), "/v1/apps/123", nil); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !called {
		t.Error("server not called")
	}
}

// TestDeleteWithBody_SendsBody confirms the body-bearing DELETE variant
// transmits the JSON body intact and accepts a 204 as success. Used by
// Apple's "delete to-many relationship" endpoints
// (e.g. /v1/betaGroups/{id}/relationships/betaTesters).
func TestDeleteWithBody_SendsBody(t *testing.T) {
	called := false
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	body := map[string]any{
		"data": []map[string]any{
			{"type": "betaTesters", "id": "T1"},
		},
	}
	if err := c.DeleteWithBody(context.Background(), "/v1/betaGroups/BG-1/relationships/betaTesters", nil, body); err != nil {
		t.Fatalf("DeleteWithBody: %v", err)
	}
	if !called {
		t.Fatal("server not called")
	}
	gotData, ok := gotBody["data"].([]any)
	if !ok || len(gotData) != 1 {
		t.Fatalf("body data = %v, want one-element slice", gotBody)
	}
}

// TestDeleteWithBody_4xxFails confirms typed-error surfacing on a non-2xx
// response (regression guard against silently dropping Apple's errors[]).
func TestDeleteWithBody_4xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"code":"BAD","title":"bad linkage"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.DeleteWithBody(context.Background(), "/v1/betaGroups/BG-1/relationships/betaTesters", nil, map[string]any{})
	if err == nil {
		t.Fatal("DeleteWithBody: want error on 422, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusUnprocessableEntity {
		t.Errorf("HTTPStatus = %d, want 422", apiErr.HTTPStatus)
	}
}

func TestBuildURL_RejectsForeignHost(t *testing.T) {
	c := &Client{baseURL: "https://api.appstoreconnect.apple.com"}
	_, err := c.buildURL("https://attacker.example.com/v1/apps", nil)
	if err == nil || !strings.Contains(err.Error(), "foreign host") {
		t.Errorf("buildURL = %v, want foreign host rejection", err)
	}
}

func TestBuildURL_AbsoluteSameHostOK(t *testing.T) {
	c := &Client{baseURL: "https://api.appstoreconnect.apple.com"}
	got, err := c.buildURL("https://api.appstoreconnect.apple.com/v1/apps?cursor=abc", nil)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	if !strings.Contains(got, "/v1/apps") {
		t.Errorf("got = %q", got)
	}
}

func TestPost_SendsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		if got["foo"] != "bar" {
			t.Errorf("body.foo = %v", got["foo"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"apps","id":"1"},"links":{"self":""}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type emptyAttrs struct{}
	_, err := Post[Single[emptyAttrs]](context.Background(), c, "/v1/apps", nil, map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
}
