package state

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

func TestG3_InvalidPlanHasNoRequestsOrCheckpoint(t *testing.T) {
	withTempCacheDir(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	changes := []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/version/copyright", To: "Valid"},
		{Op: plan.OpUpdate, Path: "/spec/metadata/locales/en-US/unknown", To: "Invalid"},
	}
	for _, dryRun := range []bool{true, false} {
		result, err := Apply(context.Background(), fixtureClient(t, srv), changes, ApplyOpts{Context: defaultApplyCtx(), Confirm: true, DryRun: dryRun})
		if !errors.Is(err, ErrUnmappedChange) || result == nil || len(result.Errors) != 1 || len(result.Applied) != 0 || len(result.Planned) != 0 {
			t.Fatalf("dryRun=%v result=%+v err=%v", dryRun, result, err)
		}
	}
	if calls != 0 {
		t.Fatalf("requests=%d", calls)
	}
	path, err := applyCheckpointPath(defaultApplyCtx())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkpoint stat=%v", err)
	}
}
