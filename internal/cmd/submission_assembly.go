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
	"github.com/ul0gic/flightline/internal/state"
)

// SubmissionAssemblyView is the stable command result for a completed
// assembly. It intentionally does not imply final submission.
type SubmissionAssemblyView struct {
	Action       string                       `json:"action"`
	Submitted    bool                         `json:"submitted"`
	SubmissionID string                       `json:"submissionId"`
	Created      bool                         `json:"created"`
	Attached     []asc.SubmissionItemProposal `json:"attached,omitempty"`
	Planned      []asc.SubmissionItemProposal `json:"planned"`
}

func (v SubmissionAssemblyView) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "SUBMISSION_ID", "CREATED", "SUBMITTED", "ITEMS"}, [][]string{{v.Action, v.SubmissionID, strconv.FormatBool(v.Created), strconv.FormatBool(v.Submitted), strconv.Itoa(len(v.Planned))}}
}

// SubmissionAssemblyDependencies keeps the irreversible path injectable. The
// production wiring must provide a fresh verifier and preflight implementation.
type SubmissionAssemblyDependencies struct {
	Verify    state.SubmissionItemVerifier
	Preflight state.SubmissionPreflight
}

var submissionAssemblyDependencies SubmissionAssemblyDependencies

// newSubmissionAssemblyCommand returns an unregistered command group. Final
// submit is a separate confirmed action and never happens during assemble.
func newSubmissionAssemblyCommand() *cobra.Command {
	group := &cobra.Command{Use: "submission-assembly", Short: "Plan, assemble, and explicitly submit an App Store review submission"}
	group.AddCommand(newSubmissionAssemblyPlanCommand(), newSubmissionAssemblyAssembleCommand(), newSubmissionAssemblySubmitCommand())
	return group
}

func submissionAssemblyFlags(cmd *cobra.Command) {
	cmd.Flags().String("version", "", "App Store version string")
	cmd.Flags().String("platform", "IOS", "App Store platform")
	cmd.Flags().StringSlice("iap-version", nil, "IAP version ID to attach (repeatable)")
	cmd.Flags().StringSlice("event", nil, "app event ID to attach (repeatable)")
	cmd.Flags().StringSlice("experiment", nil, "product page experiment ID to attach (repeatable)")
	_ = cmd.MarkFlagRequired("version")
}

func newSubmissionAssemblyPlanCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "plan <bundleId>", Short: "Show the ordered review-submission membership plan", Args: cobra.ExactArgs(1), SilenceUsage: true}
	submissionAssemblyFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		client, err := newClient()
		if err != nil {
			return err
		}
		plan, err := submissionPlanFromCommand(cmd, client, args[0])
		if err != nil {
			return err
		}
		if submissionAssemblyDependencies.Verify == nil {
			return errors.New("submission assembly: fresh proposal verifier is not configured")
		}
		for _, proposal := range plan.DesiredItems() {
			if err := submissionAssemblyDependencies.Verify(cmd.Context(), client, plan, proposal); err != nil {
				return fmt.Errorf("submission assembly: verify %s: %w", proposal.ID, err)
			}
		}
		return renderTo(cmd.OutOrStdout(), SubmissionAssemblyView{Action: "plan", Planned: plan.DesiredItems()}, outputMode(), true)
	}
	return cmd
}

func newSubmissionAssemblyAssembleCommand() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{Use: "assemble <bundleId>", Short: "Create or resume a review submission and attach exact membership", Args: cobra.ExactArgs(1), SilenceUsage: true}
	submissionAssemblyFlags(cmd)
	cmd.Flags().BoolVar(&confirmed, "confirm", false, "confirm creation and membership attachment; this does not submit to App Review")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !confirmed {
			return errors.New("submission assembly: pass --confirm to create or attach review submission items")
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		plan, err := submissionPlanFromCommand(cmd, client, args[0])
		if err != nil {
			return err
		}
		return runSubmissionAssemblyWithClient(cmd, client, plan, outputMode(), submissionAssemblyDependencies)
	}
	return cmd
}

