package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestC2B_PreservesUnmanagedPriceWindows(t *testing.T) {
	schedule := ManualPriceSchedule{ID: "S1", BaseTerritoryID: "USA", Prices: []ManualSchedulePrice{
		{ID: "old", TerritoryID: "USA", PricePointID: "OLD", StartDate: "2026-01-01"},
		{ID: "future", TerritoryID: "USA", PricePointID: "FUTURE", StartDate: "2027-01-01"},
		{ID: "foreign", TerritoryID: "GBR", PricePointID: "GBRPOINT", StartDate: "2026-01-01"},
	}}
	body, err := buildPreservingPriceSchedule("APP1", "USA", "NEW", schedule, "2026-09-23")
	if err != nil {
		t.Fatal(err)
	}
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Data struct {
			Relationships map[string]struct {
				Data json.RawMessage `json:"data"`
			} `json:"relationships"`
		} `json:"data"`
		Included []struct {
			ID            string         `json:"id"`
			Attributes    map[string]any `json:"attributes"`
			Relationships map[string]any `json:"relationships"`
		} `json:"included"`
	}
	if err := json.Unmarshal(buf, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Included) != 4 {
		t.Fatalf("included=%s", buf)
	}
	assertC2BPriceWindows(t, buf, wire.Included)
	if len(wire.Data.Relationships["manualPrices"].Data) == 0 {
		t.Fatalf("missing manualPrices linkage: %s", buf)
	}
}

