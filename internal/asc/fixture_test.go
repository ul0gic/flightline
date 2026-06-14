package asc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// FixtureRoute names a golden JSON file to serve. Status defaults to 200 when zero.
type FixtureRoute struct {
	File   string
	Status int
}

// fixtureServer keys routes by "<METHOD> <path>"; query strings are ignored
// because matching by query is too brittle for cred-redacted fixtures.
func fixtureServer(t *testing.T, routes map[string]FixtureRoute) *httptest.Server {
	t.Helper()
	captured := make(map[string]FixtureRoute, len(routes))
	for k, v := range routes {
		captured[k] = v
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		route, ok := captured[key]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			body := `{"errors":[{"id":"fixture-no-route","status":"404","code":"FIXTURE_NO_ROUTE","title":"Fixture has no route registered for this request","detail":"` + key + `"}]}`
			_, _ = w.Write([]byte(body))
			return
		}

		body, err := readFixture(route.File)
		if err != nil {
			t.Errorf("fixture %s: %v", route.File, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		status := route.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// readFixture loads testdata/golden/<name>.json, rejecting path traversal so
// an untrusted route map can't read arbitrary files.
func readFixture(name string) ([]byte, error) {
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return nil, &fixtureError{name: name, reason: "path traversal"}
	}
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}
	path := filepath.Join("testdata", "golden", name)
	return os.ReadFile(path)
}

type fixtureError struct {
	name   string
	reason string
}

func (e *fixtureError) Error() string {
	return "fixture " + e.name + ": " + e.reason
}

// fixtureClient wires a production-shaped Client to srv: real auth.Mint runs
// per request, only the base URL diverges.
func fixtureClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	keyPath := writeFixtureKey(t)
	c, err := New(Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: srv.Client(),
		UserAgent:  "flightline-test/1.0",
	})
	if err != nil {
		t.Fatalf("fixtureClient: New: %v", err)
	}
	c.SetBaseURL(srv.URL)
	return c
}

// writeFixtureKey generates a fresh ephemeral P-256 PKCS8 PEM at mode 0600: never a real .p8.
func writeFixtureKey(t *testing.T) string {
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
	path := filepath.Join(t.TempDir(), "AuthKey_TEST123ABC.p8")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	return path
}
