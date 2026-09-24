package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ContentRightsView struct {
	BundleID    string  `json:"bundleId"`
	AppID       string  `json:"appId"`
	Declaration *string `json:"contentRightsDeclaration"`
}

type ContentRightsWriteResult struct {
	ContentRightsView
	Changed bool `json:"changed"`
}

func (v ContentRightsWriteResult) TableRows() (headers []string, rows [][]string) {
	return v.ContentRightsView.TableRows()
}

func (v ContentRightsView) TableRows() (headers []string, rows [][]string) {
	value := ""
	if v.Declaration != nil {
		value = *v.Declaration
	}
	return []string{"BUNDLE_ID", "APP_ID", "CONTENT_RIGHTS_DECLARATION"}, [][]string{{v.BundleID, v.AppID, value}}
}

func newContentRightsCommand() *cobra.Command {
	group := &cobra.Command{Use: "content-rights", Short: "Inspect and set the app content rights declaration"}
	group.AddCommand(&cobra.Command{
		Use: "get <bundleId>", Short: "Show the observed content rights declaration", Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runContentRightsGetWithClient(cmd, args[0], client, outputMode())
		},
	})
	var confirmed bool
	set := &cobra.Command{
		Use: "set <bundleId> <declaration>", Short: "Set an explicit content rights declaration", Args: cobra.ExactArgs(2), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runContentRightsSetWithClient(cmd, args[0], args[1], confirmed, client, outputMode())
		},
	}
	set.Flags().BoolVar(&confirmed, "confirm", false, "confirm the explicit legal declaration")
	group.AddCommand(set)
	return group
}

func runContentRightsGetWithClient(cmd *cobra.Command, bundleID string, client *asc.Client, output string) error {
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	observed, err := asc.ReadContentRights(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), ContentRightsView{BundleID: bundleID, AppID: appID, Declaration: observed.Declaration}, output, true)
}

func runContentRightsSetWithClient(cmd *cobra.Command, bundleID, declaration string, confirmed bool, client *asc.Client, output string) error {
	if !confirmed {
		return errors.New("content rights: pass --confirm to set the declaration")
	}
	if declaration != asc.ContentRightsDoesNotUseThirdParty && declaration != asc.ContentRightsUsesThirdParty {
		return errors.New("content rights: choose an explicit supported declaration")
	}
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return err
	}
	before, err := asc.ReadContentRights(cmd.Context(), client, appID)
	if err != nil {
		return err
	}
	changed := before.Declaration == nil || *before.Declaration != declaration
	if changed {
		writeErr := asc.PatchContentRights(cmd.Context(), client, appID, declaration)
		after, readErr := asc.ReadContentRights(cmd.Context(), client, appID)
		if readErr != nil || after.Declaration == nil || *after.Declaration != declaration {
			if writeErr != nil {
				return fmt.Errorf("content rights update outcome is unconfirmed; inspect current state before retrying: %w", writeErr)
			}
			if readErr != nil {
				return fmt.Errorf("content rights update outcome is unconfirmed; inspect current state before retrying: %w", readErr)
			}
			return errors.New("content rights update did not converge; inspect current state")
		}
	}
	return renderTo(cmd.OutOrStdout(), ContentRightsWriteResult{ContentRightsView: ContentRightsView{
		BundleID: bundleID, AppID: appID, Declaration: &declaration,
	}, Changed: changed}, output, true)
}
