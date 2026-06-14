package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestPerformanceView_JSONShape(t *testing.T) {
	v := PerformanceView{
		BundleID: "com.example.alpha",
		Version:  "1.0",
		Insights: &asc.PerfPowerMetricInsights{
			Regressions: []asc.PerfPowerInsight{{MetricCategory: "HANG", HighImpact: true, SummaryString: "regressed"}},
		},
		ProductData: []asc.PerfPowerProductData{
			{Platform: "IOS", MetricCategories: []asc.PerfPowerMetricCategory{{Identifier: "HANG"}}},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.alpha"`,
		`"version":"1.0"`,
		`"insights":{`,
		`"productData":[`,
		`"platform":"IOS"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestPerformanceView_TableRows_Vertical(t *testing.T) {
	v := &PerformanceView{
		BundleID: "com.example.alpha",
		Version:  "1.0",
		Insights: &asc.PerfPowerMetricInsights{
			Regressions: []asc.PerfPowerInsight{{MetricCategory: "HANG", HighImpact: true, SummaryString: "regressed"}},
		},
		ProductData: []asc.PerfPowerProductData{
			{
				Platform: "IOS",
				MetricCategories: []asc.PerfPowerMetricCategory{
					{Identifier: "HANG", Metrics: []asc.PerfPowerMetricSnapshot{{Identifier: "hangs.duration"}}},
				},
			},
		},
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	foundCat := false
	for _, r := range rows {
		if strings.Contains(r[0], "HANG") {
			foundCat = true
		}
	}
	if !foundCat {
		t.Error("expected a HANG row in the table output")
	}
}

func TestBoolImpact(t *testing.T) {
	if got := boolImpact(true); got != "HIGH" {
		t.Errorf("boolImpact(true) = %q, want HIGH", got)
	}
	if got := boolImpact(false); got != "LOW" {
		t.Errorf("boolImpact(false) = %q, want LOW", got)
	}
}

func TestPerfPowerQuery(t *testing.T) {
	q := perfPowerQuery("IOS", "HANG", "iPhone15,3")
	if q.Get("filter[platform]") != "IOS" {
		t.Errorf("filter[platform] = %q", q.Get("filter[platform]"))
	}
	if q.Get("filter[metricType]") != "HANG" {
		t.Errorf("filter[metricType] = %q", q.Get("filter[metricType]"))
	}
	if q.Get("filter[deviceType]") != "iPhone15,3" {
		t.Errorf("filter[deviceType] = %q", q.Get("filter[deviceType]"))
	}
	// Empty inputs are skipped: defaults to no filter.
	q2 := perfPowerQuery("", "", "")
	if len(q2) != 0 {
		t.Errorf("empty inputs produced query: %v", q2)
	}
}

func TestPerformanceCommand_RegisteredOnRoot(t *testing.T) {
	var p *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "performance" {
			p = c
			break
		}
	}
	if p == nil {
		t.Fatal("performance not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range p.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"app", "build"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("performance subcommand %q missing", want)
		}
	}
}

// TestPerformance_JSONOutputStability_App locks the JSON shape.
func TestPerformance_JSONOutputStability_App(t *testing.T) {
	v := &PerformanceView{
		BundleID: "com.example.alpha",
		Version:  "1.0",
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "version"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q: JSON contract drift", key)
		}
	}
}

// TestPerformance_FixtureReplay_App exercises the app-level fetch.
func TestPerformance_FixtureReplay_App(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/perfPowerMetrics": {File: "performance_app"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	_, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}

	resp, err := asc.Get[asc.PerfPowerMetricsResponse](
		ctx, c, "/v1/apps/1234567890/perfPowerMetrics", url.Values{},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Version != "1.0" {
		t.Errorf("version = %q, want 1.0", resp.Version)
	}
	if resp.Insights == nil || len(resp.Insights.Regressions) != 1 {
		t.Fatalf("regressions len = mismatch")
	}
	if resp.Insights.Regressions[0].MetricCategory != "HANG" {
		t.Errorf("regressions[0].MetricCategory = %q, want HANG", resp.Insights.Regressions[0].MetricCategory)
	}
	if !resp.Insights.Regressions[0].HighImpact {
		t.Errorf("regressions[0].HighImpact should be true")
	}
	if len(resp.ProductData) != 1 {
		t.Fatalf("productData len = %d, want 1", len(resp.ProductData))
	}
	if len(resp.ProductData[0].MetricCategories) != 2 {
		t.Errorf("metricCategories len = %d, want 2", len(resp.ProductData[0].MetricCategories))
	}
}

// TestPerformance_FixtureReplay_Build exercises the build-scoped fetch
// chain (build lookup + perfPowerMetrics).
func TestPerformance_FixtureReplay_Build(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                             {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds":           {File: "testflight_build_lookup"},
		"GET /v1/builds/BUILD-42/perfPowerMetrics": {File: "performance_build"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	_, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		ctx, c, "/v1/apps/1234567890/builds", url.Values{"filter[version]": {"42"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("build lookup: %v", err)
	}
	if len(bpage.Data) != 1 {
		t.Fatalf("build lookup data len = %d, want 1", len(bpage.Data))
	}

	resp, err := asc.Get[asc.PerfPowerMetricsResponse](
		ctx, c, "/v1/builds/BUILD-42/perfPowerMetrics", url.Values{},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(resp.ProductData) != 1 {
		t.Fatalf("productData len = %d, want 1", len(resp.ProductData))
	}
	cats := resp.ProductData[0].MetricCategories
	if len(cats) != 2 {
		t.Fatalf("metricCategories len = %d, want 2", len(cats))
	}
	if cats[0].Identifier != "LAUNCH" {
		t.Errorf("cats[0].Identifier = %q, want LAUNCH", cats[0].Identifier)
	}
	if cats[0].Metrics[0].Unit == nil || cats[0].Metrics[0].Unit.Identifier != "ms" {
		t.Errorf("LAUNCH unit not populated as ms")
	}
}

// TestPerformance_BuildRequiredErrorMessage confirms --build absence is
// reported clearly to the user when running performance build without it.
func TestPerformance_BuildRequiredErrorMessage(t *testing.T) {
	prev := performanceBuildBuild
	t.Cleanup(func() { performanceBuildBuild = prev })

	performanceBuildBuild = ""
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := runPerformanceBuild(cmd, []string{"com.example.alpha"})
	if err == nil {
		t.Fatal("expected error when --build is empty")
	}
	if !strings.Contains(err.Error(), "--build") {
		t.Errorf("error %q does not mention --build", err.Error())
	}
}
