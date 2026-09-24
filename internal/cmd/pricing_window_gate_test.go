package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestG2_PricingCommandPreservesRequestedDates(t *testing.T) {
	start := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	end := time.Now().UTC().AddDate(0, 0, 60).Format("2006-01-02")
	var posted struct {
		Included []struct {
			Attributes struct {
				Start string `json:"startDate"`
				End   string `json:"endDate"`
			} `json:"attributes"`
			Relationships struct {
				Point struct {
					Data struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"appPricePoint"`
			} `json:"relationships"`
		} `json:"included"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"APP1","type":"apps"}]}`))
		case "/v1/apps/APP1/appPriceSchedule":
			_, _ = w.Write([]byte(`{"data":{"id":"S1","type":"appPriceSchedules","relationships":{"baseTerritory":{"data":{"id":"USA","type":"territories"}}}}}`))
		case "/v1/appPriceSchedules/S1/manualPrices":
			_, _ = w.Write([]byte(`{"data":[{"id":"P1","type":"appPrices","attributes":{"manual":true},"relationships":{"territory":{"data":{"id":"USA"}},"appPricePoint":{"data":{"id":"OLD"}}}}]}`))
		case "/v3/appPricePoints/NEW":
			_, _ = w.Write([]byte(`{"data":{"id":"NEW","type":"appPricePoints","relationships":{"territory":{"data":{"id":"USA","type":"territories"}}}}}`))
		case "/v1/appPriceSchedules":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Errorf("decode POST: %v", err)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"S2","type":"appPriceSchedules"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	result, err := setPricing(context.Background(), fixtureASCClient(t, srv), "com.example.app", "USA", "NEW", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ScheduleID != "S2" {
		t.Fatalf("wrong result %+v", result)
	}
	for _, row := range posted.Included {
		if row.Relationships.Point.Data.ID == "NEW" {
			if row.Attributes.Start != start || row.Attributes.End != end {
				t.Fatalf("requested dates lost: %+v", row.Attributes)
			}
			return
		}
	}
	t.Fatal("requested price absent from POST")
}
