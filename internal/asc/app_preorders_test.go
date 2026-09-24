package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF5B_EndAppAvailabilityPreOrdersPostsSelectedResources(t *testing.T) {
	var body struct {
		Data struct {
			Type          string `json:"type"`
			Relationships struct {
				TerritoryAvailabilities struct {
					Data []struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"data"`
				} `json:"territoryAvailabilities"`
			} `json:"relationships"`
		} `json:"data"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/endAppAvailabilityPreOrders" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"endAppAvailabilityPreOrders","id":"END-1"}}`))
	}))
	t.Cleanup(srv.Close)

	requestID, err := EndAppAvailabilityPreOrders(context.Background(), fixtureClient(t, srv), []string{"TA-GBR", "TA-USA"})
	if err != nil {
		t.Fatalf("EndAppAvailabilityPreOrders: %v", err)
	}
	if requestID != "END-1" {
		t.Fatalf("request ID = %q", requestID)
	}
	resources := body.Data.Relationships.TerritoryAvailabilities.Data
	if body.Data.Type != "endAppAvailabilityPreOrders" || len(resources) != 2 || resources[0].ID != "TA-GBR" {
		t.Fatalf("body = %#v", body)
	}
}
