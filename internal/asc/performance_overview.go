package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const performanceOverviewMediaType = "application/vnd.apple.xcode-overview+json"

// PerformanceOverview is Apple's app-level performance overview response.
type PerformanceOverview struct {
	Version             string                         `json:"version,omitempty"`
	AppMetadata         PerformanceOverviewApp         `json:"appMetadata,omitempty"`
	Insights            *PerformanceOverviewInsights   `json:"insights,omitempty"`
	Categories          []PerformanceOverviewCategory  `json:"categories"`
	Signatures          *PerformanceOverviewSignatures `json:"signatures,omitempty"`
	TelemetryIdentifier string                         `json:"telemetryIdentifier,omitempty"`
}

// PerformanceOverviewApp identifies the app summarized by the overview.
type PerformanceOverviewApp struct {
	BundleID      string `json:"bundleId,omitempty"`
	AppID         string `json:"appId,omitempty"`
	LatestVersion string `json:"latestVersion,omitempty"`
	Platform      string `json:"platform,omitempty"`
}

// PerformanceOverviewInsights groups Apple's highlighted regressions and improvements.
type PerformanceOverviewInsights struct {
	Regressions []PerformanceOverviewInsight `json:"regressions,omitempty"`
	TrendingUp  []PerformanceOverviewInsight `json:"trendingUp,omitempty"`
}

// PerformanceOverviewInsight is one highlighted metric change.
type PerformanceOverviewInsight struct {
	MetricCategory        string                          `json:"metricCategory,omitempty"`
	LatestVersion         string                          `json:"latestVersion,omitempty"`
	Metric                string                          `json:"metric,omitempty"`
	SummaryString         string                          `json:"summaryString,omitempty"`
	ReferenceVersions     string                          `json:"referenceVersions,omitempty"`
	MaxLatestVersionValue *float64                        `json:"maxLatestVersionValue,omitempty"`
	SubSystemLabel        string                          `json:"subSystemLabel,omitempty"`
	HighImpact            bool                            `json:"highImpact,omitempty"`
	Populations           []PerformanceOverviewPopulation `json:"populations,omitempty"`
}

// PerformanceOverviewPopulation is one insight's device and percentile slice.
type PerformanceOverviewPopulation struct {
	DeltaPercentage       *float64 `json:"deltaPercentage,omitempty"`
	Percentile            string   `json:"percentile,omitempty"`
	SummaryString         string   `json:"summaryString,omitempty"`
	ReferenceAverageValue *float64 `json:"referenceAverageValue,omitempty"`
	LatestVersionValue    *float64 `json:"latestVersionValue,omitempty"`
	Device                string   `json:"device,omitempty"`
}

// PerformanceOverviewCategory groups the overview's metric sections.
type PerformanceOverviewCategory struct {
	Identifier  string                       `json:"identifier,omitempty"`
	DisplayName string                       `json:"displayName,omitempty"`
	Sections    []PerformanceOverviewSection `json:"sections,omitempty"`
}

// PerformanceOverviewSection describes one metric within a category.
type PerformanceOverviewSection struct {
	Identifier     string                       `json:"identifier,omitempty"`
	DisplayName    string                       `json:"displayName,omitempty"`
	RelevanceScore *float64                     `json:"relevanceScore,omitempty"`
	SortOrder      *int                         `json:"sortOrder,omitempty"`
	Unit           *PerformanceOverviewUnit     `json:"unit,omitempty"`
	Datasets       []PerformanceOverviewDataset `json:"datasets,omitempty"`
}