func newSubmissionAssemblySubmitCommand() *cobra.Command {
	var submissionID string
	var confirmed bool
	cmd := &cobra.Command{Use: "submit <bundleId>", Short: "Submit an already assembled review submission to App Review", Args: cobra.ExactArgs(1), SilenceUsage: true, Long: "Rechecks every proposed item, exact membership, and fresh preflight before PATCHing submitted: true. An interrupted submit is unconfirmed and must be inspected before retrying."}
	submissionAssemblyFlags(cmd)
	cmd.Flags().StringVar(&submissionID, "submission", "", "existing review submission ID")
	cmd.Flags().BoolVar(&confirmed, "confirm", false, "confirm final submission to App Review")
	_ = cmd.MarkFlagRequired("submission")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !confirmed {
			return errors.New("submission assembly: pass --confirm to submit to App Review")
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		plan, err := submissionPlanFromCommand(cmd, client, args[0])
		if err != nil {
			return err
		}
		if err := state.SubmitSubmissionPlan(cmd.Context(), client, plan, strings.TrimSpace(submissionID), submissionAssemblyDependencies.Verify, submissionAssemblyDependencies.Preflight); err != nil {
			return err
		}
		return renderTo(cmd.OutOrStdout(), SubmissionAssemblyView{Action: "submit", Submitted: true, SubmissionID: strings.TrimSpace(submissionID), Planned: plan.DesiredItems()}, outputMode(), true)
	}
	return cmd
}

func runSubmissionAssemblyWithClient(cmd *cobra.Command, client *asc.Client, plan state.SubmissionPlan, output string, deps SubmissionAssemblyDependencies) error {
	result, err := state.ApplySubmissionPlan(cmd.Context(), client, plan, deps.Verify, deps.Preflight)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), SubmissionAssemblyView{Action: "assemble", SubmissionID: result.SubmissionID, Created: result.Created, Attached: result.Attached, Planned: plan.DesiredItems()}, output, true)
}

func submissionPlanFromCommand(cmd *cobra.Command, client *asc.Client, bundleID string) (state.SubmissionPlan, error) {
	version, _ := cmd.Flags().GetString("version")
	platform, _ := cmd.Flags().GetString("platform")
	version = strings.TrimSpace(version)
	platform = strings.ToUpper(strings.TrimSpace(platform))
	if version == "" || platform == "" {
		return state.SubmissionPlan{}, errors.New("submission assembly: --version and --platform are required")
	}
	appID, err := resolveAppID(cmd.Context(), client, bundleID)
	if err != nil {
		return state.SubmissionPlan{}, err
	}
	target, err := resolveSubmissionVersion(cmd.Context(), client, appID, version, platform)
	if err != nil {
		return state.SubmissionPlan{}, err
	}
	proposals := make([]asc.SubmissionItemProposal, 0)
	for _, id := range stringFlagValues(cmd, "iap-version") {
		proposals = append(proposals, asc.SubmissionItemProposal{Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: id})
	}
	for _, id := range stringFlagValues(cmd, "event") {
		proposals = append(proposals, asc.SubmissionItemProposal{Relationship: "appEvent", Type: "appEvents", ID: id})
	}
	for _, id := range stringFlagValues(cmd, "experiment") {
		proposals = append(proposals, asc.SubmissionItemProposal{Relationship: "appStoreVersionExperimentV2", Type: "appStoreVersionExperiments", ID: id})
	}
	return state.BuildSubmissionPlan(appID, platform, asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: target.ID}, proposals)
}

func resolveSubmissionVersion(ctx context.Context, client *asc.Client, appID, version, platform string) (*VersionView, error) {
	q := url.Values{"filter[versionString]": {version}, "filter[platform]": {platform}, "limit": {"200"}}
	var found *VersionView
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, client, "/v1/apps/"+appID+"/appStoreVersions", q) {
		if err != nil {
			return nil, err
		}
		for _, row := range page.Data {
			if row.Type != "appStoreVersions" || row.ID == "" || row.Attributes.VersionString != version || row.Attributes.Platform != platform {
				continue
			}
			if found != nil {
				return nil, fmt.Errorf("submission assembly: version %q on %s is ambiguous", version, platform)
			}
			candidate := &VersionView{ID: row.ID, Type: row.Type, Attributes: row.Attributes}
			found = candidate
		}
	}
	if found == nil {
		return nil, fmt.Errorf("submission assembly: version %q on %s not found", version, platform)
	}
	return found, nil
}

func stringFlagValues(cmd *cobra.Command, name string) []string {
	values, _ := cmd.Flags().GetStringSlice(name)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
