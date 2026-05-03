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

// fixtureRoute describes one entry in the cmd-level fixture-server route
// table: which JSON file under ../asc/testdata/golden/ to serve, with what
// HTTP status. Status defaults to 200 when zero.
//
// This mirrors internal/asc/fixture_test.go's helper but lives in the cmd
// package so cmd-level tests can replay the same golden corpus through the
// production-shaped client (with Options.BaseURL pointing at httptest).
type fixtureRoute struct {
	File   string
	Status int
}

// startFixtureServer spins an httptest.Server backed by a route table.
// Routes are matched against METHOD + URL.Path (query strings ignored).
//
// Unknown routes return 404 with a body that names the offending route, so
// failures pinpoint typos rather than masquerading as request bugs.
//
// Calls t.Cleanup to close the server; callers do NOT defer Close.
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

// readGoldenFixture loads a golden JSON file from internal/asc/testdata/golden/.
// Shared corpus across asc and cmd packages — single source of truth.
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

// fixtureASCClient builds a production-shaped asc.Client wired to the
// supplied fixture server via Options.BaseURL. Each call writes an ephemeral
// P-256 PKCS8 PEM at mode 0600 in t.TempDir() (never a real .p8) so the JWT
// minter runs unmodified.
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

// writeEphemeralKey generates a fresh P-256 PKCS8 key into t.TempDir at mode
// 0600 and returns the path. Never use a real .p8 in tests.
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
