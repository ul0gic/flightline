package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type BetaPolicyResult struct {
	Action            string `json:"action"`
	BundleID          string `json:"bundleId"`
	BuildID           string `json:"buildId"`
	DetailID          string `json:"detailId"`
	AutoNotifyEnabled *bool  `json:"autoNotifyEnabled"`
	Changed           bool   `json:"changed"`
}

func (r BetaPolicyResult) TableRows() (headers []string, rows [][]string) {
	return []string{"FIELD", "VALUE"}, [][]string{
		{"ACTION", r.Action}, {"BUNDLE_ID", r.BundleID}, {"BUILD_ID", r.BuildID},
		{"DETAIL_ID", r.DetailID}, {"AUTO_NOTIFY_ENABLED", boolPtrStr(r.AutoNotifyEnabled)},
		{"CHANGED", strconv.FormatBool(r.Changed)},
	}
}

type BetaNotifyResult struct {
	BundleID       string `json:"bundleId"`
	BuildID        string `json:"buildId"`
	NotificationID string `json:"notificationId"`
	Triggered      bool   `json:"triggered"`
}

func (r BetaNotifyResult) TableRows() (headers []string, rows [][]string) {
	return []string{"FIELD", "VALUE"}, [][]string{
		{"BUNDLE_ID", r.BundleID}, {"BUILD_ID", r.BuildID},
		{"NOTIFICATION_ID", r.NotificationID}, {"TRIGGERED", strconv.FormatBool(r.Triggered)},
	}
}

type BetaRecruitmentResult struct {
	GroupID   string                                                `json:"groupId,omitempty"`
	Criterion *asc.Resource[asc.BetaRecruitmentCriterionAttributes] `json:"criterion"`
	Options   *[]asc.Resource[asc.BetaRecruitmentOptionAttributes]  `json:"options,omitempty"`
}

type betaActionPreReleaseAttributes struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

func (r BetaRecruitmentResult) TableRows() (headers []string, rows [][]string) {
	if r.Options != nil {
		headers = []string{"ID", "DEVICE_FAMILIES"}
		for _, option := range *r.Options {
			rows = append(rows, []string{option.ID, strconv.Itoa(len(option.Attributes.DeviceFamilyOSVersions))})
		}
		return headers, rows
	}
	headers = []string{"FIELD", "VALUE"}
	if r.Criterion == nil {
		return headers, [][]string{{"GROUP_ID", r.GroupID}, {"STATUS", "(none)"}}
	}
	return headers, [][]string{
		{"GROUP_ID", r.GroupID}, {"CRITERION_ID", r.Criterion.ID},
		{"LAST_MODIFIED", r.Criterion.Attributes.LastModifiedDate},
		{"FILTERS", strconv.Itoa(len(r.Criterion.Attributes.DeviceFamilyOSVersionFilters))},
	}
}

func newBetaRecruitmentCommand() *cobra.Command {
	root := &cobra.Command{Use: "recruitment", Short: "Inspect beta recruitment criteria and control tester notifications",
		Long: "Read recruitment criteria and options, set a build's auto-notify policy, or explicitly notify testers. Invitation resend is unavailable because Apple's current request does not establish a supported individual-recipient relationship; recruitment criteria writes are not implemented."}
	policy := &cobra.Command{Use: "policy", Short: "Read or set a build's auto-notify policy"}
	policy.AddCommand(newBetaPolicyGetCommand(), newBetaPolicySetCommand())
	root.AddCommand(policy, newBetaNotifyCommand(), newBetaRecruitmentGetCommand(), newBetaRecruitmentOptionsCommand())
	return root
}

func newBetaPolicyGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Short: "Read a build's TestFlight auto-notify policy", Args: cobra.ExactArgs(1)}
	betaActionBuildFlags(cmd)
	cmd.RunE = runBetaPolicyGet
	return cmd
}

func newBetaPolicySetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set <bundleId>", Short: "Set a build's TestFlight auto-notify policy", Args: cobra.ExactArgs(1)}
	betaActionBuildFlags(cmd)
	cmd.Flags().Bool("enabled", false, "explicit desired auto-notify setting")
	cmd.Flags().Bool("confirm", false, "confirm enabling future automatic tester notifications")
	_ = cmd.MarkFlagRequired("enabled")
	cmd.RunE = runBetaPolicySet
	return cmd
}

func newBetaNotifyCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "notify <bundleId>", Short: "Send a TestFlight build notification to testers", Args: cobra.ExactArgs(1)}
	betaActionBuildFlags(cmd)
	cmd.Flags().Bool("confirm", false, "confirm the one-shot tester notification")
	cmd.RunE = runBetaNotify
	return cmd
}

func newBetaRecruitmentGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "criteria <bundleId>", Short: "Read a beta group's recruitment criteria", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("group", "", "beta group ID")
	_ = cmd.MarkFlagRequired("group")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		groupID, err := cmd.Flags().GetString("group")
		if err != nil {
			return err
		}
		if strings.TrimSpace(groupID) == "" {
			return errors.New("recruitment criteria: --group is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		if err := verifyBetaGroupOwner(cmd.Context(), c, appID, groupID, args[0]); err != nil {
			return err
		}
		criterion, err := asc.GetBetaRecruitmentCriterion(cmd.Context(), c, groupID)
		if err != nil {
			return err
		}
		return Render(BetaRecruitmentResult{GroupID: groupID, Criterion: criterion}, outputMode())
	}
	return cmd
}

