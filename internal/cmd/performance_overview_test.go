package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF4C_PerformanceOverviewViewJSON(t *testing.T) {
	value, goal := 3.5, 4.0
	view := PerformanceOverviewView{
		BundleID: "com.example.app",
		PerformanceOverview: asc.PerformanceOverview{
			Version: "1",
			Categories: []asc.PerformanceOverviewCategory{{
				Identifier:  "STORAGE",
				DisplayName: "Storage",
				Sections: []asc.PerformanceOverviewSection{{
					Identifier: "diskWriteBytes",
					Datasets: []asc.PerformanceOverviewDataset{{
						Points:                []asc.PerformanceOverviewPoint{{Version: "2.1", Value: &value}},
						RecommendedMetricGoal: &asc.PerformanceOverviewMetricGoal{Value: &goal, Detail: "lower is better"},
					}},
				}},
			}},
		},
	}
	var out bytes.Buffer
	if err := renderTo(&out, view, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON output: %v", err)
	}
	if got["bundleId"] != "com.example.app" || got["version"] != "1" {
		t.Fatalf("top-level output = %#v", got)
	}
	if strings.Contains(out.String(), "metricGoal") || !strings.Contains(out.String(), `"recommendedMetricGoal"`) {
		t.Fatalf("output changed the schema field name: %s", out.String())
	}
}

func TestF4C_PerformanceOverviewViewTableAndEmptyData(t *testing.T) {
	pointOne, pointTwo, goal := 2.0, 3.0, 5.0
	view := PerformanceOverviewView{BundleID: "com.example.app", PerformanceOverview: asc.PerformanceOverview{
		Categories: []asc.PerformanceOverviewCategory{{Identifier: "STORAGE", DisplayName: "Storage", Sections: []asc.PerformanceOverviewSection{{Identifier: "diskWriteBytes", Datasets: []asc.PerformanceOverviewDataset{{Points: []asc.PerformanceOverviewPoint{{Value: &pointOne}, {Value: &pointTwo}}, RecommendedMetricGoal: &asc.PerformanceOverviewMetricGoal{Value: &goal}}}}}}},
	}}
	headers, rows := view.TableRows()
	if len(headers) != 3 || len(rows) != 3 || rows[2][2] != "2 points, 1 recommended goals" {
		t.Fatalf("table = %#v / %#v", headers, rows)
	}

	empty := PerformanceOverviewView{BundleID: "com.example.app", PerformanceOverview: asc.PerformanceOverview{Categories: []asc.PerformanceOverviewCategory{}}}
	_, rows = empty.TableRows()
	if len(rows) != 2 || rows[1][2] != "No performance overview data available" {
		t.Fatalf("empty table rows = %#v", rows)
	}
}

func TestF4C_PerformanceOverviewCommandFetchesAndRenders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps":
			if r.URL.Query().Get("filter[bundleId]") != "com.example.app" {
				t.Errorf("bundle filter = %q", r.URL.Query().Get("filter[bundleId]"))
			}
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"123","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/123/performanceOverviews":
			if r.URL.Query().Get("filter[deviceType]") != "iPhone15,3" {
				t.Errorf("device filter = %q", r.URL.Query().Get("filter[deviceType]"))
			}
			_, _ = w.Write([]byte(`{"appMetadata":{"latestVersion":"2.1"},"categories":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := fixtureASCClient(t, srv)
	cmd := newPerformanceOverviewCommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runPerformanceOverviewWithClient(cmd, []string{"com.example.app"}, client, []string{"iPhone15,3"}, "json"); err != nil {
		t.Fatalf("runPerformanceOverviewWithClient: %v", err)
	}
	if !strings.Contains(output.String(), `"latestVersion": "2.1"`) || !strings.Contains(output.String(), `"categories": []`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestF4C_PerformanceOverviewConstructor(t *testing.T) {
	cmd := newPerformanceOverviewCommand()
	if cmd.Use != "overview <bundleId>" || cmd.Flags().Lookup("device-type") == nil {
		t.Fatalf("command = %q, device-type flag = %v", cmd.Use, cmd.Flags().Lookup("device-type"))
	}
}
