package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG4_AccessibilityApplyRefetchEmptyPlan(t *testing.T) {
	withTempCacheDir(t)
	var created atomic.Bool
	base := fullCoverageHandler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/apps/APP1/accessibilityDeclarations" && !created.Load() {
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/accessibilityDeclarations" {
			created.Store(true)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"type":"accessibilityDeclarations","id":"ACCESS1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsVoiceover":false}}}`))
			return
		}
		base.ServeHTTP(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()
	c := fixtureClient(t, srv)
	opts := FetchOpts{Version: "1.0", Platform: "IOS"}
	live, err := Fetch(ctx, c, "com.example.app", opts)
	if err != nil {
		t.Fatal(err)
	}
	desired := &config.State{Spec: config.StateSpec{AccessibilityDeclarations: &config.AccessibilityDeclarationsSpec{Families: map[string]config.AccessibilityDeclarationSpec{"IPHONE": {SupportsVoiceover: new(false)}}}}}
	if ds := config.ValidateWriteIntent("state.yaml", desired, live); len(ds) > 0 {
		t.Fatalf("intent: %+v", ds)
	}
	changes := plan.Diff(desired, live)
	if len(changes) != 1 {
		t.Fatalf("changes=%+v", changes)
	}
	result, err := Apply(ctx, c, changes, ApplyOpts{Context: defaultApplyCtx(), Confirm: true})
	if err != nil || len(result.Applied) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	live, err = Fetch(ctx, c, "com.example.app", opts)
	if err != nil {
		t.Fatal(err)
	}
	if changes := plan.Diff(desired, live); len(changes) != 0 {
		t.Fatalf("residual=%+v", changes)
	}
}
