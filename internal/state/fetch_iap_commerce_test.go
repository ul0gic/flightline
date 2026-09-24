package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF5A_CurrentIAPPriceSelectsOnlyActiveBase(t *testing.T) {
	schedule := asc.IAPPriceSchedule{ID: "S1", BaseTerritoryID: "USA", ManualPrices: []asc.IAPManualPrice{
		{ID: "H", TerritoryID: "USA", PricePointID: "OLD", StartDate: "2025-01-01", EndDate: "2026-01-01"},
		{ID: "C", TerritoryID: "USA", PricePointID: "NOW", StartDate: "2026-01-01", EndDate: "2027-01-01"},
		{ID: "F", TerritoryID: "USA", PricePointID: "FUTURE", StartDate: "2027-01-01"},
		{ID: "T", TerritoryID: "CAN", PricePointID: "OTHER"},
	}}
	price, err := currentIAPPrice(schedule, time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err != nil || price == nil || price.PricePointID != "NOW" || price.BaseTerritory != "USA" {
		t.Fatalf("price=%+v err=%v", price, err)
	}
	schedule.ManualPrices = append(schedule.ManualPrices, asc.IAPManualPrice{ID: "D", TerritoryID: "USA", PricePointID: "DUP", StartDate: "2026-06-01"})
	if _, err := currentIAPPrice(schedule, time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("overlapping current base prices accepted")
	}
}

func TestF5A_FetchIAPCommerceProjectsCompleteAvailability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/iapPriceSchedule":
			_, _ = w.Write([]byte(`{"data":null}`))
		case "/v2/inAppPurchases/I1/inAppPurchaseAvailability":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A1","attributes":{"availableInNewTerritories":false}}}`))
		case "/v1/inAppPurchaseAvailabilities/A1/availableTerritories":
			_, _ = w.Write([]byte(`{"data":[{"type":"territories","id":"USA"},{"type":"territories","id":"CAN"}],"links":{}}`))
		default:
			t.Errorf("request=%s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	commerce, err := FetchIAPCommerce(context.Background(), fixtureClient(t, srv), "I1")
	if err != nil || commerce == nil || commerce.Pricing != nil || commerce.Availability == nil || commerce.Availability.AvailableTerritories == nil || len(*commerce.Availability.AvailableTerritories) != 2 || *commerce.Availability.AvailableInNewTerritories {
		t.Fatalf("commerce=%+v err=%v", commerce, err)
	}
}
