package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG2_FailedParentSkipsDependentChild(t *testing.T) {
	withTempCacheDir(t)
	var childReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}]}`))
		case "/v2/inAppPurchases":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"status":"403","code":"FORBIDDEN","title":"Forbidden"}]}`))
		case "/v1/apps/APP1/inAppPurchasesV2":
			childReads.Add(1)
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	name := "Unlock"
	base := "/spec/iap/products/com.example.unlock"
	changes := []plan.Change{
		{Op: plan.OpCreate, Resource: "iap.com.example.unlock", Path: base, To: config.IAPProduct{Type: "NON_CONSUMABLE", Name: &name}},
		{Op: plan.OpCreate, Resource: "iap.com.example.unlock.loc.en-US", Path: base + "/localizations/en-US", To: config.IAPLocalization{Name: &name}},
	}
	result, err := Apply(context.Background(), fixtureClient(t, srv), changes, ApplyOpts{Context: defaultApplyCtx(), Confirm: true})
	if err == nil || result == nil {
		t.Fatalf("expected parent failure result, got %+v, %v", result, err)
	}
	if len(result.Errors) != 1 || len(result.Skipped) != 1 || result.Skipped[0].Path != changes[1].Path {
		t.Fatalf("dependent child not explicitly skipped: %+v", result)
	}
	if got := childReads.Load(); got != 0 {
		t.Fatalf("failed parent allowed %d child resolution reads", got)
	}
}

func TestG2_FailedBuildBlocksExportEvenWithReversedInput(t *testing.T) {
	withTempCacheDir(t)
	var exportReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}]}`))
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","platform":"IOS"}}]}`))
		case "/v1/builds":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/v1/appStoreVersions/VER1/build":
			exportReads.Add(1)
			_, _ = w.Write([]byte(`{"data":null}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	changes := []plan.Change{
		{Op: plan.OpUpdate, Resource: "exportCompliance", Path: "/spec/exportCompliance/usesNonExemptEncryption", To: false},
		{Op: plan.OpUpdate, Resource: "build", Path: "/spec/build/number", To: "99"},
	}
	result, err := Apply(context.Background(), fixtureClient(t, srv), changes, ApplyOpts{Context: defaultApplyCtx(), Confirm: true})
	if err == nil || result == nil {
		t.Fatalf("expected build failure, got %+v, %v", result, err)
	}
	if len(result.Errors) != 1 || len(result.Skipped) != 1 || result.Skipped[0].Path != changes[0].Path {
		t.Fatalf("export not skipped after failed prerequisite: %+v", result)
	}
	if exportReads.Load() != 0 {
		t.Fatal("export attempted against old build")
	}
}

func TestG2_LegacyCheckpointRejectedBeforeRequests(t *testing.T) {
	withTempCacheDir(t)
	actx := defaultApplyCtx()
	cp := applyCheckpoint{SchemaVersion: 2, BundleID: actx.BundleID, Version: actx.Version, Platform: actx.Platform, PlanDigest: "legacy"}
	if err := persistApplyCheckpoint(actx, cp); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer srv.Close()
	_, err := Apply(context.Background(), fixtureClient(t, srv), nil, ApplyOpts{Context: actx, Confirm: true, Resume: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported schemaVersion 2") {
		t.Fatalf("legacy checkpoint accepted: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("legacy checkpoint caused API calls")
	}
}
