package state

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG7_PhasedPauseConvergesOnDistributedVersion(t *testing.T) {
	withTempCacheDir(t)
	var paused atomic.Bool
	var writes atomic.Int32
	srv := httptest.NewServer(phasedGateHandler(t, &paused, &writes))
	defer srv.Close()
	c := fixtureClient(t, srv)
	ctx := context.Background()
	opts := FetchOpts{Version: "1.0", Platform: "IOS", RequireEditable: true, AllowPhasedRelease: true}
	before, err := Fetch(ctx, c, "com.example.app", opts)
	if err != nil {
		t.Fatal(err)
	}
	desired := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{PhasedRelease: &config.PhasedReleaseSpec{State: new("PAUSED")}}}}
	if diagnostics := config.ValidateWriteIntent("", desired, before); len(diagnostics) != 0 {
		t.Fatalf("intent=%+v", diagnostics)
	}
	changes := plan.Diff(desired, before)
	if len(changes) != 1 || len(ValidateVersionChanges(before, changes)) != 0 {
		t.Fatalf("changes=%+v", changes)
	}
	applyOpts := ApplyOpts{Confirm: true, Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}}
	if _, err := Apply(ctx, c, changes, applyOpts); err != nil {
		t.Fatal(err)
	}
	after, err := Fetch(ctx, c, "com.example.app", opts)
	if err != nil {
		t.Fatal(err)
	}
	residual := plan.Diff(desired, after)
	if len(residual) != 0 || writes.Load() != 1 {
		t.Fatalf("residual=%+v writes=%d", residual, writes.Load())
	}
	if _, err := Apply(ctx, c, residual, applyOpts); err != nil || writes.Load() != 1 {
		t.Fatalf("second apply err=%v writes=%d", err, writes.Load())
	}
}

func phasedGateHandler(t *testing.T, paused *atomic.Bool, writes *atomic.Int32) http.Handler {
	t.Helper()
	base := fullCoverageHandler(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		if r.URL.Path == "/v1/apps/APP1/appStoreVersions" {
			_, _ = w.Write([]byte(`{"data":[{"id":"VER1","type":"appStoreVersions","attributes":{"versionString":"1.0","platform":"IOS","appVersionState":"READY_FOR_DISTRIBUTION"}}]}`))
			return
		}
		if r.URL.Path == "/v1/appStoreVersionPhasedReleases/PH1" && r.Method == http.MethodPatch {
			var body struct {
				Data struct {
					Attributes struct {
						State string `json:"phasedReleaseState"`
					} `json:"attributes"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Data.Attributes.State != "PAUSED" {
				t.Errorf("invalid transition body=%+v err=%v", body, err)
			}
			paused.Store(true)
		} else if r.URL.Path != "/v1/appStoreVersions/VER1/appStoreVersionPhasedRelease" {
			base.ServeHTTP(w, r)
			return
		}
		status := "ACTIVE"
		if paused.Load() {
			status = "PAUSED"
		}
		_, _ = w.Write([]byte(`{"data":{"id":"PH1","type":"appStoreVersionPhasedReleases","attributes":{"phasedReleaseState":"` + status + `","currentDayNumber":2,"totalPauseDuration":0}}}`))
	})
}
