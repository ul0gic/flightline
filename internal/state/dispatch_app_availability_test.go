package state

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

func TestF5B_ApplyAppAvailabilityChangeRefreshesAndPatchesActivePreOrder(t *testing.T) {
	trueValue := true
	var patch struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Available *bool `json:"available"`
			} `json:"attributes"`
		} `json:"data"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1"}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[{"type":"territoryAvailabilities","id":"TA-GBR","attributes":{"available":false,"preOrderEnabled":true},"relationships":{"territory":{"data":{"type":"territories","id":"GBR"}}}}],"links":{}}`))
		case "/v1/territoryAvailabilities/TA-GBR":
			if r.Method != http.MethodPatch {
				t.Errorf("method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Errorf("decode patch: %v", err)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"territoryAvailabilities","id":"TA-GBR","attributes":{"available":true}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	change := plan.Change{Op: plan.OpUpdate, Path: "/spec/appAvailability/territories/GBR/available", From: false, To: true}
	if err := applyAppAvailabilityChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app"}, change); err != nil {
		t.Fatalf("applyAppAvailabilityChange: %v", err)
	}
	if patch.Data.ID != "TA-GBR" || patch.Data.Attributes.Available == nil || !*patch.Data.Attributes.Available || !trueValue {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestF5B_ApplyAppAvailabilityChangeRejectsOrdinaryTerritoryBeforePatch(t *testing.T) {
	var patches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1"}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[{"type":"territoryAvailabilities","id":"TA-USA","attributes":{"available":false,"preOrderEnabled":false},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
		case "/v1/territoryAvailabilities/TA-USA":
			patches++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	err := applyAppAvailabilityChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app"}, plan.Change{Op: plan.OpUpdate, Path: "/spec/appAvailability/territories/USA/available", From: false, To: true})
	if err == nil || !strings.Contains(err.Error(), "ordinary availability writes are unsupported") || patches != 0 {
		t.Fatalf("err = %v, patches = %d", err, patches)
	}
}

func TestF5B_ValidateAppAvailabilityChangeRejectsMalformedAndInvalidDate(t *testing.T) {
	for _, change := range []plan.Change{
		{Op: plan.OpCreate, Path: "/spec/appAvailability/territories/GBR/available", To: true},
		{Op: plan.OpUpdate, Path: "/spec/appAvailability/territories/GBR/preOrderEnabled", To: true},
		{Op: plan.OpUpdate, Path: "/spec/appAvailability/territories/GBR/releaseDate", To: ""},
	} {
		if err := ValidateAppAvailabilityChange(change); err == nil {
			t.Fatalf("change accepted: %#v", change)
		}
	}
}
