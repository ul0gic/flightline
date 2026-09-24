package cmd

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// AppPricePointView is the stable view of a selectable app price point.
type AppPricePointView struct {
	ID            string `json:"id"`
	TerritoryID   string `json:"territoryId"`
	CustomerPrice string `json:"customerPrice,omitempty"`
	Proceeds      string `json:"proceeds,omitempty"`
}

// AppPricePointList is a price-point discovery response.
type AppPricePointList struct {
	BundleID     string              `json:"bundleId,omitempty"`
	PricePointID string              `json:"pricePointId,omitempty"`
	TerritoryID  string              `json:"territoryId,omitempty"`
	PricePoints  []AppPricePointView `json:"pricePoints"`
}

func (l AppPricePointList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"TERRITORY", "CUSTOMER_PRICE", "PROCEEDS", "ID"}
	rows = make([][]string, 0, len(l.PricePoints))
	for _, point := range l.PricePoints {
		rows = append(rows, []string{point.TerritoryID, point.CustomerPrice, point.Proceeds, point.ID})
	}
	return headers, rows
}

// newAppPricePointsCommand builds price-point discovery commands for lead-owned registration.
func newAppPricePointsCommand() *cobra.Command {
	var listTerritory string
	var equalizationTerritory string
	group := &cobra.Command{
		Use:   "price-points",
		Short: "Discover app price points and equalizations",
	}
	list := &cobra.Command{
		Use:          "list <bundleId>",
		Short:        "List selectable price points for an app",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runAppPricePointsListWithClient(cmd, args, client, listTerritory, outputMode())
		},
	}
	list.Flags().StringVar(&listTerritory, "territory", "", "filter to an App Store territory ID")

	equalizations := &cobra.Command{
		Use:          "equalizations <pricePointId>",
		Short:        "List territory equalizations for a price point",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runAppPricePointEqualizationsWithClient(cmd, args, client, equalizationTerritory, outputMode())
		},
	}
	equalizations.Flags().StringVar(&equalizationTerritory, "territory", "", "filter to an App Store territory ID")

	group.AddCommand(list, equalizations)
	return group
}

func runAppPricePointsListWithClient(cmd *cobra.Command, args []string, client *asc.Client, territoryID, output string) error {
	bundleID := args[0]
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	points, err := asc.ListAppPricePoints(cmd.Context(), client, appID, territoryID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), appPricePointList(bundleID, "", territoryID, points), output, true)
}

func runAppPricePointEqualizationsWithClient(cmd *cobra.Command, args []string, client *asc.Client, territoryID, output string) error {
	pricePointID := args[0]
	points, err := asc.ListAppPricePointEqualizations(cmd.Context(), client, pricePointID, territoryID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), appPricePointList("", pricePointID, territoryID, points), output, true)
}

func appPricePointList(bundleID, pricePointID, territoryID string, points []asc.AppPricePoint) AppPricePointList {
	result := AppPricePointList{
		BundleID:     bundleID,
		PricePointID: pricePointID,
		TerritoryID:  territoryID,
		PricePoints:  make([]AppPricePointView, 0, len(points)),
	}
	for _, point := range points {
		result.PricePoints = append(result.PricePoints, AppPricePointView{
			ID:            point.ID,
			TerritoryID:   point.TerritoryID,
			CustomerPrice: point.CustomerPrice,
			Proceeds:      point.Proceeds,
		})
	}
	slices.SortFunc(result.PricePoints, func(a, b AppPricePointView) int {
		if a.TerritoryID == b.TerritoryID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.TerritoryID, b.TerritoryID)
	})
	return result
}