func assertC2BPriceWindows(t *testing.T, buf []byte, included []struct {
	ID            string         `json:"id"`
	Attributes    map[string]any `json:"attributes"`
	Relationships map[string]any `json:"relationships"`
}) {
	t.Helper()
	points := map[string]bool{}
	var links []struct {
		ID string `json:"id"`
	}
	var linkage struct {
		Data struct {
			Relationships map[string]struct {
				Data json.RawMessage `json:"data"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf, &linkage); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(linkage.Data.Relationships["manualPrices"].Data, &links); err != nil {
		t.Fatal(err)
	}
	for i, row := range included {
		if len(links) != len(included) || links[i].ID != row.ID || row.ID != fmt.Sprintf("${newprice-%d}", i) {
			t.Fatalf("inline linkage mismatch: %s", buf)
		}
		if _, ok := row.Relationships["territory"]; ok {
			t.Fatalf("unsupported inline territory: %s", buf)
		}
		pointID := c2bPointID(t, row.Relationships["appPricePoint"])
		points[pointID] = true
		if pointID == "NEW" && (row.Attributes["startDate"] != "2026-09-23" || row.Attributes["endDate"] != "2027-01-01") {
			t.Fatalf("new price does not stop at future window: %s", buf)
		}
		if pointID == "OLD" && row.Attributes["endDate"] != "2026-09-23" {
			t.Fatalf("historical price overlaps replacement: %s", buf)
		}
	}
	if !points["OLD"] || !points["FUTURE"] || !points["GBRPOINT"] || !points["NEW"] {
		t.Fatalf("prices=%v", points)
	}
}

func c2bPointID(t *testing.T, relationship any) string {
	t.Helper()
	buf, err := json.Marshal(relationship)
	if err != nil {
		t.Fatal(err)
	}
	var rel struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf, &rel); err != nil {
		t.Fatal(err)
	}
	return rel.Data.ID
}

func TestC2B_RejectsPointTerritoryMismatchBeforePost(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP1/appPriceSchedule":
			w.WriteHeader(http.StatusNotFound)
		case "/v3/appPricePoints/PP1":
			_, _ = w.Write([]byte(`{"data":{"id":"PP1","relationships":{"territory":{"data":{"id":"GBR"}}}}}`))
		case "/v1/appPriceSchedules":
			posts.Add(1)
			_, _ = w.Write([]byte(`{"data":{"id":"S1"}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	err := CreatePreservingPriceSchedule(context.Background(), newTestClient(t, srv), "APP1", "USA", "PP1", nil, time.Now())
	if err == nil || posts.Load() != 0 {
		t.Fatalf("err=%v posts=%d", err, posts.Load())
	}
}

func TestC2B_RejectsOverlappingActiveBasePrices(t *testing.T) {
	schedule := ManualPriceSchedule{ID: "S1", BaseTerritoryID: "USA", Prices: []ManualSchedulePrice{
		{TerritoryID: "USA", PricePointID: "OLD", StartDate: "2026-01-01"},
		{TerritoryID: "USA", PricePointID: "NEWER", StartDate: "2026-09-01"},
	}}
	if _, err := activeBasePrice(schedule, "2026-09-23"); err == nil {
		t.Fatal("overlapping active base prices accepted")
	}
	if _, _, err := spliceManualPrices(schedule.Prices, "USA", "NEW", "2026-09-23", "", "2026-09-23", true); err == nil {
		t.Fatal("overlapping active prices accepted for write")
	}
}

func TestC2B_FutureWindowSpliceAndValidation(t *testing.T) {
	existing := make([]ManualSchedulePrice, 0, 3)
	existing = append(existing,
		ManualSchedulePrice{ID: "current", TerritoryID: "USA", PricePointID: "OLD", StartDate: "2026-01-01"},
		ManualSchedulePrice{ID: "foreign", TerritoryID: "GBR", PricePointID: "GBRPOINT", StartDate: "2026-01-01"},
	)
	prices, replacement, err := spliceManualPrices(existing, "USA", "NEW", "2026-11-01", "2026-12-01", "2026-09-23", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 3 || prices[0].EndDate != "2026-11-01" || prices[1].StartDate != "2026-12-01" || prices[2].TerritoryID != "GBR" || replacement.StartDate != "2026-11-01" || replacement.EndDate != "2026-12-01" {
		t.Fatalf("prices=%+v replacement=%+v", prices, replacement)
	}
	existing = append(existing, ManualSchedulePrice{ID: "future", TerritoryID: "USA", PricePointID: "FUTURE", StartDate: "2026-11-15"})
	if _, _, err := spliceManualPrices(existing, "USA", "NEW", "2026-11-01", "2026-12-01", "2026-09-23", false); err == nil {
		t.Fatal("overlapping future price accepted")
	}
	for _, dates := range [][2]string{{"2026-09-22", ""}, {"2026-11-01", "2026-11-01"}, {"bad", ""}} {
		if _, _, err := requestedPriceWindow(dates[0], dates[1], "2026-09-23"); err == nil {
			t.Fatalf("invalid dates accepted: %v", dates)
		}
	}
}

func TestC2B_ReplaceCurrentPriceTwiceSameDay(t *testing.T) {
	existing := []ManualSchedulePrice{{ID: "today", TerritoryID: "USA", PricePointID: "FIRST", StartDate: "2026-09-23"}}
	prices, replacement, err := spliceManualPrices(existing, "USA", "SECOND", "2026-09-23", "", "2026-09-23", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 0 || replacement.PricePointID != "SECOND" || replacement.StartDate != "2026-09-23" {
		t.Fatalf("prices=%+v replacement=%+v", prices, replacement)
	}
}

func TestC2B_FutureRowBeforeCurrentDoesNotRestoreOldPrice(t *testing.T) {
	existing := []ManualSchedulePrice{
		{ID: "future", TerritoryID: "USA", PricePointID: "FUTURE", StartDate: "2027-01-01"},
		{ID: "current", TerritoryID: "USA", PricePointID: "OLD", StartDate: "2026-01-01"},
	}
	prices, replacement, err := spliceManualPrices(existing, "USA", "NEW", "2026-09-23", "", "2026-09-23", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 || prices[0].PricePointID != "FUTURE" || prices[1].EndDate != "2026-09-23" || replacement.EndDate != "2027-01-01" {
		t.Fatalf("prices=%+v replacement=%+v", prices, replacement)
	}
}

func TestC2B_ExistingRequestedWindowIsNoOp(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP1/appPriceSchedule":
			_, _ = w.Write([]byte(`{"data":{"id":"S1","relationships":{"baseTerritory":{"data":{"id":"USA"}}}}}`))
		case "/v1/appPriceSchedules/S1/manualPrices":
			_, _ = w.Write([]byte(`{"data":[{"id":"P1","attributes":{"manual":true,"startDate":"2026-11-01","endDate":"2026-12-01"},"relationships":{"territory":{"data":{"id":"USA"}},"appPricePoint":{"data":{"id":"NEW"}}}}],"links":{}}`))
		case "/v3/appPricePoints/NEW":
			_, _ = w.Write([]byte(`{"data":{"id":"NEW","relationships":{"territory":{"data":{"id":"USA"}}}}}`))
		case "/v1/appPriceSchedules":
			posts.Add(1)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	result, err := CreatePreservingPriceWindow(context.Background(), newTestClient(t, srv), "APP1", "USA", "NEW", "2026-11-01", "2026-12-01", &ActiveBasePrice{TerritoryID: "USA"}, time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err != nil || result.ScheduleID != "S1" || result.Changed || posts.Load() != 0 {
		t.Fatalf("result=%+v err=%v posts=%d", result, err, posts.Load())
	}
}

func TestC1A_ReadActiveBasePrice(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP1/appPriceSchedule":
			if r.URL.Query().Has("limit") || r.URL.Query().Get("fields[appPriceSchedules]") != "baseTerritory,manualPrices" {
				t.Errorf("invalid schedule query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appPriceSchedules","id":"S1","relationships":{"baseTerritory":{"data":{"type":"territories","id":"USA"}}}}}`))
		case "/v1/appPriceSchedules/S1/manualPrices":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"data":[{"id":"P2","attributes":{"manual":true,"startDate":"2026-01-01"},"relationships":{"territory":{"data":{"id":"USA"}},"appPricePoint":{"data":{"id":"CURRENT"}}}},{"id":"P3","attributes":{"manual":true,"startDate":"2027-01-01"},"relationships":{"territory":{"data":{"id":"USA"}},"appPricePoint":{"data":{"id":"FUTURE"}}}}],"links":{}}`))
				return
			}
			if r.URL.Query().Get("include") != "territory,appPricePoint" {
				t.Errorf("missing relationship include: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"P1","attributes":{"manual":true},"relationships":{"territory":{"data":{"id":"GBR"}},"appPricePoint":{"data":{"id":"WRONG"}}}}],"links":{"next":"` + serverURL + `/v1/appPriceSchedules/S1/manualPrices?page=2"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	got, err := ReadActiveBasePrice(context.Background(), newTestClient(t, srv), "APP1", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.TerritoryID != "USA" || got.PricePointID != "CURRENT" {
		t.Fatalf("got %+v", got)
	}
}

func TestC1A_ReadActiveBasePriceLaterPageFailure(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/apps/APP1/appPriceSchedule" {
			_, _ = w.Write([]byte(`{"data":{"id":"S1","relationships":{"baseTerritory":{"data":{"id":"USA"}}}}}`))
			return
		}
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"links":{"next":"` + serverURL + `/v1/appPriceSchedules/S1/manualPrices?page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	if _, err := ReadActiveBasePrice(context.Background(), newTestClient(t, srv), "APP1", time.Now()); err == nil {
		t.Fatal("later page failure accepted")
	}
}

func TestC1A_ReadActiveBasePriceAbsenceBoundary(t *testing.T) {
	for _, tc := range []priceAbsenceCase{
		{"schedule absent", http.StatusNotFound, 0, "", false},
		{"schedule null", http.StatusOK, 0, "", false},
		{"schedule malformed", http.StatusOK, 0, "", true},
		{"nested collection missing", http.StatusOK, http.StatusNotFound, "", true},
		{"missing price territory", http.StatusOK, http.StatusOK, `{"data":[{"id":"P1","attributes":{"manual":true},"relationships":{"appPricePoint":{"data":{"id":"PP1"}}}}],"links":{}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) { runPriceAbsenceCase(t, tc) })
	}
}

type priceAbsenceCase struct {
	name                         string
	scheduleStatus, pricesStatus int
	priceBody                    string
	wantError                    bool
}

func runPriceAbsenceCase(t *testing.T, tc priceAbsenceCase) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/apps/APP1/appPriceSchedule" {
			w.WriteHeader(tc.scheduleStatus)
			if tc.scheduleStatus == http.StatusOK {
				_, _ = w.Write([]byte(priceAbsenceScheduleBody(tc.name)))
			}
			return
		}
		w.WriteHeader(tc.pricesStatus)
		_, _ = w.Write([]byte(tc.priceBody))
	}))
	defer srv.Close()
	got, err := ReadActiveBasePrice(context.Background(), newTestClient(t, srv), "APP1", time.Now())
	if (err != nil) != tc.wantError {
		t.Fatalf("got %+v, %v; wantError=%v", got, err, tc.wantError)
	}
}

func priceAbsenceScheduleBody(name string) string {
	switch name {
	case "schedule null":
		return `{"data":null}`
	case "schedule malformed":
		return `{"data":{}}`
	default:
		return `{"data":{"id":"S1","relationships":{"baseTerritory":{"data":{"id":"USA"}}}}}`
	}
}
