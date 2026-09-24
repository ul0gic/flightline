package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestF5A_IAPPricePointsPaginationAndTerritory(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/inAppPurchases/I1/pricePoints" || r.URL.Query().Get("filter[territory]") != "USA" {
			t.Errorf("request=%s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePricePoints","id":"P2","attributes":{"customerPrice":"2.99"},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePricePoints","id":"P1","attributes":{"customerPrice":"1.99"},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{"next":"` + serverURL + `/v2/inAppPurchases/I1/pricePoints?filter%5Bterritory%5D=USA&page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	points, err := ListIAPPricePoints(context.Background(), newTestClient(t, srv), "I1", "USA")
	if err != nil || len(points) != 2 || points[0].ID != "P1" || points[1].ID != "P2" {
		t.Fatalf("points=%+v err=%v", points, err)
	}
}

func TestF5A_IAPPriceBodyPreservesWindows(t *testing.T) {
	schedule := IAPPriceSchedule{ID: "S1", BaseTerritoryID: "USA", ManualPrices: []IAPManualPrice{
		{ID: "PAST", TerritoryID: "USA", PricePointID: "OLD", StartDate: "2025-01-01", EndDate: "2026-01-01"},
		{ID: "CURRENT", TerritoryID: "USA", PricePointID: "CURRENT", StartDate: "2026-01-01"},
		{ID: "FUTURE", TerritoryID: "USA", PricePointID: "FUTURE", StartDate: "2027-01-01"},
		{ID: "OTHER", TerritoryID: "CAN", PricePointID: "CANPOINT", StartDate: "2026-01-01"},
	}}
	body, err := buildIAPPreservingPriceBody("I1", "USA", "NEW", schedule, "2026-09-23")
	if err != nil {
		t.Fatal(err)
	}
	buf, _ := json.Marshal(body)
	for _, fragment := range []string{`"id":"CANPOINT"`, `"id":"FUTURE"`, `"id":"OLD"`, `"id":"NEW"`, `"endDate":"2027-01-01"`, `"startDate":"2026-09-23"`, `"type":"inAppPurchasePriceSchedules"`} {
		if !strings.Contains(string(buf), fragment) {
			t.Errorf("missing %s in %s", fragment, buf)
		}
	}
	if !strings.Contains(string(buf), `"endDate":"2026-09-23"`) || !strings.Contains(string(buf), `"id":"CURRENT"`) {
		t.Fatalf("current price history was not closed at replacement: %s", buf)
	}
}

func TestF5A_IAPPriceWriteRejectsWrongPointBeforePOST(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			w.WriteHeader(http.StatusCreated)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/iapPriceSchedule") {
			_, _ = w.Write([]byte(`{"data":null}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/pricePoints") {
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
			return
		}
		t.Errorf("unexpected request %s", r.URL)
	}))
	defer srv.Close()
	_, err := CreatePreservingIAPPriceSchedule(context.Background(), newTestClient(t, srv), "I1", "USA", "WRONG", nil, time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err == nil || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF5A_IAPAvailabilityWriteNoOpAndFullSet(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			buf, _ := json.Marshal(body)
			if !strings.Contains(string(buf), `"availableInNewTerritories":false`) || !strings.Contains(string(buf), `"id":"CAN"`) || !strings.Contains(string(buf), `"id":"USA"`) {
				t.Errorf("body=%s", buf)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A2","attributes":{"availableInNewTerritories":false}}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/inAppPurchaseAvailability") {
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A1","attributes":{"availableInNewTerritories":true}}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/availableTerritories") {
			_, _ = w.Write([]byte(`{"data":[{"type":"territories","id":"USA"}],"links":{}}`))
			return
		}
		t.Errorf("unexpected request %s", r.URL)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	expected := &IAPAvailability{ID: "A1", AvailableInNewTerritories: new(true), TerritoryIDs: []string{"USA"}}
	result, err := CreateIAPAvailability(context.Background(), c, "I1", true, []string{"USA"}, expected)
	if err != nil || result.Changed || posts != 0 {
		t.Fatalf("no-op result=%+v err=%v posts=%d", result, err, posts)
	}
	result, err = CreateIAPAvailability(context.Background(), c, "I1", false, []string{"USA", "CAN"}, expected)
	if err != nil || !result.Changed || posts != 1 {
		t.Fatalf("write result=%+v err=%v posts=%d", result, err, posts)
	}
}

func TestF5A_IAPSchedulePaginatesManualWindows(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/inAppPurchases/I1/iapPriceSchedule":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchasePriceSchedules","id":"S1","relationships":{"baseTerritory":{"data":{"type":"territories","id":"USA"}}}}}`))
		case r.URL.Path == "/v1/inAppPurchasePriceSchedules/S1/manualPrices" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePrices","id":"M2","attributes":{"manual":true,"startDate":"2027-01-01"},"relationships":{"territory":{"data":{"type":"territories","id":"CAN"}},"inAppPurchasePricePoint":{"data":{"type":"inAppPurchasePricePoints","id":"PC"}}}}],"links":{}}`))
		case r.URL.Path == "/v1/inAppPurchasePriceSchedules/S1/manualPrices":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePrices","id":"M1","attributes":{"manual":true,"startDate":"2026-01-01"},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}},"inAppPurchasePricePoint":{"data":{"type":"inAppPurchasePricePoints","id":"PU"}}}}],"links":{"next":"` + serverURL + `/v1/inAppPurchasePriceSchedules/S1/manualPrices?page=2"}}`))
		default:
			t.Errorf("request=%s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	schedule, err := ReadIAPPriceSchedule(context.Background(), newTestClient(t, srv), "I1")
	if err != nil || schedule.BaseTerritoryID != "USA" || len(schedule.ManualPrices) != 2 || schedule.ManualPrices[1].TerritoryID != "CAN" {
		t.Fatalf("schedule=%+v err=%v", schedule, err)
	}
}

func TestF5A_IAPAvailabilityCompleteTerritoriesAndLaterFailure(t *testing.T) {
	var serverURL string
	broken := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/inAppPurchases/I1/inAppPurchaseAvailability":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseAvailabilities","id":"A1","attributes":{"availableInNewTerritories":false}}}`))
		case r.URL.Path == "/v1/inAppPurchaseAvailabilities/A1/availableTerritories" && r.URL.Query().Get("page") == "2":
			if broken {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"type":"territories","id":"CAN"}],"links":{}}`))
		case r.URL.Path == "/v1/inAppPurchaseAvailabilities/A1/availableTerritories":
			_, _ = w.Write([]byte(`{"data":[{"type":"territories","id":"USA"}],"links":{"next":"` + serverURL + `/v1/inAppPurchaseAvailabilities/A1/availableTerritories?page=2"}}`))
		default:
			t.Errorf("request=%s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	c := newTestClient(t, srv)
	got, err := ReadIAPAvailability(context.Background(), c, "I1")
	if err != nil || got.AvailableInNewTerritories == nil || *got.AvailableInNewTerritories || len(got.TerritoryIDs) != 2 {
		t.Fatalf("availability=%+v err=%v", got, err)
	}
	broken = true
	if _, err := ReadIAPAvailability(context.Background(), c, "I1"); err == nil {
		t.Fatal("later territory page failure accepted")
	}
}

