package cmd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

// Status defaults to 200 when zero.
type fixtureRoute struct {
	File   string
	Status int
}

// Routes match on METHOD + URL.Path (query ignored); unknown routes 404 with
// the offending route in the body. Closes via t.Cleanup: callers do NOT defer.
func startFixtureServer(t *testing.T, routes map[string]fixtureRoute) *httptest.Server {
	t.Helper()
	captured := make(map[string]fixtureRoute, len(routes))
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
		body, err := readGoldenFixture(route.File)
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

// Golden corpus is shared with the asc package: single source of truth.
func readGoldenFixture(name string) ([]byte, error) {
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return nil, errors.New("fixture: path traversal: " + name)
	}
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}
	path := filepath.Join("..", "asc", "testdata", "golden", name)
	return os.ReadFile(path) // #nosec G304 -- name is sanitized above
}

// Writes an ephemeral P-256 key (never a real .p8) so the JWT minter runs unmodified.
func fixtureASCClient(t *testing.T, srv *httptest.Server) *asc.Client {
	t.Helper()
	keyPath := writeEphemeralKey(t)
	c, err := asc.New(asc.Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
		UserAgent:  "flightline-test/1.0",
	})
	if err != nil {
		t.Fatalf("fixtureASCClient: New: %v", err)
	}
	return c
}

func writeEphemeralKey(t *testing.T) string {
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
