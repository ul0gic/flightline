package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5B_AppAvailabilityCommandReadsTerritoriesAndBlockers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1","attributes":{"availableInNewTerritories":false}}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[
				{"type":"territoryAvailabilities","id":"TA-USA","attributes":{"available":true,"contentStatuses":["AVAILABLE"]},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}},
				{"type":"territoryAvailabilities","id":"TA-GBR","attributes":{"available":false,"releaseDate":"2027-01-02","preOrderEnabled":true,"preOrderPublishDate":"2026-12-02","contentStatuses":["MISSING_RATING"]},"relationships":{"territory":{"data":{"type":"territories","id":"GBR"}}}}
			],"links":{}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	command := newAppAvailabilityCommand()
	command.SetContext(context.Background())
	var output bytes.Buffer
	command.SetOut(&output)
	if err := runAppAvailabilityWithClient(command, []string{"com.example.app"}, fixtureASCClient(t, srv), "json"); err != nil {
		t.Fatalf("runAppAvailabilityWithClient: %v", err)
	}
	var result AppAvailabilityView
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, output.String())
	}
	if result.ID != "AVAIL-1" || len(result.Territories) != 2 || result.Territories[0].TerritoryID != "GBR" {
		t.Fatalf("result = %#v", result)
	}
	if result.Territories[0].ContentStatuses[0] != "MISSING_RATING" || result.Territories[0].PreOrderEnabled == nil || !*result.Territories[0].PreOrderEnabled {
		t.Fatalf("blocker state = %#v", result.Territories[0])
	}
}

func TestF5B_AppAvailabilityEmptyTerritoriesRemainAnArray(t *testing.T) {
	encoded, err := json.Marshal(AppAvailabilityView{BundleID: "com.example.app", Territories: []TerritoryAvailabilityView{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"territories":[]`) {
		t.Fatalf("encoded = %s", encoded)
	}
}

func TestF5B_AppAvailabilityConstructor(t *testing.T) {
	group := newAppAvailabilityCommand()
	found := map[string]bool{}
	for _, child := range group.Commands() {
		found[child.Name()] = true
	}
	if group.Name() != "app-availability" || !found["get"] || !found["preorders"] {
		t.Fatalf("command tree = %#v", group)
	}
}
