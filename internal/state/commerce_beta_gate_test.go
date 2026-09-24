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

func TestG5_InvalidExtensionPreventsEarlierWrite(t *testing.T) {
	withTempCacheDir(t)
	for _, invalid := range []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/iap/products/p/commerce/pricing", To: config.IAPPriceSpec{BaseTerritory: "USA"}},
		{Op: plan.OpUpdate, Path: "/spec/appAvailability/territories/USA/preOrderEnabled", To: true},
		{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/reviewDetails/demoAccountPassword", To: "not-a-real-secret"},
		{Op: plan.OpUpdate, Path: "/spec/testflight/groups/family/builds", To: []config.BetaBuildSelector{{Number: "42"}}},
	} {
		t.Run(invalid.Path, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}))
			defer srv.Close()
			changes := []plan.Change{{Op: plan.OpUpdate, Path: "/spec/version/copyright", To: "updated"}, invalid}
			result, err := Apply(context.Background(), fixtureClient(t, srv), changes, ApplyOpts{Context: defaultApplyCtx(), Confirm: true})
			if err == nil || len(result.Applied) != 0 || requests.Load() != 0 {
				t.Fatalf("result=%+v err=%v requests=%d", result, err, requests.Load())
			}
		})
	}
}

func TestG5_NewParentsKeepCommerceAndMembershipAsChildren(t *testing.T) {
	builds := []config.BetaBuildSelector{{Number: "42", Version: "1.0", Platform: "IOS"}}
	desired := &config.State{Spec: config.StateSpec{
		IAP:        &config.IAPSpec{Products: map[string]config.IAPProduct{"com.example.item": {Type: "NON_CONSUMABLE", Name: new("Item"), Commerce: &config.IAPCommerceSpec{Pricing: &config.IAPPriceSpec{BaseTerritory: "USA", PricePointID: "P1"}}}}},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{"family": {IsInternal: new(true), Builds: &builds}}},
	}}
	changes := plan.Diff(desired, &config.State{})
	if len(changes) != 4 {
		t.Fatalf("changes=%+v", changes)
	}
	if errors := ValidateChanges(changes); len(errors) != 0 {
		t.Fatalf("validation=%+v", errors)
	}
	parents := map[string]plan.Change{}
	for _, ch := range changes {
		switch value := ch.To.(type) {
		case config.IAPProduct:
			if value.Commerce != nil {
				t.Fatal("IAP parent contains commerce")
			}
			parents["iap"] = ch
		case config.TestFlightGroup:
			if value.Builds != nil {
				t.Fatal("group parent contains builds")
			}
			parents["beta"] = ch
		}
	}
	if len(parents) != 2 {
		t.Fatalf("parents=%+v", parents)
	}
	for _, ch := range changes {
		if ch.Path == "/spec/iap/products/com.example.item/commerce/pricing" && !changeDependsOn(ch, parents["iap"]) {
			t.Fatal("missing IAP parent dependency")
		}
		if ch.Path == "/spec/testflight/groups/family/builds" && !changeDependsOn(ch, parents["beta"]) {
			t.Fatal("missing group parent dependency")
		}
	}
}

