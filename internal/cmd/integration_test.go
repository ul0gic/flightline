//go:build integration

package cmd

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/ul0gic/skipper/internal/asc"
	"github.com/ul0gic/skipper/internal/auth"
)

// requireCreds skips the test when ASC creds aren't in the environment.
// Integration runs are explicit (`go test -tags=integration ./...`); the
// skip keeps a no-creds invocation green so CI matrices that lack secrets
// don't fail spuriously.
func requireCreds(t *testing.T) (keyID, issuerID, keyPath string) {
	t.Helper()
	keyID = os.Getenv("APP_STORE_CONNECT_KEY_ID")
	issuerID = os.Getenv("APP_STORE_CONNECT_ISSUER_ID")
	if keyID == "" || issuerID == "" {
		t.Skip("APP_STORE_CONNECT_KEY_ID / APP_STORE_CONNECT_ISSUER_ID not set; skipping integration test")
	}
	var err error
	keyPath, err = auth.KeyPath(keyID)
	if err != nil {
		t.Fatalf("KeyPath: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Skipf(".p8 not found at %s; skipping (set APP_STORE_CONNECT_KEY_PATH to override)", keyPath)
	}
	return keyID, issuerID, keyPath
}

func TestIntegration_Whoami(t *testing.T) {
	keyID, issuerID, keyPath := requireCreds(t)
	c, err := asc.New(asc.Options{KeyID: keyID, IssuerID: issuerID, KeyPath: keyPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	type minAppAttrs struct{}
	if _, err := asc.Get[asc.Collection[minAppAttrs]](
		context.Background(),
		c,
		"/v1/apps",
		url.Values{"limit": {"1"}},
	); err != nil {
		t.Fatalf("whoami probe: %v", err)
	}
}

func TestIntegration_AppsList(t *testing.T) {
	keyID, issuerID, keyPath := requireCreds(t)
	c, err := asc.New(asc.Options{KeyID: keyID, IssuerID: issuerID, KeyPath: keyPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	apps, err := collectApps(context.Background(), c, "/v1/apps", url.Values{"limit": {"50"}}, 5)
	if err != nil {
		t.Fatalf("collectApps: %v", err)
	}
	if len(apps) == 0 {
		t.Log("warning: account has no apps; integration smoke is technically green but uninformative")
	}
	for _, a := range apps {
		if a.Attributes.BundleID == "" {
			t.Errorf("app %q has empty bundleId", a.ID)
		}
	}
}
