package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// VersionReleaseResult confirms a submitted manual-release request.
type VersionReleaseResult struct {
	BundleID  string `json:"bundleId"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	RequestID string `json:"requestId"`
}

func (r VersionReleaseResult) TableRows() (headers []string, rows [][]string) {
	return []string{"BUNDLE_ID", "VERSION", "PLATFORM", "REQUEST_ID"}, [][]string{{r.BundleID, r.Version, r.Platform, r.RequestID}}
}

func newVersionReleaseCommand() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{Use: "version-release <bundleId>", Short: "Release an approved manual version", Args: cobra.ExactArgs(1), SilenceUsage: true, Long: "Release an approved MANUAL version in Pending Developer Release. This action is never run by state apply and its outcome must be inspected before retrying."}
	cmd.Flags().String("version", "", "App Store version string")
	cmd.Flags().String("platform", "IOS", "App Store platform")
	cmd.Flags().BoolVar(&confirmed, "confirm", false, "confirm release of the approved version")
	_ = cmd.MarkFlagRequired("version")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		client, err := newClient()
		if err != nil {
			return err
		}
		return runVersionReleaseWithClient(cmd, args[0], confirmed, client, outputMode())
	}
	return cmd
}

func runVersionReleaseWithClient(cmd *cobra.Command, bundleID string, confirmed bool, client *asc.Client, output string) error {
	if !confirmed {
		return errors.New("version release: pass --confirm to release the approved version")
	}
	target, err := resolvePhasedReleaseTarget(cmd.Context(), client, cmd, bundleID)
	if err != nil {
		return err
	}
	if versionDisplayState(target.Attributes) != "PENDING_DEVELOPER_RELEASE" || target.Attributes.ReleaseType != "MANUAL" {
		return fmt.Errorf("version release: requires MANUAL version in PENDING_DEVELOPER_RELEASE, got releaseType=%q state=%q", target.Attributes.ReleaseType, versionDisplayState(target.Attributes))
	}
	request, err := asc.CreateAppStoreVersionReleaseRequest(cmd.Context(), client, target.ID)
	if err != nil {
		return fmt.Errorf("version release outcome is unconfirmed; inspect version state before retrying: %w", err)
	}
	return renderTo(cmd.OutOrStdout(), VersionReleaseResult{BundleID: bundleID, Version: target.Attributes.VersionString, Platform: target.Attributes.Platform, RequestID: request.ID}, output, true)
}
