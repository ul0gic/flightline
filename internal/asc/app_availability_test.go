package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5B_ReadAppAvailabilityPaginatesTerritories(t *testing.T) {
	var requests int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveF5BAppAvailabilityRequest(t, w, r, &requests, srv.URL)
	}))
	t.Cleanup(srv.Close)

	availability, err := ReadAppAvailability(context.Background(), fixtureClient(t, srv), "APP-1")
	if err != nil {
		t.Fatalf("ReadAppAvailability: %v", err)
	}
	if availability.ID != "AVAIL-1" || availability.AvailableInNewTerritories == nil || !*availability.AvailableInNewTerritories {
		t.Fatalf("availability = %#v", availability)
	}
	if len(availability.Territories) != 2 || availability.Territories[1].TerritoryID != "GBR" || availability.Territories[1].ContentStatuses[0] != "MISSING_RATING" {
		t.Fatalf("territories = %#v", availability.Territories)
	}
	if requests != 2 {
		t.Fatalf("territory requests = %d, want 2", requests)
	}
}

func serveF5BAppAvailabilityRequest(t *testing.T, w http.ResponseWriter, r *http.Request, requests *int, serverURL string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/apps/APP-1/appAvailabilityV2" {
		if r.URL.Query().Get("fields[appAvailabilities]") != "availableInNewTerritories" {
			t.Errorf("fields[appAvailabilities] = %q", r.URL.Query().Get("fields[appAvailabilities]"))
		}
		_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1","attributes":{"availableInNewTerritories":true}}}`))
		return
	}
	if r.URL.Path == "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities" {
		serveF5BAvailabilityTerritories(t, w, r, requests, serverURL)
		return
	}
	t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	http.NotFound(w, r)
}

func serveF5BAvailabilityTerritories(t *testing.T, w http.ResponseWriter, r *http.Request, requests *int, serverURL string) {
	t.Helper()
	(*requests)++
	if *requests == 1 {
		if r.URL.Query().Get("fields[territoryAvailabilities]") != "available,releaseDate,preOrderEnabled,preOrderPublishDate,contentStatuses,territory" {
			t.Errorf("territory fields = %q", r.URL.Query().Get("fields[territoryAvailabilities]"))
		}
		if r.URL.Query().Get("limit") != "200" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []any{territoryAvailabilityFixture("TA-USA", "USA", true, nil)},
			"links": map[string]string{"next": serverURL + "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities?cursor=next"},
		})
		return
	}
	if r.URL.Query().Get("cursor") != "next" {
		t.Errorf("cursor = %q", r.URL.Query().Get("cursor"))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":  []any{territoryAvailabilityFixture("TA-GBR", "GBR", false, []string{"MISSING_RATING"})},
		"links": map[string]string{},
	})
}

func TestF5B_ReadAppAvailabilityRejectsMissingTerritoryRelationship(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1"}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[{"type":"territoryAvailabilities","id":"TA-1","attributes":{"available":true}}],"links":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	_, err := ReadAppAvailability(context.Background(), fixtureClient(t, srv), "APP-1")
	if err == nil || !strings.Contains(err.Error(), "missing territory relationship") {
		t.Fatalf("ReadAppAvailability error = %v", err)
	}
}

func TestF5B_ReadAppAvailabilityRejectsDuplicateTerritory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1","attributes":{"availableInNewTerritories":false}}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{
					territoryAvailabilityFixture("TA-USA-1", "USA", true, nil),
					territoryAvailabilityFixture("TA-USA-2", "USA", false, nil),
				},
				"links": map[string]string{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	_, err := ReadAppAvailability(context.Background(), fixtureClient(t, srv), "APP-1")
	if err == nil || !strings.Contains(err.Error(), "duplicate territory") {
		t.Fatalf("ReadAppAvailability error = %v", err)
	}
}

func territoryAvailabilityFixture(id, territoryID string, available bool, statuses []string) map[string]any {
	return map[string]any{
		"type": "territoryAvailabilities",
		"id":   id,
		"attributes": map[string]any{
			"available":       available,
			"contentStatuses": statuses,
		},
		"relationships": map[string]any{
			"territory": map[string]any{"data": map[string]any{"type": "territories", "id": territoryID}},
		},
	}
}
