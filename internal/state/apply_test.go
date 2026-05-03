package state

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

// withTempCacheDir reroutes os.UserCacheDir into a temp dir for the
// duration of the test by setting XDG_CACHE_HOME (Linux) and HOME.
// macOS UserCacheDir reads $HOME/Library/Caches; setting both works
// on both platforms.
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

// TestApply_EmptyChanges — no changes, no calls.
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

// TestApply_VersionCopyrightOneCall — one change → resolveAppID + list
// versions + PATCH version. Three calls total; the last is the PATCH.
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

// TestApply_ResumeSkipsApplied — pre-seed a checkpoint matching the
// change, run Apply with Resume; the change must be skipped and zero
// PATCHes issued.
func TestApply_ResumeSkipsApplied(t *testing.T) {
	withTempCacheDir(t)
	var patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			atomic.AddInt32(&patches, 1)
		}
	}))
	defer srv.Close()

	cp := applyCheckpoint{
		SchemaVersion: applyCheckpointSchemaVersion,
		BundleID:      "com.example.app",
		Applied: []checkpointKey{{
			Resource: "version",
			Path:     "/spec/version/copyright",
			ToJSON:   `"© 2026"`,
		}},
	}
	buf, _ := json.MarshalIndent(cp, "", "  ")
	dir, err := applyCheckpointPath("com.example.app")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dir, buf, 0o600); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	c := fixtureClient(t, srv)
	changes := []plan.Change{{
		Op: plan.OpUpdate, Resource: "version", Path: "/spec/version/copyright",
		From: "old", To: "© 2026",
	}}
	res, err := Apply(context.Background(), c, changes, ApplyOpts{
		Context: defaultApplyCtx(), Confirm: true, Resume: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Skipped) != 1 || len(res.Applied) != 0 {
		t.Errorf("expected 1 skipped 0 applied; got %+v", res)
	}
	if atomic.LoadInt32(&patches) != 0 {
		t.Errorf("expected 0 PATCHes on resume; got %d", patches)
	}
}

// TestApply_DryRunIssuesNoCalls — DryRun records intent but never
// hits the wire.
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

// TestApply_UnmappedSurfacesError — a path the dispatch table doesn't
// recognise must surface as ErrUnmappedChange. /spec/futureSurface/foo
// is intentionally outside every covered prefix.
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

// TestSchemaToWireAgeRating — sanity check the rename map for a few
// representative entries.
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