// PerformanceOverviewUnit describes the values in a metric section.
type PerformanceOverviewUnit struct {
	Identifier  string `json:"identifier,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// PerformanceOverviewDataset contains filtered metric points and its recommended goal.
type PerformanceOverviewDataset struct {
	FilterCriteria        PerformanceOverviewFilterCriteria `json:"filterCriteria,omitempty"`
	Points                []PerformanceOverviewPoint        `json:"points,omitempty"`
	RecommendedMetricGoal *PerformanceOverviewMetricGoal    `json:"recommendedMetricGoal,omitempty"`
}

// PerformanceOverviewFilterCriteria identifies a dataset's device and percentile.
type PerformanceOverviewFilterCriteria struct {
	Percentile          string `json:"percentile,omitempty"`
	Device              string `json:"device,omitempty"`
	DeviceMarketingName string `json:"deviceMarketingName,omitempty"`
}

// PerformanceOverviewPoint is one metric value associated with an app version.
type PerformanceOverviewPoint struct {
	Version             string                                  `json:"version,omitempty"`
	Value               *float64                                `json:"value,omitempty"`
	ErrorMargin         *float64                                `json:"errorMargin,omitempty"`
	PercentageBreakdown *PerformanceOverviewPercentageBreakdown `json:"percentageBreakdown,omitempty"`
}

// PerformanceOverviewPercentageBreakdown identifies a point's subsystem share.
type PerformanceOverviewPercentageBreakdown struct {
	Value          *float64 `json:"value,omitempty"`
	SubSystemLabel string   `json:"subSystemLabel,omitempty"`
}

// PerformanceOverviewMetricGoal is Apple's recommended threshold and explanation.
type PerformanceOverviewMetricGoal struct {
	Value  *float64 `json:"value,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// PerformanceOverviewSignatures groups the prominent performance signatures.
type PerformanceOverviewSignatures struct {
	TopHangPoint      []PerformanceOverviewSignature `json:"topHangPoint,omitempty"`
	TopLaunchPoint    []PerformanceOverviewSignature `json:"topLaunchPoint,omitempty"`
	TopDiskWritePoint []PerformanceOverviewSignature `json:"topDiskWritePoint,omitempty"`
}

// PerformanceOverviewSignature identifies one sampled performance signature.
type PerformanceOverviewSignature struct {
	SignatureID    string                             `json:"signatureId,omitempty"`
	Signature      string                             `json:"signature,omitempty"`
	Count          *int                               `json:"count,omitempty"`
	Weight         *float64                           `json:"weight,omitempty"`
	SourceFile     string                             `json:"sourceFile,omitempty"`
	LineNumber     *int                               `json:"lineNumber,omitempty"`
	TrendInfo      string                             `json:"trendInfo,omitempty"`
	MetricsSummary *PerformanceOverviewMetricsSummary `json:"metricsSummary,omitempty"`
}

// PerformanceOverviewMetricsSummary contains signature reference-version values.
type PerformanceOverviewMetricsSummary struct {
	ReferenceVersions []PerformanceOverviewVersionValue `json:"referenceVersions,omitempty"`
}

// PerformanceOverviewVersionValue is a signature metric value for one version.
type PerformanceOverviewVersionValue struct {
	Version string   `json:"version,omitempty"`
	Value   *float64 `json:"value,omitempty"`
}

// FetchPerformanceOverview reads Apple's custom xcode overview response.
func (c *Client) FetchPerformanceOverview(ctx context.Context, appID string, deviceTypes []string) (PerformanceOverview, error) {
	var zero PerformanceOverview
	if strings.TrimSpace(appID) == "" {
		return zero, errors.New("asc: performance overview app id is required")
	}
	query := url.Values{}
	if len(deviceTypes) > 0 {
		values := make([]string, 0, len(deviceTypes))
		for _, device := range deviceTypes {
			if device = strings.TrimSpace(device); device != "" {
				values = append(values, device)
			}
		}
		if len(values) > 0 {
			query.Set("filter[deviceType]", strings.Join(values, ","))
		}
	}
	resp, err := c.do(ctx, http.MethodGet, "/v1/apps/"+url.PathEscape(appID)+"/performanceOverviews", query, nil, performanceOverviewMediaType)
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, c.errorFromResponse(resp)
	}
	var out PerformanceOverview
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("asc: decode performance overview response: %w", err)
	}
	if out.Categories == nil {
		out.Categories = []PerformanceOverviewCategory{}
	}
	return out, nil
}
