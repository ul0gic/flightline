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

type BetaDistributionResult struct {
	Action   string                               `json:"action"`
	BundleID string                               `json:"bundleId"`
	GroupID  string                               `json:"groupId"`
	BuildID  string                               `json:"buildId,omitempty"`
	Changed  bool                                 `json:"changed"`
	Builds   *[]asc.Resource[asc.BuildAttributes] `json:"builds,omitempty"`
}

func (r BetaDistributionResult) TableRows() (headers []string, rows [][]string) {
	if r.Action == "list" {
		headers = []string{"BUILD_NUMBER", "BUILD_ID", "PROCESSING_STATE"}
		if r.Builds != nil {
			for _, build := range *r.Builds {
				rows = append(rows, []string{build.Attributes.Version, build.ID, build.Attributes.ProcessingState})
			}
		}
		return headers, rows
	}
	return []string{"FIELD", "VALUE"}, [][]string{
		{"ACTION", r.Action}, {"BUNDLE_ID", r.BundleID}, {"GROUP_ID", r.GroupID},
		{"BUILD_ID", r.BuildID}, {"CHANGED", strconv.FormatBool(r.Changed)},
	}
}

func newBetaDistributionCommand() *cobra.Command {
	root := &cobra.Command{Use: "distribution", Short: "List and assign TestFlight builds to beta groups"}
	root.AddCommand(newBetaDistributionListCommand(), newBetaDistributionAddCommand(), newBetaDistributionRemoveCommand())
	return root
}

func newBetaDistributionListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Short: "List a beta group's assigned builds", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("group", "", "beta group ID")
	_ = cmd.MarkFlagRequired("group")
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runBetaDistribution(cmd, args, "list") }
	return cmd
}

func newBetaDistributionAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add <bundleId>", Short: "Assign a build to a beta group", Args: cobra.ExactArgs(1)}
	betaDistributionBuildFlags(cmd)
	cmd.Flags().Bool("allow-notifications", false, "permit assignment when Apple has auto-notify enabled")
	cmd.Flags().Bool("confirm", false, "confirm assignment with auto-notify enabled")
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runBetaDistribution(cmd, args, "add") }
	return cmd
}

func newBetaDistributionRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove <bundleId>", Short: "Remove a build from a beta group", Args: cobra.ExactArgs(1)}
	betaDistributionBuildFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runBetaDistribution(cmd, args, "remove") }
	return cmd
}

func betaDistributionBuildFlags(cmd *cobra.Command) {
	cmd.Flags().String("group", "", "beta group ID")
	cmd.Flags().String("build", "", "build number (CFBundleVersion)")
	cmd.Flags().String("version", "", "release version to disambiguate a build number")
	cmd.Flags().String("platform", "IOS", "platform to disambiguate a build number")
	_ = cmd.MarkFlagRequired("group")
	_ = cmd.MarkFlagRequired("build")
}

func runBetaDistribution(cmd *cobra.Command, args []string, action string) error {
	groupID, err := cmd.Flags().GetString("group")
	if err != nil {
		return err
	}
	if strings.TrimSpace(groupID) == "" {
		return errors.New("distribution: --group is required")
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
	builds, err := asc.ListBetaGroupBuilds(cmd.Context(), c, groupID)
	if err != nil {
		return err
	}
	result := BetaDistributionResult{Action: action, BundleID: args[0], GroupID: groupID}
	if action == "list" {
		if builds == nil {
			builds = []asc.Resource[asc.BuildAttributes]{}
		}
		result.Builds = &builds
		return Render(result, outputMode())
	}
	buildID, err := resolveBetaDistributionBuild(cmd, c, appID, args[0])
	if err != nil {
		return err
	}
	result.BuildID = buildID
	member := betaBuildMember(builds, buildID)
	changed, err := mutateBetaDistribution(cmd, c, action, groupID, buildID, member)
	if err != nil {
		return err
	}
	result.Changed = changed
	return Render(result, outputMode())
}

func verifyBetaGroupOwner(ctx context.Context, c *asc.Client, appID, groupID, bundleID string) error {
	groups, err := collectBetaGroups(ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/betaGroups", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		return err
	}
	for i := range groups {
		if groups[i].ID == groupID {
			return nil
		}
	}
	return fmt.Errorf("beta group %s does not belong to %s", groupID, bundleID)
}

func resolveBetaDistributionBuild(cmd *cobra.Command, c *asc.Client, appID, bundleID string) (string, error) {
	build, err := cmd.Flags().GetString("build")
	if err != nil {
		return "", err
	}
	version, err := cmd.Flags().GetString("version")
	if err != nil {
		return "", err
	}
	platform, err := cmd.Flags().GetString("platform")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(build) == "" {
		return "", errors.New("distribution: --build is required")
	}
	return resolveBuildIDWithOptions(cmd.Context(), c, appID, bundleID, build, buildLookupOptions{ReleaseVersion: version, Platform: platform})
}

func betaBuildMember(builds []asc.Resource[asc.BuildAttributes], buildID string) bool {
	for _, row := range builds {
		if row.ID == buildID {
			return true
		}
	}
	return false
}

func mutateBetaDistribution(cmd *cobra.Command, c *asc.Client, action, groupID, buildID string, member bool) (bool, error) {
	switch action {
	case "add":
		if member {
			return false, nil
		}
		allow, err := cmd.Flags().GetBool("allow-notifications")
		if err != nil {
			return false, err
		}
		confirm, err := cmd.Flags().GetBool("confirm")
		if err != nil {
			return false, err
		}
		if err := betaDistributionNotificationGuard(cmd.Context(), c, buildID, allow, confirm); err != nil {
			return false, err
		}
		if err := asc.AddBetaGroupBuild(cmd.Context(), c, groupID, buildID); err != nil {
			return false, err
		}
	case "remove":
		if !member {
			return false, nil
		}
		if err := asc.RemoveBetaGroupBuild(cmd.Context(), c, groupID, buildID); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported distribution action %s", action)
	}
	return true, nil
}

func betaDistributionNotificationGuard(ctx context.Context, c *asc.Client, buildID string, allow, confirm bool) error {
	detail, err := asc.GetBuildBetaDetail(ctx, c, buildID)
	if err != nil {
		return fmt.Errorf("cannot verify auto-notify for build %s: %w", buildID, err)
	}
	if detail.Attributes.AutoNotifyEnabled == nil {
		return fmt.Errorf("build %s auto-notify state is unknown; set it explicitly in App Store Connect before assignment", buildID)
	}
	if *detail.Attributes.AutoNotifyEnabled && (!allow || !confirm) {
		return fmt.Errorf("build %s auto-notify is enabled; disable it in App Store Connect or pass --allow-notifications --confirm", buildID)
	}
	return nil
}
