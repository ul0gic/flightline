package state

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

func TestG3_AgeFrequencyWritesExactEnum(t *testing.T) {
	var value any
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"APP1","type":"apps"}]}`))
		case "/v1/apps/APP1/appInfos":
			_, _ = w.Write([]byte(`{"data":[{"id":"INFO1","type":"appInfos","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}]}`))
		case "/v1/appInfos/INFO1/ageRatingDeclaration":
			_, _ = w.Write([]byte(`{"data":{"id":"AGE1","type":"ageRatingDeclarations"}}`))
		case "/v1/ageRatingDeclarations/AGE1":
			var body struct {
				Data struct {
					Attributes map[string]any `json:"attributes"`
				} `json:"data"`
			}
			if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
				t.Errorf("decode: %v", decodeErr)
			}
			value = body.Data.Attributes["violenceRealisticProlongedGraphicOrSadistic"]
			_, _ = w.Write([]byte(`{"data":{"id":"AGE1","type":"ageRatingDeclarations"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}, plan.Change{Op: plan.OpUpdate, Resource: "ageRating", Path: "/spec/ageRating/prolongedGraphicSadisticRealisticViolence", To: "INFREQUENT"})
	if err != nil {
		t.Fatal(err)
	}
	if value != "INFREQUENT" {
		t.Fatalf("frequency reduced or coerced: %#v", value)
	}
}