func newBetaRecruitmentOptionsCommand() *cobra.Command {
	return &cobra.Command{Use: "criteria-options", Short: "List available recruitment device and OS options", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		options, err := asc.ListBetaRecruitmentOptions(cmd.Context(), c)
		if err != nil {
			return err
		}
		if options == nil {
			options = []asc.Resource[asc.BetaRecruitmentOptionAttributes]{}
		}
		return Render(BetaRecruitmentResult{Options: &options}, outputMode())
	}}
}

func betaActionBuildFlags(cmd *cobra.Command) {
	cmd.Flags().String("build", "", "build number (CFBundleVersion)")
	cmd.Flags().String("version", "", "prerelease version")
	cmd.Flags().String("platform", "", "platform (IOS, MAC_OS, TV_OS, or VISION_OS)")
	_ = cmd.MarkFlagRequired("build")
	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("platform")
}

func betaActionBuildID(cmd *cobra.Command, c *asc.Client, appID, bundleID string) (string, error) {
	build, _ := cmd.Flags().GetString("build")
	version, _ := cmd.Flags().GetString("version")
	platform, _ := cmd.Flags().GetString("platform")
	if strings.TrimSpace(build) == "" || strings.TrimSpace(version) == "" || strings.TrimSpace(platform) == "" {
		return "", errors.New("beta action requires --build, --version, and --platform")
	}
	view, err := lookupBuild(cmd.Context(), c, appID, build, buildLookupOptions{ReleaseVersion: version, Platform: platform})
	if err != nil {
		return "", err
	}
	if view == nil || view.ID == "" || view.Type != "builds" || view.Attributes.Version != build {
		return "", fmt.Errorf("build %s for %s did not resolve to a complete exact build", build, bundleID)
	}
	if err := verifyBetaActionBuildIdentity(cmd.Context(), c, view.ID, appID, version, platform); err != nil {
		return "", err
	}
	return view.ID, nil
}

func verifyBetaActionBuildIdentity(ctx context.Context, c *asc.Client, buildID, appID, version, platform string) error {
	path := "/v1/builds/" + url.PathEscape(buildID)
	app, err := asc.Get[asc.Single[asc.EmptyAttributes]](ctx, c, path+"/app", nil)
	if err != nil {
		return fmt.Errorf("verify beta build app: %w", err)
	}
	if app.Data.Type != "apps" || app.Data.ID != appID {
		return fmt.Errorf("build %s belongs to a different app; no action taken", buildID)
	}
	pre, err := asc.Get[asc.Single[betaActionPreReleaseAttributes]](ctx, c, path+"/preReleaseVersion", nil)
	if err != nil {
		return fmt.Errorf("verify beta build prerelease version: %w", err)
	}
	if pre.Data.Type != "preReleaseVersions" || pre.Data.Attributes.Version != version || !strings.EqualFold(pre.Data.Attributes.Platform, platform) {
		return fmt.Errorf("build %s prerelease identity does not match requested %s/%s; no action taken", buildID, version, platform)
	}
	return nil
}

func betaActionClientAndBuild(cmd *cobra.Command, bundleID string) (*asc.Client, string, error) {
	c, err := newClient()
	if err != nil {
		return nil, "", err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return nil, "", err
	}
	buildID, err := betaActionBuildID(cmd, c, appID, bundleID)
	if err != nil {
		return nil, "", err
	}
	return c, buildID, nil
}

func runBetaPolicyGet(cmd *cobra.Command, args []string) error {
	c, buildID, err := betaActionClientAndBuild(cmd, args[0])
	if err != nil {
		return err
	}
	detail, err := asc.GetBuildBetaDetail(cmd.Context(), c, buildID)
	if err != nil {
		return err
	}
	return Render(BetaPolicyResult{Action: "get", BundleID: args[0], BuildID: buildID, DetailID: detail.ID,
		AutoNotifyEnabled: detail.Attributes.AutoNotifyEnabled}, outputMode())
}

func runBetaPolicySet(cmd *cobra.Command, args []string) error {
	if !cmd.Flags().Changed("enabled") {
		return errors.New("policy set requires --enabled=true or --enabled=false")
	}
	enabled, err := cmd.Flags().GetBool("enabled")
	if err != nil {
		return err
	}
	confirm, err := cmd.Flags().GetBool("confirm")
	if err != nil {
		return err
	}
	if enabled && !confirm {
		return errors.New("enabling automatic tester notifications requires --confirm")
	}
	c, buildID, err := betaActionClientAndBuild(cmd, args[0])
	if err != nil {
		return err
	}
	detail, err := asc.GetBuildBetaDetail(cmd.Context(), c, buildID)
	if err != nil {
		return err
	}
	result := BetaPolicyResult{Action: "set", BundleID: args[0], BuildID: buildID, DetailID: detail.ID,
		AutoNotifyEnabled: detail.Attributes.AutoNotifyEnabled}
	if detail.Attributes.AutoNotifyEnabled != nil && *detail.Attributes.AutoNotifyEnabled == enabled {
		return Render(result, outputMode())
	}
	updated, err := asc.UpdateBuildAutoNotify(cmd.Context(), c, detail.ID, enabled)
	if err != nil {
		return err
	}
	result.AutoNotifyEnabled = updated.Attributes.AutoNotifyEnabled
	result.Changed = true
	return Render(result, outputMode())
}

func runBetaNotify(cmd *cobra.Command, args []string) error {
	confirmed, err := cmd.Flags().GetBool("confirm")
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("notify requires --confirm; this action sends a tester notification")
	}
	c, buildID, err := betaActionClientAndBuild(cmd, args[0])
	if err != nil {
		return err
	}
	notification, err := asc.NotifyBetaBuild(cmd.Context(), c, buildID)
	if err != nil {
		return err
	}
	return Render(BetaNotifyResult{BundleID: args[0], BuildID: buildID, NotificationID: notification.ID, Triggered: true}, outputMode())
}
