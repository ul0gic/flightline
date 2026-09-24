package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestF5A_IAPCommerceReadCommandTree(t *testing.T) {
	root := newIAPCommerceCommand()
	want := map[string]bool{"price-points": false, "pricing": false, "availability": false, "set-price": false, "set-availability": false}
	for _, child := range root.Commands() {
		if _, ok := want[child.Name()]; !ok {
			t.Fatalf("unexpected command %s", child.Name())
		}
		want[child.Name()] = true
		if child.Flags().Lookup("product") == nil {
			t.Errorf("%s lacks product selector", child.Name())
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing command %s", name)
		}
	}
}

func TestF5A_IAPCommerceWriteFlagsRequireConfirmationAndPreserveOmission(t *testing.T) {
	for _, cmd := range []*cobra.Command{newIAPSetPriceCommand(), newIAPSetAvailabilityCommand()} {
		if err := requireIAPCommerceConfirm(cmd); err == nil {
			t.Errorf("%s allowed write without confirmation", cmd.Name())
		}
	}
	cmd := newIAPSetAvailabilityCommand()
	if err := cmd.Flags().Set("new-territories", "false"); err != nil {
		t.Fatal(err)
	}
	input, err := iapAvailabilityFlags(cmd)
	if err != nil || input.newTerritories == nil || *input.newTerritories || input.territories != nil {
		t.Fatalf("partial input=%+v err=%v", input, err)
	}
	clearCommand := newIAPSetAvailabilityCommand()
	if err := clearCommand.Flags().Set("clear-territories", "true"); err != nil {
		t.Fatal(err)
	}
	input, err = iapAvailabilityFlags(clearCommand)
	if err != nil || input.territories == nil || len(*input.territories) != 0 {
		t.Fatalf("clear input=%+v err=%v", input, err)
	}
}

func TestF5A_IAPSetPriceNoOpAndReadFailureDoNotPOST(t *testing.T) {
	posts := 0
	failPoint := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			t.Error("unexpected POST")
			return
		}
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/iapPriceSchedule":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchasePriceSchedules","id":"S1","relationships":{"baseTerritory":{"data":{"type":"territories","id":"USA"}}}}}`))
		case "/v1/inAppPurchasePriceSchedules/S1/manualPrices":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePrices","id":"M1","attributes":{"manual":true,"startDate":"2026-01-01"},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}},"inAppPurchasePricePoint":{"data":{"type":"inAppPurchasePricePoints","id":"P1"}}}}],"links":{}}`))
		case "/v2/inAppPurchases/I1/pricePoints":
			if failPoint {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePricePoints","id":"P1","relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := fixtureASCClient(t, srv)
	at := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	result, err := setIAPPriceWithClient(context.Background(), c, "I1", "USA", "P1", at)
	if err != nil || result.Changed || posts != 0 {
		t.Fatalf("result=%+v err=%v posts=%d", result, err, posts)
	}
	failPoint = true
	if _, err := setIAPPriceWithClient(context.Background(), c, "I1", "USA", "P1", at); err == nil || posts != 0 {
		t.Fatalf("read failure err=%v posts=%d", err, posts)
	}
}

func TestF5A_IAPSetPricePostsIAPSchedule(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			if r.URL.Path != "/v1/inAppPurchasePriceSchedules" {
				t.Errorf("wrong POST %s", r.URL)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			buf, _ := json.Marshal(body)
			if !strings.Contains(string(buf), `"id":"I1"`) || !strings.Contains(string(buf), `"id":"P2"`) || !strings.Contains(string(buf), `"id":"USA"`) {
				t.Errorf("payload=%s", buf)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchasePriceSchedules","id":"S2"}}`))
			return
		}
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/iapPriceSchedule":
			_, _ = w.Write([]byte(`{"data":null}`))
		case "/v2/inAppPurchases/I1/pricePoints":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePricePoints","id":"P2","relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	result, err := setIAPPriceWithClient(context.Background(), fixtureASCClient(t, srv), "I1", "USA", "P2", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err != nil || !result.Changed || result.ID != "S2" || posts != 1 {
		t.Fatalf("result=%+v err=%v posts=%d", result, err, posts)
	}
}

func TestF5A_IAPSetAvailabilityPreservesOmittedSetAndPostsExplicitEmpty(t *testing.T) {
	posts := 0
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if posts == 2 {
				_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A3","attributes":{"availableInNewTerritories":true}}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A2","attributes":{"availableInNewTerritories":false}}}`))
			}
			return
		}
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/inAppPurchaseAvailability":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A1","attributes":{"availableInNewTerritories":true}}}`))
		case "/v1/inAppPurchaseAvailabilities/A1/availableTerritories":
			_, _ = w.Write([]byte(`{"data":[{"type":"territories","id":"USA"}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := fixtureASCClient(t, srv)
	result, err := setIAPAvailabilityWithClient(context.Background(), c, "I1", iapAvailabilityInput{newTerritories: new(true)})
	if err != nil || result.Changed || posts != 0 {
		t.Fatalf("no-op result=%+v err=%v posts=%d", result, err, posts)
	}
	result, err = setIAPAvailabilityWithClient(context.Background(), c, "I1", iapAvailabilityInput{newTerritories: new(false)})
	if err != nil || !result.Changed || posts != 1 {
		t.Fatalf("partial result=%+v err=%v posts=%d", result, err, posts)
	}
	buf, _ := json.Marshal(body)
	if !strings.Contains(string(buf), `"availableInNewTerritories":false`) || !strings.Contains(string(buf), `"id":"USA"`) {
		t.Fatalf("omitted set not preserved: %s", buf)
	}
	empty := []string{}
	result, err = setIAPAvailabilityWithClient(context.Background(), c, "I1", iapAvailabilityInput{newTerritories: new(true), territories: &empty})
	if err != nil || !result.Changed || posts != 2 {
		t.Fatalf("clear result=%+v err=%v posts=%d", result, err, posts)
	}
	buf, _ = json.Marshal(body)
	if !strings.Contains(string(buf), `"availableTerritories":{"data":[]}`) {
		t.Fatalf("explicit empty set lost: %s", buf)
	}
}

func TestF5A_IAPCommerceOutputKeepsResourceSpecificFields(t *testing.T) {
	falseValue := false
	results := []any{
		IAPPricePointsResult{ProductID: "P1", Points: []asc.IAPPricePoint{{ID: "PP1", TerritoryID: "USA", CustomerPrice: "1.99"}}},
		IAPPricingResult{ProductID: "P1", Schedule: asc.IAPPriceSchedule{ID: "S1", BaseTerritoryID: "USA", ManualPrices: []asc.IAPManualPrice{{ID: "M1", PricePointID: "PP1"}}}},
		IAPAvailabilityResult{ProductID: "P1", Availability: asc.IAPAvailability{ID: "A1", AvailableInNewTerritories: &falseValue, TerritoryIDs: []string{"USA"}}},
	}
	for _, result := range results {
		buf, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(buf), `"productId":"P1"`) {
			t.Errorf("missing product ID: %s", buf)
		}
	}
	buf, _ := json.Marshal(results[2])
	if !strings.Contains(string(buf), `"availableInNewTerritories":false`) {
		t.Errorf("explicit false lost: %s", buf)
	}
}
