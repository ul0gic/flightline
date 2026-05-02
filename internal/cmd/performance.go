package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// PerformanceView is the read-side view for `performance app|build`.
//
// Surfaces Apple's xcodeMetrics envelope keyed by bundleId (and optional
// buildNumber for the build-scoped variant). Insights and ProductData are
// passed through as Apple shaped them so JSON consumers see the wire shape
// they'd see from a curl against ASC.
type PerformanceView struct {
	BundleID    string                       `json:"bundleId"`
	BuildNumber string                       `json:"buildNumber,omitempty"`
	BuildID     string                       `json:"buildId,omitempty"`
	Version     string                       `json:"version,omitempty"`
	Insights    *asc.PerfPowerMetricInsights `json:"insights,omitempty"`
	ProductData []asc.PerfPowerProductData   `json:"productData,omitempty"`
	Note        string                       `json:"note,omitempty"`
}

// TableRows for the performance view. Vertical layout summarizes the
// metric categories and any insight call-outs; consumers wanting the full
// dataset payload must use --output json (the dataset arrays are too deep
// to fit in a tabular view).
func (v *PerformanceView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
	}
	if v.BuildNumber != "" {
		rows = append(rows, []string{"BUILD_NUMBER", v.BuildNumber})
	}
	if v.BuildID != "" {
		rows = append(rows, []string{"BUILD_ID", v.BuildID})
	}
	rows = append(rows, []string{"VERSION", v.Version})
	if v.Note != "" {
		rows = append(rows, []string{"NOTE", v.Note})
	}

	if v.Insights != nil {
		if n := len(v.Insights.Regressions); n > 0 {
			rows = append(rows, []string{"REGRESSIONS", fmt.Sprintf("%d", n)})
			for i := range v.Insights.Regressions {
				ins := &v.Insights.Regressions[i]
				rows = append(rows, []string{
					"  " + ins.MetricCategory + " (" + boolImpact(ins.HighImpact) + ")",
					truncTitle(ins.SummaryString, 80),
				})
			}
		}
		if n := len(v.Insights.TrendingUp); n > 0 {
			rows = append(rows, []string{"TRENDING_UP", fmt.Sprintf("%d", n)})
			for i := range v.Insights.TrendingUp {
				ins := &v.Insights.TrendingUp[i]
				rows = append(rows, []string{
					"  " + ins.MetricCategory + " (" + boolImpact(ins.HighImpact) + ")",
					truncTitle(ins.SummaryString, 80),
				})
			}
		}
	}

	for i := range v.ProductData {
		pd := &v.ProductData[i]
		rows = append(rows, []string{"PLATFORM", pd.Platform})
		for j := range pd.MetricCategories {
			cat := &pd.MetricCategories[j]
			rows = append(rows, []string{
				"  " + cat.Identifier,
				fmt.Sprintf("%d metrics", len(cat.Metrics)),
			})
		}
	}
	return headers, rows
}

// boolImpact renders a high-impact flag as a short label so the table cell
// stays narrow.
func boolImpact(high bool) string {
	if high {
		return "HIGH"
	}
	return "LOW"
}

var performanceCmd = &cobra.Command{
	Use:   "performance",
	Short: "Read Xcode Organizer performance metrics",
	Long: `performance groups read commands over Apple's perfPowerMetrics
endpoints — the same battery / memory / hangs / launches / disk-writes
metrics the Xcode Organizer "Metrics" tab shows.

  - app <bundleId>                    — app-level (cross-build aggregate)
  - build <bundleId> --build <number> — build-specific metrics

Filter by --platform, --category (HANG | LAUNCH | MEMORY | DISK |
BATTERY | TERMINATION | ANIMATION), and --device.`,
}

var performanceAppCmd = &cobra.Command{
	Use:          "app <bundleId>",
	Short:        "Read app-level performance metrics (cross-build aggregate)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPerformanceApp,
	Example: `  skipper performance app com.example.myapp
  skipper performance app com.example.myapp --category MEMORY
  skipper performance app com.example.myapp --output json | jq '.insights.regressions'`,
}

var performanceBuildCmd = &cobra.Command{
	Use:          "build <bundleId>",
	Short:        "Read build-specific performance metrics",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPerformanceBuild,
	Example: `  skipper performance build com.example.myapp --build 42
  skipper performance build com.example.myapp --build 42 --category HANG --output json`,
}

