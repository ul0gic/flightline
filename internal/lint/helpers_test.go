package lint

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

// newTestClient wires an asc.Client to srv. Separate from internal/cmd/helpers_test.go because lint must not import cmd.
// Uses an ephemeral P-256 key at 0600 so the JWT minter runs unmodified.
func newTestClient(t *testing.T, srv *httptest.Server) *asc.Client {
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
	keyPath := filepath.Join(t.TempDir(), "AuthKey_TEST123ABC.p8")
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	c, err := asc.New(asc.Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
		UserAgent:  "flightline-lint-test/1.0",
	})
	if err != nil {
		t.Fatalf("asc.New: %v", err)
	}
	return c
}
