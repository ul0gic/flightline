package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// withTempCacheDir reroutes os.UserCacheDir into a temp dir. Sets both XDG_CACHE_HOME
// (Linux) and HOME ($HOME/Library/Caches on macOS) so it works cross-platform.
func withTempCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func defaultApplyCtx() ApplyContext {
	return ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}
}

func TestApply_RequiresConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	_, err := Apply(context.Background(), c, nil, ApplyOpts{Context: defaultApplyCtx()})
	if err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Errorf("expected confirm-required error; got %v", err)
	}
}

func TestApply_RequiresBundleID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	_, err := Apply(context.Background(), c, nil, ApplyOpts{Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "BundleID") {
		t.Errorf("expected BundleID-required error; got %v", err)
	}
}

func TestApply_EmptyChanges(t *testing.T) {
	withTempCacheDir(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	res, err := Apply(context.Background(), c, nil, ApplyOpts{Context: defaultApplyCtx(), Confirm: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) != 0 || len(res.Errors) != 0 {
		t.Errorf("expected empty result; got %+v", res)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("expected zero API calls; got %d", calls)
	}
}

func TestApply_VersionCopyrightOneCall(t *testing.T) {
	withTempCacheDir(t)
	var patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","copyright":"old"}}],"links":{}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "© 2026") {
				t.Errorf("PATCH body doesn't contain new copyright: %s", body)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersions","id":"VER1","attributes":{"copyright":"© 2026"}}}`))
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := fixtureClient(t, srv)
	changes := []plan.Change{{
		Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright",
		From: "old", To: "© 2026",
	}}
	res, err := Apply(context.Background(), c, changes, ApplyOpts{
		Context: defaultApplyCtx(), Confirm: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) != 1 {
		t.Errorf("Applied = %d, want 1; res=%+v", len(res.Applied), res)
	}
	if atomic.LoadInt32(&patches) != 1 {
		t.Errorf("patches = %d, want 1", patches)
	}
}

func seedApplyCheckpoint(t *testing.T, actx ApplyContext, original, applied []plan.Change) string {
	t.Helper()
	digest, err := checkpointDigest(checkpointKeys(original))
	if err != nil {
		t.Fatalf("checkpoint digest: %v", err)
	}
	cp := applyCheckpoint{
		SchemaVersion: applyCheckpointSchemaVersion,
		BundleID:      actx.BundleID,
		Version:       actx.Version,
		Platform:      actx.Platform,
		PlanDigest:    digest,
		Applied:       checkpointKeys(applied),
	}
	if err := persistApplyCheckpoint(actx, cp); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	path, err := applyCheckpointPath(actx)
	if err != nil {
		t.Fatalf("checkpoint path: %v", err)
	}
	return path
}

func copyrightFixtureServer(t *testing.T, version string, patches *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":%q}}],"links":{}}`, version)
		case r.URL.Path == "/v1/appStoreVersions/VER1" && r.Method == http.MethodPatch:
			atomic.AddInt32(patches, 1)
			_, _ = io.WriteString(w, `{"data":{"type":"appStoreVersions","id":"VER1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

func TestApply_ResumeAppliesResidualPlanAndCleansCheckpoint(t *testing.T) {
	withTempCacheDir(t)
	var patches int32
	srv := copyrightFixtureServer(t, "1.0", &patches)
	defer srv.Close()

	applied := plan.Change{
		Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/releaseType",
		From: "MANUAL", To: "AFTER_APPROVAL",
	}
	residual := plan.Change{
		Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright",
		From: "old", To: "© 2026",
	}
	actx := defaultApplyCtx()
	checkpoint := seedApplyCheckpoint(t, actx, []plan.Change{applied, residual}, []plan.Change{applied})

	c := fixtureClient(t, srv)
	res, err := Apply(context.Background(), c, []plan.Change{residual}, ApplyOpts{
		Context: actx, Confirm: true, Resume: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Skipped) != 0 || len(res.Applied) != 1 {
		t.Errorf("expected residual change applied; got %+v", res)
	}
	if atomic.LoadInt32(&patches) != 1 {
		t.Errorf("expected 1 PATCH on resume; got %d", patches)
	}
	if _, err := os.Stat(checkpoint); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("successful apply left checkpoint behind: %v", err)
	}
}

func TestApply_ResumeReappliesTargetAfterPortalDrift(t *testing.T) {
	withTempCacheDir(t)
	var patches int32
	srv := copyrightFixtureServer(t, "1.0", &patches)
	defer srv.Close()

	change := plan.Change{
		Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright",
		From: "drifted", To: "© 2026",
	}
	actx := defaultApplyCtx()
	seedApplyCheckpoint(t, actx, []plan.Change{change}, []plan.Change{change})

	res, err := Apply(context.Background(), fixtureClient(t, srv), []plan.Change{change}, ApplyOpts{
		Context: actx, Confirm: true, Resume: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) != 1 || atomic.LoadInt32(&patches) != 1 {
		t.Errorf("drifted target was not reapplied: result=%+v patches=%d", res, patches)
	}
}

func TestApply_ResumeRejectsPlanMismatchBeforeDispatch(t *testing.T) {
	withTempCacheDir(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()

	original := plan.Change{Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright", To: "© 2026"}
	current := plan.Change{Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright", To: "different"}
	actx := defaultApplyCtx()
	seedApplyCheckpoint(t, actx, []plan.Change{original}, nil)

	_, err := Apply(context.Background(), fixtureClient(t, srv), []plan.Change{current}, ApplyOpts{
		Context: actx, Confirm: true, Resume: true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected plan mismatch; got %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("plan mismatch made %d API calls", calls)
	}
}

func TestApply_CheckpointIsolatedAcrossVersions(t *testing.T) {
	withTempCacheDir(t)
	var patches int32
	srv := copyrightFixtureServer(t, "1.1", &patches)
	defer srv.Close()

	change := plan.Change{Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright", To: "© 2026"}
	oldCtx := defaultApplyCtx()
	seedApplyCheckpoint(t, oldCtx, []plan.Change{change}, []plan.Change{change})
	newCtx := oldCtx
	newCtx.Version = "1.1"

	res, err := Apply(context.Background(), fixtureClient(t, srv), []plan.Change{change}, ApplyOpts{
		Context: newCtx, Confirm: true, Resume: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) != 1 || atomic.LoadInt32(&patches) != 1 {
		t.Errorf("old version checkpoint affected new version: result=%+v patches=%d", res, patches)
	}
}

func TestApply_SurfacesCheckpointPersistenceFailure(t *testing.T) {
	withTempCacheDir(t)
	var patches int32
	srv := copyrightFixtureServer(t, "1.0", &patches)
	defer srv.Close()

	actx := defaultApplyCtx()
	path, err := applyCheckpointPath(actx)
	if err != nil {
		t.Fatalf("checkpoint path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(path)), 0o700); err != nil {
		t.Fatalf("mkdir checkpoint parent: %v", err)
	}
	if err := os.WriteFile(filepath.Dir(path), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block checkpoint directory: %v", err)
	}
	change := plan.Change{Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright", To: "© 2026"}

	res, err := Apply(context.Background(), fixtureClient(t, srv), []plan.Change{change}, ApplyOpts{
		Context: actx, Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "checkpoint persistence failed") {
		t.Fatalf("expected persistence error; got %v", err)
	}
	if len(res.Applied) != 1 || atomic.LoadInt32(&patches) != 1 {
		t.Errorf("write result not preserved on checkpoint failure: result=%+v patches=%d", res, patches)
	}
}

func TestApply_DryRunIssuesNoCalls(t *testing.T) {
	withTempCacheDir(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()

	c := fixtureClient(t, srv)
	changes := []plan.Change{{
		Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright",
		From: "old", To: "© 2026",
	}}
	res, err := Apply(context.Background(), c, changes, ApplyOpts{
		Context: defaultApplyCtx(), DryRun: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) != 1 {
		t.Errorf("expected 1 applied (dry-run); got %+v", res)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("expected 0 calls in dry-run; got %d", calls)
	}
}

func TestApply_ContinuesPastFailedChange(t *testing.T) {
	withTempCacheDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case "/v1/appStoreVersions/VER1":
			_, _ = io.WriteString(w, `{"data":{"type":"appStoreVersions","id":"VER1"}}`)
		default:
			http.Error(w, "unhandled "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := fixtureClient(t, srv)
	changes := []plan.Change{
		{Op: plan.OpUpdate, Resource: "futureSurface", Path: "/spec/futureSurface/foo", To: "x"},
		{Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright", To: "2026 Corp"},
	}
	res, err := Apply(context.Background(), c, changes, ApplyOpts{
		Context: defaultApplyCtx(), Confirm: true,
	})
	if err == nil {
		t.Fatal("expected summary error when a change fails")
	}
	if len(res.Errors) != 1 {
		t.Errorf("errors = %d, want 1: %+v", len(res.Errors), res.Errors)
	}
	if len(res.Applied) != 1 {
		t.Errorf("applied = %d, want 1 (later change must still run): %+v", len(res.Applied), res.Applied)
	}
}

func TestApply_UnmappedSurfacesError(t *testing.T) {
	withTempCacheDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	c := fixtureClient(t, srv)
	changes := []plan.Change{{
		Op: plan.OpUpdate, Resource: "futureSurface", Path: "/spec/futureSurface/foo",
		To: "x",
	}}
	res, err := Apply(context.Background(), c, changes, ApplyOpts{
		Context: defaultApplyCtx(), Confirm: true,
	})
	if err == nil {
		t.Fatal("expected error for unmapped change")
	}
	if !errors.Is(err, ErrUnmappedChange) {
		t.Errorf("expected errors.Is(err, ErrUnmappedChange); got %v", err)
	}
	if len(res.Errors) != 1 {
		t.Errorf("expected 1 error; got %+v", res)
	}
}

func TestSchemaToWireAgeRating(t *testing.T) {
	cases := map[string]string{
		"/spec/ageRating/cartoonOrFantasyViolence": "violenceCartoonOrFantasy",
		"/spec/ageRating/contestsAndGambling":      "contests",
		"/spec/ageRating/gambling":                 "gambling",
	}
	for in, want := range cases {
		got, err := schemaToWireAgeRating(in)
		if err != nil {
			t.Errorf("%s: err %v", in, err)
		}
		if got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
	if _, err := schemaToWireAgeRating("/spec/ageRating/nonsense"); err == nil {
		t.Error("expected error on unknown leaf")
	}
}

func TestCheckpointKeys_IncludeAssetContentIdentity(t *testing.T) {
	change := plan.Change{
		Resource: "screenshots.en-US.APP_IPHONE_69",
		Path:     "/spec/screenshots/locales/en-US/APP_IPHONE_69",
		To: []config.ScreenshotFile{{
			Path: "same.png", SourceFileChecksum: "first",
		}},
	}
	first := checkpointKeys([]plan.Change{change})[0]
	change.To = []config.ScreenshotFile{{Path: "same.png", SourceFileChecksum: "second"}}
	second := checkpointKeys([]plan.Change{change})[0]
	if first == second {
		t.Fatal("checkpoint key must change when asset bytes change at the same path")
	}
}
