package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// PhasedReleaseView is the stable L1 view of a version rollout.
type PhasedReleaseView struct {
	BundleID           string `json:"bundleId"`
	Version            string `json:"version"`
	Platform           string `json:"platform"`
	ID                 string `json:"id"`
	State              string `json:"state"`
	StartDate          string `json:"startDate,omitempty"`
	TotalPauseDuration *int   `json:"totalPauseDuration,omitempty"`
	CurrentDayNumber   *int   `json:"currentDayNumber,omitempty"`
}

func (v PhasedReleaseView) TableRows() (headers []string, rows [][]string) {
	return []string{"VERSION", "PLATFORM", "STATE", "START_DATE", "ID"}, [][]string{{v.Version, v.Platform, v.State, v.StartDate, v.ID}}
}

func newPhasedReleaseCommand() *cobra.Command {
	group := &cobra.Command{Use: "phased-release", Short: "Inspect and control phased release for an app update"}
	group.AddCommand(newPhasedReleaseGetCommand(), newPhasedReleaseEnableCommand(), newPhasedReleasePauseCommand(), newPhasedReleaseResumeCommand())
	return group
}

func phasedReleaseTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("version", "", "App Store version string")
	cmd.Flags().String("platform", "IOS", "App Store platform")
	_ = cmd.MarkFlagRequired("version")
}

func newPhasedReleaseGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Short: "Show a version's phased-release state", Args: cobra.ExactArgs(1), SilenceUsage: true}
	phasedReleaseTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		client, err := newClient()
		if err != nil {
			return err
		}
		return runPhasedReleaseGetWithClient(cmd, args[0], client, outputMode())
	}
	return cmd
}

func newPhasedReleaseEnableCommand() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{Use: "enable <bundleId>", Short: "Enable inactive phased release for an eligible update", Args: cobra.ExactArgs(1), SilenceUsage: true}
	phasedReleaseTargetFlags(cmd)
	cmd.Flags().BoolVar(&confirmed, "confirm", false, "confirm enabling phased release")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		client, err := newClient()
		if err != nil {
			return err
		}
		return runPhasedReleaseEnableWithClient(cmd, args[0], confirmed, client, outputMode())
	}
	return cmd
}

func newPhasedReleasePauseCommand() *cobra.Command  { return newPhasedReleaseStateCommand("pause") }
func newPhasedReleaseResumeCommand() *cobra.Command { return newPhasedReleaseStateCommand("resume") }

func newPhasedReleaseStateCommand(action string) *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{Use: action + " <bundleId>", Short: action + " an active or paused phased release", Args: cobra.ExactArgs(1), SilenceUsage: true}
	phasedReleaseTargetFlags(cmd)
	cmd.Flags().BoolVar(&confirmed, "confirm", false, "confirm phased release "+action)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		client, err := newClient()
		if err != nil {
			return err
		}
		return runPhasedReleaseStateWithClient(cmd, args[0], action, confirmed, client, outputMode())
	}
	return cmd
}

func runPhasedReleaseGetWithClient(cmd *cobra.Command, bundleID string, client *asc.Client, output string) error {
	target, err := resolvePhasedReleaseTarget(cmd.Context(), client, cmd, bundleID)
	if err != nil {
		return err
	}
	release, err := asc.ReadAppStoreVersionPhasedRelease(cmd.Context(), client, target.ID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), newPhasedReleaseView(bundleID, target, release), output, true)
}

func runPhasedReleaseEnableWithClient(cmd *cobra.Command, bundleID string, confirmed bool, client *asc.Client, output string) error {
	if !confirmed {
		return errors.New("phased release: pass --confirm to enable a phased release")
	}
	target, err := resolvePhasedReleaseTarget(cmd.Context(), client, cmd, bundleID)
	if err != nil {
		return err
	}
	if !phasedReleaseEnableState(versionDisplayState(target.Attributes)) {
		return fmt.Errorf("phased release: version state %q is not eligible to enable", versionDisplayState(target.Attributes))
	}
	if existing, err := asc.ReadAppStoreVersionPhasedRelease(cmd.Context(), client, target.ID); err == nil {
		if existing.PhasedReleaseState == "INACTIVE" {
			return renderTo(cmd.OutOrStdout(), newPhasedReleaseView(bundleID, target, existing), output, true)
		}
		return fmt.Errorf("phased release: rollout already exists in %s; choose an explicit supported action", existing.PhasedReleaseState)
	} else if !phasedReleaseNotFound(err) {
		return err
	}
	release, err := asc.EnableAppStoreVersionPhasedRelease(cmd.Context(), client, target.ID)
	if err != nil {
		return fmt.Errorf("phased release enable outcome is unconfirmed; inspect current state before retrying: %w", err)
	}
	if release.PhasedReleaseState != "INACTIVE" {
		return errors.New("phased release: enable was not confirmed INACTIVE; inspect current state before retrying")
	}
	return renderTo(cmd.OutOrStdout(), newPhasedReleaseView(bundleID, target, release), output, true)
}

