package state

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

// fixtureClient wires an asc.Client to an httptest server with an
// ephemeral P-256 key (never a real .p8). Mirrors the helper in
// internal/cmd/helpers_test.go.
func fixtureClient(t *testing.T, srv *httptest.Server) *asc.Client {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "AuthKey_TEST123ABC.p8")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	c, err := asc.New(asc.Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestFetch_VersionAndAgeRating — the happy path: app exists, version
// exists, age rating populated, build attached. Ensures the wire
// projection emits the schema-shaped fields.
func TestFetch_VersionAndAgeRating(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/apps") && r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/apps/APP1/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","platform":"IOS","copyright":"© 2026","releaseType":"MANUAL"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = w.Write([]byte(`{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`))
		case r.URL.Path == "/v1/appInfos/AINFO1/ageRatingDeclaration":
			_, _ = w.Write([]byte(`{"data":{"type":"ageRatingDeclarations","id":"AR1","attributes":{"violenceCartoonOrFantasy":"NONE","gambling":false}}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1/build":
			_, _ = w.Write([]byte(`{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42","usesNonExemptEncryption":false}}}`))
		default:
			http.Error(w, "unhandled "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := fixtureClient(t, srv)
	got, err := Fetch(context.Background(), c, "com.example.app", FetchOpts{Version: "1.0"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Metadata.BundleID != "com.example.app" {
		t.Errorf("bundleId = %q", got.Metadata.BundleID)
	}
	if got.Spec.Version == nil || got.Spec.Version.Copyright == nil || *got.Spec.Version.Copyright != "© 2026" {
		t.Errorf("copyright projection failed: %+v", got.Spec.Version)
	}
	if got.Spec.AgeRating == nil || got.Spec.AgeRating.CartoonOrFantasyViolence == nil ||
		*got.Spec.AgeRating.CartoonOrFantasyViolence != "NONE" {
		t.Errorf("age rating projection failed: %+v", got.Spec.AgeRating)
	}
	if got.Spec.ExportCompliance == nil || got.Spec.ExportCompliance.UsesNonExemptEncryption == nil ||
		*got.Spec.ExportCompliance.UsesNonExemptEncryption != false {
		t.Errorf("export compliance projection failed: %+v", got.Spec.ExportCompliance)
	}
}

// TestFetch_AppNotFound — ASC returns an empty app collection; Fetch
// emits a typed error naming the bundleId.
func TestFetch_AppNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/apps" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := fixtureClient(t, srv)
	_, err := Fetch(context.Background(), c, "com.example.missing", FetchOpts{})
	if err == nil {
		t.Fatal("expected error for missing app")
	}
	if !strings.Contains(err.Error(), "com.example.missing") {
		t.Errorf("error doesn't name bundleId: %v", err)
	}
}

// TestFetch_RequiredArgs — defensive guards.
func TestFetch_RequiredArgs(t *testing.T) {
	if _, err := Fetch(context.Background(), nil, "x", FetchOpts{}); err == nil {
		t.Error("expected error for nil client")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if _, err := Fetch(context.Background(), c, "", FetchOpts{}); err == nil {
		t.Error("expected error for empty bundleId")
	}
}

// silenceURLLint quiets the unused-import warning when net/url isn't
// referenced after a refactor — kept for intent.
var _ = url.Values{}
