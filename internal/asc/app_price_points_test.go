package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF5B_ListAppPricePointsPaginatesAndFiltersTerritory(t *testing.T) {
	var requests int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requests++
		if r.URL.Path != "/v1/apps/APP-1/appPricePoints" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if requests == 1 {
			if r.URL.Query().Get("filter[territory]") != "USA" || r.URL.Query().Get("limit") != "200" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("fields[appPricePoints]") != "customerPrice,proceeds,territory" {
				t.Errorf("fields = %q", r.URL.Query().Get("fields[appPricePoints]"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":  []any{appPricePointFixture("PP-1", "USA", "0.99", "0.70")},
				"links": map[string]string{"next": srv.URL + "/v1/apps/APP-1/appPricePoints?cursor=next"},
			})
			return
		}
		if r.URL.Query().Get("cursor") != "next" {
			t.Errorf("cursor = %q", r.URL.Query().Get("cursor"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []any{appPricePointFixture("PP-2", "USA", "1.99", "1.39")},
			"links": map[string]string{},
		})
	}))
	t.Cleanup(srv.Close)

	points, err := ListAppPricePoints(context.Background(), fixtureClient(t, srv), "APP-1", " USA ")
	if err != nil {
		t.Fatalf("ListAppPricePoints: %v", err)
	}
	if len(points) != 2 || points[0].ID != "PP-1" || points[1].CustomerPrice != "1.99" || requests != 2 {
		t.Fatalf("points = %#v, requests = %d", points, requests)
	}
}

func TestF5B_ListAppPricePointEqualizationsUsesDirectEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v3/appPricePoints/PP-SOURCE/equalizations" || r.URL.Query().Get("filter[territory]") != "GBR" {
			t.Errorf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []any{appPricePointFixture("PP-GBR", "GBR", "0.99", "0.68")},
			"links": map[string]string{},
		})
	}))
	t.Cleanup(srv.Close)

	points, err := ListAppPricePointEqualizations(context.Background(), fixtureClient(t, srv), "PP-SOURCE", "GBR")
	if err != nil {
		t.Fatalf("ListAppPricePointEqualizations: %v", err)
	}
	if len(points) != 1 || points[0].TerritoryID != "GBR" || points[0].Proceeds != "0.68" {
		t.Fatalf("points = %#v", points)
	}
}

func appPricePointFixture(id, territoryID, customerPrice, proceeds string) map[string]any {
	return map[string]any{
		"type": "appPricePoints",
		"id":   id,
		"attributes": map[string]any{
			"customerPrice": customerPrice,
			"proceeds":      proceeds,
		},
		"relationships": map[string]any{
			"territory": map[string]any{"data": map[string]any{"type": "territories", "id": territoryID}},
		},
	}
}