func runPhasedReleaseStateWithClient(cmd *cobra.Command, bundleID, action string, confirmed bool, client *asc.Client, output string) error {
	if !confirmed {
		return fmt.Errorf("phased release: pass --confirm to %s", action)
	}
	target, err := resolvePhasedReleaseTarget(cmd.Context(), client, cmd, bundleID)
	if err != nil {
		return err
	}
	if versionDisplayState(target.Attributes) != "READY_FOR_DISTRIBUTION" {
		return fmt.Errorf("phased release: %s requires READY_FOR_DISTRIBUTION, got %q", action, versionDisplayState(target.Attributes))
	}
	release, err := asc.ReadAppStoreVersionPhasedRelease(cmd.Context(), client, target.ID)
	if err != nil {
		return err
	}
	want := "PAUSED"
	if action == "resume" {
		want = "ACTIVE"
	}
	if release.PhasedReleaseState == want {
		return renderTo(cmd.OutOrStdout(), newPhasedReleaseView(bundleID, target, release), output, true)
	}
	if action == "pause" && release.PhasedReleaseState != "ACTIVE" || action == "resume" && release.PhasedReleaseState != "PAUSED" {
		return fmt.Errorf("phased release: cannot %s observed %s rollout", action, release.PhasedReleaseState)
	}
	if action == "pause" {
		release, err = asc.PauseAppStoreVersionPhasedRelease(cmd.Context(), client, release.ID)
	} else {
		release, err = asc.ResumeAppStoreVersionPhasedRelease(cmd.Context(), client, release.ID)
	}
	if err != nil {
		return fmt.Errorf("phased release %s outcome is unconfirmed; inspect current state before retrying: %w", action, err)
	}
	if release.PhasedReleaseState != want {
		return errors.New("phased release: update was not confirmed; inspect current state before retrying")
	}
	return renderTo(cmd.OutOrStdout(), newPhasedReleaseView(bundleID, target, release), output, true)
}

type phasedReleaseTarget struct {
	ID         string
	Attributes asc.VersionAttributes
}

func resolvePhasedReleaseTarget(ctx context.Context, client *asc.Client, cmd *cobra.Command, bundleID string) (phasedReleaseTarget, error) {
	version, _ := cmd.Flags().GetString("version")
	platform, _ := cmd.Flags().GetString("platform")
	if strings.TrimSpace(version) == "" {
		return phasedReleaseTarget{}, errors.New("phased release: --version is required")
	}
	appID, err := resolveAppID(ctx, client, bundleID)
	if err != nil {
		return phasedReleaseTarget{}, err
	}
	query := url.Values{"filter[versionString]": {version}, "filter[platform]": {platform}, "limit": {"200"}}
	var found *phasedReleaseTarget
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, client, "/v1/apps/"+url.PathEscape(appID)+"/appStoreVersions", query) {
		if err != nil {
			return phasedReleaseTarget{}, err
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "appStoreVersions" || row.Attributes.VersionString != version || row.Attributes.Platform != platform {
				continue
			}
			if found != nil {
				return phasedReleaseTarget{}, fmt.Errorf("phased release: version %q on %s is ambiguous", version, platform)
			}
			candidate := phasedReleaseTarget{ID: row.ID, Attributes: row.Attributes}
			found = &candidate
		}
	}
	if found == nil {
		return phasedReleaseTarget{}, fmt.Errorf("phased release: version %q on %s not found", version, platform)
	}
	return *found, nil
}

func newPhasedReleaseView(bundleID string, target phasedReleaseTarget, release *asc.AppStoreVersionPhasedRelease) PhasedReleaseView {
	return PhasedReleaseView{BundleID: bundleID, Version: target.Attributes.VersionString, Platform: target.Attributes.Platform, ID: release.ID, State: release.PhasedReleaseState, StartDate: release.StartDate, TotalPauseDuration: release.TotalPauseDuration, CurrentDayNumber: release.CurrentDayNumber}
}

func phasedReleaseEnableState(state string) bool {
	switch state {
	case "PREPARE_FOR_SUBMISSION", "WAITING_FOR_REVIEW", "IN_REVIEW", "WAITING_FOR_EXPORT_COMPLIANCE", "PENDING_DEVELOPER_RELEASE", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED":
		return true
	}
	return false
}

func phasedReleaseNotFound(err error) bool {
	var apiErr *asc.APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound
}
