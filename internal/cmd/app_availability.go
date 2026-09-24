package cmd

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// AppAvailabilityView is the stable read view of availability across all territories.
type AppAvailabilityView struct {
	BundleID                  string                      `json:"bundleId"`
	ID                        string                      `json:"id"`
	AvailableInNewTerritories *bool                       `json:"availableInNewTerritories,omitempty"`
	Territories               []TerritoryAvailabilityView `json:"territories"`
}

// TerritoryAvailabilityView is one territory's availability and current blockers.
type TerritoryAvailabilityView struct {
	ID                  string   `json:"id"`
	TerritoryID         string   `json:"territoryId"`
	Available           *bool    `json:"available,omitempty"`
	ReleaseDate         string   `json:"releaseDate,omitempty"`
	PreOrderEnabled     *bool    `json:"preOrderEnabled,omitempty"`
	PreOrderPublishDate string   `json:"preOrderPublishDate,omitempty"`
	ContentStatuses     []string `json:"contentStatuses"`
}

func (v AppAvailabilityView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"TERRITORY", "AVAILABLE", "RELEASE_DATE", "PREORDER", "PREORDER_PUBLISH_DATE", "CONTENT_STATUSES", "ID"}
	rows = make([][]string, 0, len(v.Territories))
	for _, territory := range v.Territories {
		rows = append(rows, []string{
			territory.TerritoryID,
			boolPtrStr(territory.Available),
			territory.ReleaseDate,
			boolPtrStr(territory.PreOrderEnabled),
			territory.PreOrderPublishDate,
			strings.Join(territory.ContentStatuses, ","),
			territory.ID,
		})
	}
	return headers, rows
}

// newAppAvailabilityCommand builds the app-availability command tree for lead-owned registration.
func newAppAvailabilityCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "app-availability",
		Short: "Inspect app availability by territory",
	}
	group.AddCommand(&cobra.Command{
		Use:          "get <bundleId>",
		Short:        "Show availability, pre-order state, and blockers for every territory",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runAppAvailabilityWithClient(cmd, args, client, outputMode())
		},
	})
	group.AddCommand(newAppPreordersCommand())
	return group
}

func runAppAvailabilityWithClient(cmd *cobra.Command, args []string, client *asc.Client, output string) error {
	bundleID := args[0]
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	availability, err := asc.ReadAppAvailability(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	view := AppAvailabilityView{
		BundleID:                  bundleID,
		ID:                        availability.ID,
		AvailableInNewTerritories: availability.AvailableInNewTerritories,
		Territories:               make([]TerritoryAvailabilityView, 0, len(availability.Territories)),
	}
	for _, territory := range availability.Territories {
		view.Territories = append(view.Territories, TerritoryAvailabilityView{
			ID:                  territory.ID,
			TerritoryID:         territory.TerritoryID,
			Available:           territory.Available,
			ReleaseDate:         territory.ReleaseDate,
			PreOrderEnabled:     territory.PreOrderEnabled,
			PreOrderPublishDate: territory.PreOrderPublishDate,
			ContentStatuses:     slices.Clone(territory.ContentStatuses),
		})
	}
	slices.SortFunc(view.Territories, func(a, b TerritoryAvailabilityView) int {
		if a.TerritoryID == b.TerritoryID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.TerritoryID, b.TerritoryID)
	})
	return renderTo(cmd.OutOrStdout(), view, output, true)
}
