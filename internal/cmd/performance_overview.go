package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// PerformanceOverviewView adds the caller's bundle ID to Apple's response.
type PerformanceOverviewView struct {
	BundleID string `json:"bundleId"`
	asc.PerformanceOverview
}

// TableRows summarizes overview categories, insights, and signature counts.
func (v PerformanceOverviewView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"TYPE", "NAME", "DETAIL"}
	rows = append(rows, []string{"APP", v.BundleID, v.AppMetadata.LatestVersion})
	if v.Insights != nil {
		rows = append(rows,
			[]string{"INSIGHTS", "REGRESSIONS", strconv.Itoa(len(v.Insights.Regressions))},
			[]string{"INSIGHTS", "TRENDING_UP", strconv.Itoa(len(v.Insights.TrendingUp))},
		)
	}
	for _, category := range v.Categories {
		rows = append(rows, []string{"CATEGORY", category.Identifier, category.DisplayName})
		for _, section := range category.Sections {
			pointCount, goalCount := 0, 0
			for _, dataset := range section.Datasets {
				pointCount += len(dataset.Points)
				if dataset.RecommendedMetricGoal != nil {
					goalCount++
				}
			}
			rows = append(rows, []string{"SECTION", section.Identifier, fmt.Sprintf("%d points, %d recommended goals", pointCount, goalCount)})
		}
	}
	if v.Signatures != nil {
		rows = append(rows,
			[]string{"SIGNATURES", "HANG", strconv.Itoa(len(v.Signatures.TopHangPoint))},
			[]string{"SIGNATURES", "LAUNCH", strconv.Itoa(len(v.Signatures.TopLaunchPoint))},
			[]string{"SIGNATURES", "DISK_WRITE", strconv.Itoa(len(v.Signatures.TopDiskWritePoint))},
		)
	}
	if len(rows) == 1 && v.AppMetadata.LatestVersion == "" && len(v.Categories) == 0 && (v.Insights == nil || len(v.Insights.Regressions)+len(v.Insights.TrendingUp) == 0) && (v.Signatures == nil || len(v.Signatures.TopHangPoint)+len(v.Signatures.TopLaunchPoint)+len(v.Signatures.TopDiskWritePoint) == 0) {
		rows = append(rows, []string{"DATA", "", "No performance overview data available"})
	}
	return headers, rows
}

func newPerformanceOverviewCommand() *cobra.Command {
	var deviceTypes []string
	cmd := &cobra.Command{
		Use:          "overview <bundleId>",
		Short:        "Read app performance overview",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runPerformanceOverviewWithClient(cmd, args, client, deviceTypes, outputMode())
		},
		Example: `  flightline performance overview com.example.myapp
  flightline performance overview com.example.myapp --device-type iPhone15,3 --output json`,
	}
	cmd.Flags().StringSliceVar(&deviceTypes, "device-type", nil, "filter by device type (Apple model id)")
	return cmd
}

func runPerformanceOverviewWithClient(cmd *cobra.Command, args []string, client *asc.Client, deviceTypes []string, output string) error {
	bundleID := args[0]
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	overview, err := client.FetchPerformanceOverview(cmd.Context(), appID, deviceTypes)
	if err != nil {
		return err
	}
	view := PerformanceOverviewView{BundleID: bundleID, PerformanceOverview: overview}
	return renderTo(cmd.OutOrStdout(), view, output, true)
}