func TestF5A_IAPSingletonNullVersusMalformed(t *testing.T) {
	for _, body := range []string{`{"data":null}`, `{"data":{}}`, `{}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		schedule, err := ReadIAPPriceSchedule(context.Background(), newTestClient(t, srv), "I1")
		if strings.Contains(body, "null") && (err != nil || schedule.ID != "") {
			t.Fatalf("null schedule=%+v err=%v", schedule, err)
		}
		if !strings.Contains(body, "null") && err == nil {
			t.Fatal("malformed schedule accepted")
		}
		srv.Close()
	}
}

func TestF5A_IAPOptional404DoesNotHideNested404(t *testing.T) {
	for _, nested := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !nested || strings.HasSuffix(r.URL.Path, "/manualPrices") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchasePriceSchedules","id":"S1","relationships":{"baseTerritory":{"data":{"type":"territories","id":"USA"}}}}}`))
		}))
		schedule, err := ReadIAPPriceSchedule(context.Background(), newTestClient(t, srv), "I1")
		if nested && err == nil {
			t.Fatal("nested 404 accepted as missing schedule")
		}
		if !nested && (err != nil || schedule.ID != "") {
			t.Fatalf("optional schedule=%+v err=%v", schedule, err)
		}
		srv.Close()
	}
}
