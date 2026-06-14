package asc

// perfPowerMetrics returns Apple's xcodeMetrics envelope (NOT JSON:API); standard JSON decoding works.
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

// PerfPowerMetricSnapshot is one metric (e.g. memory.peak) with goal bounds, unit, and datasets.
// Datasets are raw maps to avoid pinning Apple's deeply-nested numeric arrays across spec revisions.
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
