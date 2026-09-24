package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// EndAppPreordersResult reports the explicitly released preorder territories.
type EndAppPreordersResult struct {
	BundleID    string   `json:"bundleId"`
	Territories []string `json:"territories"`
	RequestID   string   `json:"requestId"`
}

func (r EndAppPreordersResult) TableRows() (headers []string, rows [][]string) {
	return []string{"BUNDLE_ID", "TERRITORIES", "REQUEST_ID"}, [][]string{{r.BundleID, strings.Join(r.Territories, ","), r.RequestID}}
}

func newAppPreordersCommand() *cobra.Command {
	var territories []string
	var confirmed bool
	group := &cobra.Command{
		Use:   "preorders",
		Short: "Manage explicit app pre-order actions",
	}
	end := &cobra.Command{
		Use:          "end <bundleId>",
		Short:        "End active pre-orders and release immediately",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Long: `End active pre-orders for the selected territories and release the app immediately.

This action cannot be reconciled through state apply. Flightline re-reads each selected territory and requires an active pre-order before sending the request.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runEndAppPreordersWithClient(cmd, args, client, territories, confirmed, outputMode())
		},
	}
	end.Flags().StringArrayVar(&territories, "territory", nil, "App Store territory ID to release (repeatable)")
	end.Flags().BoolVar(&confirmed, "confirm", false, "confirm immediate release in the selected territories")
	_ = end.MarkFlagRequired("territory")
	_ = end.MarkFlagRequired("confirm")
	group.AddCommand(end)
	return group
}

func runEndAppPreordersWithClient(cmd *cobra.Command, args []string, client *asc.Client, territoryIDs []string, confirmed bool, output string) error {
	if !confirmed {
		return errors.New("app preorders: pass --confirm to end pre-orders and release immediately")
	}
	selected, err := normalizePreorderTerritories(territoryIDs)
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), client, args[0])
	if err != nil {
		return err
	}
	availability, err := asc.ReadAppAvailability(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	resources := make(map[string]string, len(availability.Territories))
	for _, territory := range availability.Territories {
		if territory.PreOrderEnabled != nil && *territory.PreOrderEnabled {
			resources[territory.TerritoryID] = territory.ID
		}
	}
	availabilityIDs := make([]string, 0, len(selected))
	for _, territoryID := range selected {
		availabilityID, ok := resources[territoryID]
		if !ok {
			return fmt.Errorf("app preorders: territory %q is not an observed active pre-order", territoryID)
		}
		availabilityIDs = append(availabilityIDs, availabilityID)
	}
	requestID, err := asc.EndAppAvailabilityPreOrders(cmd.Context(), client, availabilityIDs)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), EndAppPreordersResult{BundleID: args[0], Territories: selected, RequestID: requestID}, output, true)
}

func normalizePreorderTerritories(territoryIDs []string) ([]string, error) {
	if len(territoryIDs) == 0 {
		return nil, errors.New("app preorders: at least one --territory is required")
	}
	selected := make([]string, 0, len(territoryIDs))
	seen := make(map[string]bool, len(territoryIDs))
	for _, territoryID := range territoryIDs {
		territoryID = strings.TrimSpace(territoryID)
		if territoryID == "" {
			return nil, errors.New("app preorders: territory ID is required")
		}
		if seen[territoryID] {
			return nil, fmt.Errorf("app preorders: territory %q was specified more than once", territoryID)
		}
		seen[territoryID] = true
		selected = append(selected, territoryID)
	}
	sort.Strings(selected)
	return selected, nil
}
