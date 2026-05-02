package asc

// Performance metrics read surface (Xcode Organizer metrics).
//
// Apple ships the same metrics the Xcode Organizer "Metrics" tab shows via
// the perfPowerMetrics endpoints — battery, memory, hangs, launches,
// disk-writes, terminations, animation hitches.
//
//	GET /v1/apps/{id}/perfPowerMetrics    — app-level (cross-build aggregate)
//	GET /v1/builds/{id}/perfPowerMetrics  — build-specific
//
// Both endpoints return Apple's `xcodeMetrics` envelope (NOT JSON:API):
// a `version` string, an `insights` block summarizing trending-up /
// regressions, and a `productData` array carrying per-platform metric
// categories. Apple's response Content-Type is
// `application/vnd.apple.xcode-metrics+json`; standard application/json
// decoding works against the same body.
//
// Source:
//
//	jq '.components.schemas.xcodeMetrics' openapi.oas.json
//	jq '.components.schemas.MetricsInsight' openapi.oas.json
//	jq '.components.schemas.MetricCategory' openapi.oas.json

// Apple-defined MetricCategory values, surfaced as named constants for
// command code that filters / compares.
const (
	MetricCategoryHang        = "HANG"
	MetricCategoryLaunch      = "LAUNCH"
	MetricCategoryMemory      = "MEMORY"
	MetricCategoryDisk        = "DISK"
	MetricCategoryBattery     = "BATTERY"
	MetricCategoryTermination = "TERMINATION"
	MetricCategoryAnimation   = "ANIMATION"
)

// PerfPowerMetricsResponse is Apple's xcodeMetrics envelope.
type PerfPowerMetricsResponse struct {
	Version     string                   `json:"version,omitempty"`
	Insights    *PerfPowerMetricInsights `json:"insights,omitempty"`
	ProductData []PerfPowerProductData   `json:"productData,omitempty"`
}

// PerfPowerMetricInsights groups Apple's trending-up and regression call-outs.
type PerfPowerMetricInsights struct {
	TrendingUp  []PerfPowerInsight `json:"trendingUp,omitempty"`
	Regressions []PerfPowerInsight `json:"regressions,omitempty"`
}

// PerfPowerInsight is one regression / trending-up entry.
type PerfPowerInsight struct {
	MetricCategory        string                       `json:"metricCategory,omitempty"`
	LatestVersion         string                       `json:"latestVersion,omitempty"`
	Metric                string                       `json:"metric,omitempty"`
	SummaryString         string                       `json:"summaryString,omitempty"`
	ReferenceVersions     string                       `json:"referenceVersions,omitempty"`
	MaxLatestVersionValue float64                      `json:"maxLatestVersionValue,omitempty"`
	SubSystemLabel        string                       `json:"subSystemLabel,omitempty"`
	HighImpact            bool                         `json:"highImpact,omitempty"`
	Populations           []PerfPowerInsightPopulation `json:"populations,omitempty"`
}

// PerfPowerInsightPopulation is one device / percentile slice of an
// insight (e.g. p90 on iPhone 15 Pro).
type PerfPowerInsightPopulation struct {
	DeltaPercentage       float64 `json:"deltaPercentage,omitempty"`
	Percentile            string  `json:"percentile,omitempty"`
	SummaryString         string  `json:"summaryString,omitempty"`
	ReferenceAverageValue float64 `json:"referenceAverageValue,omitempty"`
	LatestVersionValue    float64 `json:"latestVersionValue,omitempty"`
	Device                string  `json:"device,omitempty"`
}

// PerfPowerProductData groups metric categories under one platform (typically
// IOS). Apple v4.3 only emits IOS for perfPowerMetrics.
type PerfPowerProductData struct {
	Platform         string                    `json:"platform,omitempty"`
	MetricCategories []PerfPowerMetricCategory `json:"metricCategories,omitempty"`
}

// PerfPowerMetricCategory groups metrics under a category (HANG, LAUNCH, …).
type PerfPowerMetricCategory struct {
	Identifier string                    `json:"identifier,omitempty"`
	Metrics    []PerfPowerMetricSnapshot `json:"metrics,omitempty"`
}

// PerfPowerMetricSnapshot is one metric (e.g. memory.peak) with its goal
// bounds, unit, and dataset payload. Datasets are kept as raw JSON-shaped
// maps to avoid pinning the deeply-nested numeric arrays Apple revises
// across spec versions; consumers that need them decode further on demand.
type PerfPowerMetricSnapshot struct {
	Identifier string                `json:"identifier,omitempty"`
	GoalKeys   []PerfPowerMetricGoal `json:"goalKeys,omitempty"`
	Unit       *PerfPowerMetricUnit  `json:"unit,omitempty"`
	Datasets   []map[string]any      `json:"datasets,omitempty"`
}

// PerfPowerMetricGoal is one goal-key bracket (Apple's "good / OK / bad"
// thresholds for a metric).
type PerfPowerMetricGoal struct {
	GoalKey    string `json:"goalKey,omitempty"`
	LowerBound int    `json:"lowerBound,omitempty"`
	UpperBound int    `json:"upperBound,omitempty"`
}

// PerfPowerMetricUnit describes a metric's unit (e.g. "ms", "MB").
type PerfPowerMetricUnit struct {
	Identifier  string `json:"identifier,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}
