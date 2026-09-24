package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestF4C_FetchPerformanceOverviewDecodesSchemaFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/123/performanceOverviews" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != performanceOverviewMediaType {
			t.Errorf("Accept = %q", got)
		}
		if got := r.URL.Query().Get("filter[deviceType]"); got != "iPhone15,3,iPad14,1" {
			t.Errorf("filter[deviceType] = %q", got)
		}
		w.Header().Set("Content-Type", performanceOverviewMediaType)
		_, _ = w.Write([]byte(`{"version":"1","appMetadata":{"bundleId":"com.example.app","appId":"123","latestVersion":"2.1","platform":"IOS"},"insights":{"regressions":[{"metricCategory":"STORAGE","metric":"diskWriteBytes","highImpact":true,"populations":[{"device":"iPhone15,3"}]}]},"categories":[{"identifier":"STORAGE","displayName":"Storage","sections":[{"identifier":"diskWriteBytes","displayName":"Disk writes","datasets":[{"filterCriteria":{"deviceMarketingName":"iPhone 15 Pro"},"points":[{"version":"2.1","value":4.5,"percentageBreakdown":{"value":1.2,"subSystemLabel":"cache"}}],"recommendedMetricGoal":{"value":3.0,"detail":"lower is better"}}]}]}],"signatures":{"topDiskWritePoint":[{"signatureId":"sig-1","signature":"write()","count":2,"metricsSummary":{"referenceVersions":[{"version":"2.0","value":2.5}]}}]},"telemetryIdentifier":"telemetry-1"}`))
	}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	got, err := c.FetchPerformanceOverview(context.Background(), "123", []string{"iPhone15,3", "iPad14,1"})
	if err != nil {
		t.Fatalf("FetchPerformanceOverview: %v", err)
	}
	if got.AppMetadata.LatestVersion != "2.1" || got.Insights.Regressions[0].MetricCategory != "STORAGE" {
		t.Fatalf("overview metadata/insights = %+v / %+v", got.AppMetadata, got.Insights)
	}
	section := got.Categories[0].Sections[0]
	if section.Datasets[0].RecommendedMetricGoal.Value == nil || *section.Datasets[0].RecommendedMetricGoal.Value != 3 || section.Datasets[0].Points[0].PercentageBreakdown.SubSystemLabel != "cache" {
		t.Fatalf("section data = %+v", section)
	}
	if got.Signatures.TopDiskWritePoint[0].MetricsSummary.ReferenceVersions[0].Version != "2.0" || got.TelemetryIdentifier != "telemetry-1" {
		t.Fatalf("signatures/telemetry = %+v / %q", got.Signatures, got.TelemetryIdentifier)
	}
}

func TestF4C_PerformanceOverviewPreservesPresentZeroValues(t *testing.T) {
	var overview PerformanceOverview
	if err := json.Unmarshal([]byte(`{"categories":[{"sections":[{"relevanceScore":0,"sortOrder":0,"datasets":[{"points":[{"value":0,"errorMargin":0}]}]}]}]}`), &overview); err != nil {
		t.Fatalf("decode: %v", err)
	}
	section := overview.Categories[0].Sections[0]
	point := section.Datasets[0].Points[0]
	if section.RelevanceScore == nil || *section.RelevanceScore != 0 || section.SortOrder == nil || *section.SortOrder != 0 || point.Value == nil || *point.Value != 0 || point.ErrorMargin == nil || *point.ErrorMargin != 0 {
		t.Fatalf("present zero values were lost: %+v / %+v", section, point)
	}
}

func TestF4C_FetchPerformanceOverviewEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"categories":[]}`))
	}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	got, err := c.FetchPerformanceOverview(context.Background(), "123", nil)
	if err != nil {
		t.Fatalf("FetchPerformanceOverview: %v", err)
	}
	if got.Categories == nil || len(got.Categories) != 0 {
		t.Fatalf("categories = %#v, want nonnil empty slice", got.Categories)
	}
}

func TestF4C_FetchPerformanceOverviewAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"status":"403","code":"FORBIDDEN","title":"Access denied","detail":"Not permitted"}]}`))
	}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	_, err := c.FetchPerformanceOverview(context.Background(), "123", nil)
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "Not permitted") {
		t.Fatalf("error = %v, want surfaced API error payload", err)
	}
}

func TestF4C_FetchPerformanceOverviewRequiresAppID(t *testing.T) {
	c := &Client{}
	if _, err := c.FetchPerformanceOverview(context.Background(), " ", nil); err == nil {
		t.Fatal("expected app ID validation error")
	}
}

func TestF4C_PerformanceOverviewDeviceFilterEncoding(t *testing.T) {
	values := url.Values{}
	values.Set("filter[deviceType]", "iPhone15,3,iPad14,1")
	if got := values.Encode(); got != "filter%5BdeviceType%5D=iPhone15%2C3%2CiPad14%2C1" {
		t.Fatalf("encoded filter = %q", got)
	}
}
