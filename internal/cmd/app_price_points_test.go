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

func TestF5B_AppPricePointListCommandDiscoversFilteredPoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/APP-1/appPricePoints":
			if r.URL.Query().Get("filter[territory]") != "USA" {
				t.Errorf("territory filter = %q", r.URL.Query().Get("filter[territory]"))
			}
			_, _ = w.Write([]byte(`{"data":[{"type":"appPricePoints","id":"PP-USA-1","attributes":{"customerPrice":"0.99","proceeds":"0.70"},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	command := newAppPricePointsCommand()
	command.SetContext(context.Background())
	var output bytes.Buffer
	command.SetOut(&output)
	if err := runAppPricePointsListWithClient(command, []string{"com.example.app"}, fixtureASCClient(t, srv), "USA", "json"); err != nil {
		t.Fatalf("runAppPricePointsListWithClient: %v", err)
	}
	var result AppPricePointList
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, output.String())
	}
	if result.BundleID != "com.example.app" || result.TerritoryID != "USA" || len(result.PricePoints) != 1 || result.PricePoints[0].ID != "PP-USA-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestF5B_AppPricePointEqualizationsCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v3/appPricePoints/PP-USA-1/equalizations" || r.URL.Query().Get("filter[territory]") != "GBR" {
			t.Errorf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appPricePoints","id":"PP-GBR-1","attributes":{"customerPrice":"0.89","proceeds":"0.60"},"relationships":{"territory":{"data":{"type":"territories","id":"GBR"}}}}],"links":{}}`))
	}))
	t.Cleanup(srv.Close)

	command := newAppPricePointsCommand()
	command.SetContext(context.Background())
	var output bytes.Buffer
	command.SetOut(&output)
	if err := runAppPricePointEqualizationsWithClient(command, []string{"PP-USA-1"}, fixtureASCClient(t, srv), "GBR", "json"); err != nil {
		t.Fatalf("runAppPricePointEqualizationsWithClient: %v", err)
	}
	if !strings.Contains(output.String(), `"pricePointId": "PP-USA-1"`) || !strings.Contains(output.String(), `"id": "PP-GBR-1"`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestF5B_AppPricePointConstructorsAndEmptyList(t *testing.T) {
	group := newAppPricePointsCommand()
	found := map[string]bool{}
	for _, child := range group.Commands() {
		found[child.Name()] = true
	}
	if group.Name() != "price-points" || !found["list"] || !found["equalizations"] {
		t.Fatalf("command tree = %#v", group)
	}
	encoded, err := json.Marshal(AppPricePointList{PricePoints: []AppPricePointView{}})
	if err != nil || !strings.Contains(string(encoded), `"pricePoints":[]`) {
		t.Fatalf("encoded = %s, err = %v", encoded, err)
	}
}