var (
	performanceAppPlatform string
	performanceAppCategory string
	performanceAppDevice   string
	performanceBuildBuild  string
	performanceBuildPlat   string
	performanceBuildCat    string
	performanceBuildDev    string
)

func init() {
	performanceAppCmd.Flags().StringVar(&performanceAppPlatform, "platform", "IOS", "filter by platform (Apple v4.3 only emits IOS)")
	performanceAppCmd.Flags().StringVar(&performanceAppCategory, "category", "", "filter by metric category: HANG | LAUNCH | MEMORY | DISK | BATTERY | TERMINATION | ANIMATION")
	performanceAppCmd.Flags().StringVar(&performanceAppDevice, "device", "", "filter by device type (Apple model id, e.g. iPhone15,3)")

	performanceBuildCmd.Flags().StringVar(&performanceBuildBuild, "build", "", "build number to inspect (CFBundleVersion, e.g. 42)")
	performanceBuildCmd.Flags().StringVar(&performanceBuildPlat, "platform", "IOS", "filter by platform (Apple v4.3 only emits IOS)")
	performanceBuildCmd.Flags().StringVar(&performanceBuildCat, "category", "", "filter by metric category")
	performanceBuildCmd.Flags().StringVar(&performanceBuildDev, "device", "", "filter by device type")
	_ = performanceBuildCmd.MarkFlagRequired("build")

	performanceCmd.AddCommand(performanceAppCmd)
	performanceCmd.AddCommand(performanceBuildCmd)
	rootCmd.AddCommand(performanceCmd)
}

func runPerformanceApp(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	q := perfPowerQuery(performanceAppPlatform, performanceAppCategory, performanceAppDevice)
	resp, err := asc.Get[asc.PerfPowerMetricsResponse](
		cmd.Context(), c, "/v1/apps/"+appID+"/perfPowerMetrics", q,
	)
	if err != nil {
		return err
	}

	view := &PerformanceView{
		BundleID:    bundleID,
		Version:     resp.Version,
		Insights:    resp.Insights,
		ProductData: resp.ProductData,
	}
	if len(resp.ProductData) == 0 && resp.Insights == nil {
		view.Note = "no performance metrics available for this app yet (Apple needs sufficient user telemetry; metrics surface 7-30 days after release)"
	}
	return Render(view, outputMode())
}

func runPerformanceBuild(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	build := strings.TrimSpace(performanceBuildBuild)
	if build == "" {
		return fmt.Errorf("performance: --build is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	bq := url.Values{
		"filter[version]": {build},
		"limit":           {"1"},
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		cmd.Context(), c, "/v1/apps/"+appID+"/builds", bq,
	)
	if err != nil {
		return err
	}
	if len(bpage.Data) == 0 {
		return fmt.Errorf("performance: no build %q found for %q", build, bundleID)
	}
	buildID := bpage.Data[0].ID

	q := perfPowerQuery(performanceBuildPlat, performanceBuildCat, performanceBuildDev)
	resp, err := asc.Get[asc.PerfPowerMetricsResponse](
		cmd.Context(), c, "/v1/builds/"+buildID+"/perfPowerMetrics", q,
	)
	if err != nil {
		return err
	}

	view := &PerformanceView{
		BundleID:    bundleID,
		BuildNumber: build,
		BuildID:     buildID,
		Version:     resp.Version,
		Insights:    resp.Insights,
		ProductData: resp.ProductData,
	}
	if len(resp.ProductData) == 0 && resp.Insights == nil {
		view.Note = "no performance metrics available for this build (Apple may not have collected enough telemetry yet)"
	}
	return Render(view, outputMode())
}

// perfPowerQuery builds the filter[] tuple Apple's perfPowerMetrics
// endpoints accept. Empty inputs are skipped so callers always default to
// "no filter" rather than silently constraining the result.
func perfPowerQuery(platform, category, device string) url.Values {
	q := url.Values{}
	if p := strings.TrimSpace(platform); p != "" {
		q.Set("filter[platform]", p)
	}
	if cat := strings.TrimSpace(category); cat != "" {
		q.Set("filter[metricType]", cat)
	}
	if d := strings.TrimSpace(device); d != "" {
		q.Set("filter[deviceType]", d)
	}
	return q
}
