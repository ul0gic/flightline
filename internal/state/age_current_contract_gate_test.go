package state

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG4_CurrentAgeFieldsReachWire(t *testing.T) {
	observed := projectAgeRating(asc.AgeRatingDeclarationAttributes{AgeRatingOverrideV2: "EIGHTEEN_PLUS", KoreaAgeRatingOverride: "NINETEEN_PLUS", GracRatingClassificationNumber: "SYNTHETIC-GRAC"})
	desired := &config.State{APIVersion: "flightline.dev/v1alpha1", Kind: "AppState", Metadata: config.StateMetadata{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Spec: config.StateSpec{AgeRating: observed}}
	if ds := config.Validate("state.yaml", desired); len(ds) > 0 {
		t.Fatalf("schema: %+v", ds)
	}
	changes := plan.Diff(desired, nil)
	if len(changes) != 3 {
		t.Fatalf("changes=%+v", changes)
	}
	for _, change := range changes {
		t.Run(change.Path, func(t *testing.T) { assertG4AgeWire(t, change) })
	}
	if cs := plan.Diff(desired, desired); len(cs) > 0 {
		t.Fatalf("unchanged: %+v", cs)
	}
	for _, path := range []string{"/spec/ageRating/ageRatingOverrideV2", "/spec/ageRating/koreaAgeRatingOverride"} {
		if errors := ValidateChanges([]plan.Change{{Op: plan.OpUpdate, Path: path, To: "NONE|ALL"}}); len(errors) != 1 {
			t.Fatalf("invalid enum accepted: %s", path)
		}
	}
}

func assertG4AgeWire(t *testing.T, change plan.Change) {
	t.Helper()
	base := fullCoverageHandler(t)
	var sent map[string]any
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/v1/ageRatingDeclarations/AR1" {
			var body struct {
				Data struct {
					Attributes map[string]any `json:"attributes"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			sent = body.Data.Attributes
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"type":"ageRatingDeclarations","id":"AR1"}}`))
			return
		}
		base.ServeHTTP(w, r)
	}, change)
	if err != nil {
		t.Fatal(err)
	}
	key, err := schemaToWireAgeRating(change.Path)
	if err != nil || sent[key] != change.To {
		t.Fatalf("payload=%+v err=%v", sent, err)
	}
}
