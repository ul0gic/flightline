package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type AppEULAView struct {
	BundleID      string   `json:"bundleId"`
	AppID         string   `json:"appId"`
	ID            string   `json:"id,omitempty"`
	AgreementText string   `json:"agreementText,omitempty"`
	Territories   []string `json:"territories"`
}

type AppEULAWriteResult struct {
	AppEULAView
	Changed bool `json:"changed"`
}

func (v AppEULAWriteResult) TableRows() (headers []string, rows [][]string) {
	return v.AppEULAView.TableRows()
}

type AppEULADeleteResult struct {
	BundleID string `json:"bundleId"`
	AppID    string `json:"appId"`
	Deleted  bool   `json:"deleted"`
}

func (v AppEULADeleteResult) TableRows() (headers []string, rows [][]string) {
	return []string{"BUNDLE_ID", "APP_ID", "DELETED"}, [][]string{{v.BundleID, v.AppID, strconv.FormatBool(v.Deleted)}}
}

func (v AppEULAView) TableRows() (headers []string, rows [][]string) {
	return []string{"BUNDLE_ID", "APP_ID", "EULA_ID", "AGREEMENT_TEXT", "TERRITORIES"}, [][]string{{v.BundleID, v.AppID, v.ID, v.AgreementText, strings.Join(v.Territories, ",")}}
}

func newAppEULACommand() *cobra.Command {
	group := &cobra.Command{Use: "app-eula", Short: "Inspect and manage the custom app EULA"}
	group.AddCommand(&cobra.Command{
		Use: "get <bundleId>", Short: "Show the observed custom EULA and complete territory set", Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runAppEULAGetWithClient(cmd, args[0], client, outputMode())
		},
	})
	var file string
	var territories []string
	var confirmSet bool
	set := &cobra.Command{
		Use: "set <bundleId>", Short: "Create or update the custom EULA from an agreement file and exact territories", Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runAppEULASetWithClient(cmd, args[0], file, territories, confirmSet, client, outputMode())
		},
	}
	set.Flags().StringVar(&file, "text-file", "", "file containing the authored agreement text")
	set.Flags().StringArrayVar(&territories, "territory", nil, "territory ID covered by the custom EULA (repeatable; exact set)")
	set.Flags().BoolVar(&confirmSet, "confirm", false, "confirm the custom legal agreement and territory scope")
	group.AddCommand(set)
	var confirmDelete bool
	deleteCmd := &cobra.Command{
		Use: "delete <bundleId>", Short: "Delete the app's custom EULA", Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runAppEULADeleteWithClient(cmd, args[0], confirmDelete, client, outputMode())
		},
	}
	deleteCmd.Flags().BoolVar(&confirmDelete, "confirm", false, "confirm deletion of the custom legal agreement")
	group.AddCommand(deleteCmd)
	return group
}

func runAppEULAGetWithClient(cmd *cobra.Command, bundleID string, client *asc.Client, output string) error {
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	observed, err := asc.ReadAppEULA(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	view := AppEULAView{BundleID: bundleID, AppID: appID, Territories: []string{}}
	if observed != nil {
		view.ID = observed.ID
		view.AgreementText = observed.AgreementText
		view.Territories = observed.Territories
	}
	return renderTo(cmd.OutOrStdout(), view, output, true)
}

func runAppEULASetWithClient(cmd *cobra.Command, bundleID, file string, territories []string, confirmed bool, client *asc.Client, output string) error {
	if !confirmed {
		return errors.New("app EULA: pass --confirm to set a custom legal agreement")
	}
	wantText, selected, err := authoredAppEULA(file, territories)
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	current, err := asc.ReadAppEULA(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	changed := current == nil || current.AgreementText != wantText || !slices.Equal(current.Territories, selected)
	if changed {
		writeErr := writeAppEULA(cmd, client, appID, current, wantText, selected)
		after, readErr := asc.ReadAppEULA(cmd.Context(), client, appID)
		if readErr != nil || after == nil || after.AgreementText != wantText || !slices.Equal(after.Territories, selected) {
			if writeErr != nil {
				return fmt.Errorf("app EULA write outcome is unconfirmed; inspect current state before retrying: %w", writeErr)
			}
			if readErr != nil {
				return fmt.Errorf("app EULA write outcome is unconfirmed; inspect current state before retrying: %w", readErr)
			}
			return errors.New("app EULA write did not converge; inspect current state")
		}
		current = after
	}
	return renderTo(cmd.OutOrStdout(), AppEULAWriteResult{AppEULAView: AppEULAView{
		BundleID: bundleID, AppID: appID, ID: current.ID, AgreementText: current.AgreementText, Territories: current.Territories,
	}, Changed: changed}, output, true)
}

func authoredAppEULA(file string, territories []string) (agreementText string, selectedTerritories []string, err error) {
	if file == "" {
		return "", nil, errors.New("app EULA: --text-file is required")
	}
	text, err := os.ReadFile(file) //nolint:gosec // The user explicitly selects the agreement file.
	if err != nil {
		return "", nil, fmt.Errorf("app EULA: read agreement file: %w", err)
	}
	if strings.TrimSpace(string(text)) == "" {
		return "", nil, errors.New("app EULA: agreement text must be nonempty")
	}
	selected, err := normalizeEULATerritories(territories)
	if err != nil {
		return "", nil, err
	}
	return string(text), selected, nil
}

func writeAppEULA(cmd *cobra.Command, client *asc.Client, appID string, current *asc.AppEULA, wantText string, selected []string) error {
	if current == nil {
		_, err := asc.CreateAppEULA(cmd.Context(), client, appID, wantText, selected)
		return err
	}
	var patchText *string
	var patchTerritories *[]string
	if current.AgreementText != wantText {
		patchText = &wantText
	}
	if !slices.Equal(current.Territories, selected) {
		patchTerritories = &selected
	}
	return asc.PatchAppEULA(cmd.Context(), client, current.ID, patchText, patchTerritories)
}

func runAppEULADeleteWithClient(cmd *cobra.Command, bundleID string, confirmed bool, client *asc.Client, output string) error {
	if !confirmed {
		return errors.New("app EULA: pass --confirm to delete the custom legal agreement")
	}
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	current, err := asc.ReadAppEULA(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	deleted := current != nil
	if deleted {
		writeErr := asc.DeleteAppEULA(cmd.Context(), client, current.ID)
		after, readErr := asc.ReadAppEULA(cmd.Context(), client, appID)
		if readErr != nil || after != nil {
			if writeErr != nil {
				return fmt.Errorf("app EULA deletion outcome is unconfirmed; inspect current state before retrying: %w", writeErr)
			}
			if readErr != nil {
				return fmt.Errorf("app EULA deletion outcome is unconfirmed; inspect current state before retrying: %w", readErr)
			}
			return errors.New("app EULA deletion did not converge; inspect current state")
		}
	}
	return renderTo(cmd.OutOrStdout(), AppEULADeleteResult{BundleID: bundleID, AppID: appID, Deleted: deleted}, output, true)
}

func normalizeEULATerritories(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("app EULA: at least one --territory is required")
	}
	selected := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return nil, errors.New("app EULA: territory IDs must be nonempty and unique")
		}
		seen[value] = true
		selected = append(selected, value)
	}
	slices.Sort(selected)
	return selected, nil
}
