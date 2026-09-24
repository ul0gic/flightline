package state

import (
	"context"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF7A_PhasedReleaseChangeRejectsReadonlyBeforeWrite(t *testing.T) {
	trueValue, active, paused, day := true, "ACTIVE", "PAUSED", 1
	ch := plan.Change{Op: plan.OpUpdate, Path: "/spec/version/phasedRelease", From: config.PhasedReleaseSpec{Enabled: &trueValue, State: &active, CurrentDayNumber: &day}, To: config.PhasedReleaseSpec{Enabled: &trueValue, State: &paused}}
	if err := ValidatePhasedReleaseChange(ch); err == nil {
		t.Fatal("readonly removal accepted")
	}
}

func TestF7A_PhasedReleaseReplayAtTargetDoesNotPatch(t *testing.T) {
	trueValue, active, paused := true, "ACTIVE", "PAUSED"
	var patches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"A1"}]}`))
		case "/v1/apps/A1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"V1","attributes":{"versionString":"1.0","platform":"IOS","appVersionState":"READY_FOR_DISTRIBUTION"}}]}`))
		case "/v1/appStoreVersions/V1/appStoreVersionPhasedRelease":
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionPhasedReleases","id":"PR1","attributes":{"phasedReleaseState":"PAUSED"}}}`))
		case "/v1/appStoreVersionPhasedReleases/PR1":
			patches++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	ch := plan.Change{Op: plan.OpUpdate, Path: "/spec/version/phasedRelease", From: config.PhasedReleaseSpec{Enabled: &trueValue, State: &active}, To: config.PhasedReleaseSpec{Enabled: &trueValue, State: &paused}}
	if err := applyPhasedReleaseChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, ch); err != nil || patches != 0 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}

func TestF7A_FetchPhasedReleaseTreatsOnlyTyped404AsAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	t.Cleanup(srv.Close)
	got, err := FetchPhasedRelease(context.Background(), fixtureClient(t, srv), "V1")
	if err != nil || got != nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