func TestG5_CommerceBetaApplyRefetchEmptyPlan(t *testing.T) {
	withTempCacheDir(t)
	var commerce, availability, metadata, membership atomic.Bool
	srv := httptest.NewServer(g5CommerceBetaHandler(t, &commerce, &availability, &metadata, &membership))
	defer srv.Close()
	desired := &config.State{Spec: config.StateSpec{
		IAP:             &config.IAPSpec{Products: map[string]config.IAPProduct{"com.x.lifetime": {Type: "NON_CONSUMABLE", Commerce: &config.IAPCommerceSpec{Availability: &config.IAPAvailabilitySpec{AvailableInNewTerritories: new(false)}}}}},
		AppAvailability: &config.AppAvailabilitySpec{Territories: map[string]config.TerritoryAvailabilitySpec{"USA": {ReleaseDate: new("2099-02-01")}}},
		TestFlight:      &config.TestFlightSpec{Metadata: &config.BetaMetadataSpec{AppLocalizations: map[string]config.BetaAppLocalizationSpec{"en-US": {Description: new("Updated beta")}}}, Groups: map[string]config.TestFlightGroup{"family": {Builds: &[]config.BetaBuildSelector{}}}},
	}}
	ctx := context.Background()
	c := fixtureClient(t, srv)
	opts := FetchOpts{Version: "1.0", Platform: "IOS", BetaDesired: desired.Spec.TestFlight}
	live, err := Fetch(ctx, c, "com.example.app", opts)
	if err != nil {
		t.Fatal(err)
	}
	if ds := config.ValidateWriteIntent("state.yaml", desired, live); len(ds) > 0 {
		t.Fatalf("intent=%+v", ds)
	}
	changes := plan.Diff(desired, live)
	if len(changes) != 4 {
		t.Fatalf("changes=%+v", changes)
	}
	result, err := Apply(ctx, c, changes, ApplyOpts{Context: defaultApplyCtx(), Confirm: true})
	if err != nil || len(result.Applied) != 4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	live, err = Fetch(ctx, c, "com.example.app", opts)
	if err != nil {
		t.Fatal(err)
	}
	if changes := plan.Diff(desired, live); len(changes) != 0 {
		t.Fatalf("residual=%+v", changes)
	}
	if !commerce.Load() || !availability.Load() || !metadata.Load() || !membership.Load() {
		t.Fatal("missing mutation")
	}
	if territories := live.Spec.IAP.Products["com.x.lifetime"].Commerce.Availability.AvailableTerritories; territories == nil || len(*territories) != 1 || (*territories)[0] != "USA" {
		t.Fatal("omitted IAP territories changed")
	}
}

func g5CommerceBetaHandler(t *testing.T, commerce, availability, metadata, membership *atomic.Bool) http.Handler {
	t.Helper()
	base := fullCoverageHandler(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/inAppPurchaseAvailabilities":
			commerce.Store(true)
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"IA1","attributes":{"availableInNewTerritories":false}}}`))
			return
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/territoryAvailabilities/AVT1":
			availability.Store(true)
			_, _ = w.Write([]byte(`{"data":{"type":"territoryAvailabilities","id":"AVT1","attributes":{"releaseDate":"2099-02-01"}}}`))
			return
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/betaAppLocalizations/BA1":
			metadata.Store(true)
			_, _ = w.Write([]byte(`{"data":{"type":"betaAppLocalizations","id":"BA1","attributes":{"locale":"en-US","description":"Updated beta"}}}`))
			return
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/betaGroups/BG1/relationships/builds":
			membership.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if response, ok := g5CommerceBetaResponse(r.URL.Path, commerce, availability, metadata, membership); ok {
			_, _ = w.Write([]byte(response))
			return
		}
		base.ServeHTTP(w, r)
	})
}

func g5CommerceBetaResponse(path string, commerce, availability, metadata, membership *atomic.Bool) (string, bool) {
	response, ok := commerceBetaSnapshotResponse(path)
	if !ok {
		return "", false
	}
	switch path {
	case "/v2/inAppPurchases/IAP1/inAppPurchaseAvailability":
		if commerce.Load() {
			response = strings.Replace(response, `"availableInNewTerritories":true`, `"availableInNewTerritories":false`, 1)
		}
	case "/v2/appAvailabilities/AVA1/territoryAvailabilities":
		if availability.Load() {
			response = strings.Replace(response, "2099-01-01", "2099-02-01", 1)
		}
	case "/v1/apps/APP1/betaAppLocalizations":
		if metadata.Load() {
			response = strings.Replace(response, "Beta description", "Updated beta", 1)
		}
	case "/v1/betaGroups/BG1/builds":
		if membership.Load() {
			response = `{"data":[]}`
		}
	}
	return response, true
}
