package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ExperimentListResult struct {
	Experiments []asc.ProductExperiment `json:"experiments"`
}

func (r ExperimentListResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Experiments))
	for i := range r.Experiments {
		x := &r.Experiments[i]
		rows = append(rows, []string{x.Name, x.Platform, x.State, strconv.Itoa(x.TrafficProportion), x.ID})
	}
	return []string{"NAME", "PLATFORM", "STATE", "TRAFFIC", "ID"}, rows
}

type ExperimentActionResult struct {
	Action             string                `json:"action"`
	Experiment         asc.ProductExperiment `json:"experiment"`
	Changed            bool                  `json:"changed"`
	VersionAssociation string                `json:"versionAssociation,omitempty"`
}

func (r ExperimentActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "STATE", "CHANGED"}, [][]string{{r.Action, r.Experiment.ID, r.Experiment.State, strconv.FormatBool(r.Changed)}}
}

type ExperimentProposalResult struct {
	Proposal  asc.SubmissionItemProposal `json:"proposal"`
	VersionID string                     `json:"versionId"`
}

func (r ExperimentProposalResult) TableRows() (headers []string, rows [][]string) {
	return []string{"RELATIONSHIP", "TYPE", "ID", "VERSION"}, [][]string{{r.Proposal.Relationship, r.Proposal.Type, r.Proposal.ID, r.VersionID}}
}

func newExperimentsCommand() *cobra.Command {
	root := &cobra.Command{Use: "experiments", Short: "Manage App Store product page experiments"}
	root.AddCommand(newExperimentListCommand(), newExperimentGetCommand(), newExperimentCreateCommand(), newExperimentUpdateCommand(), newExperimentDeleteCommand(), newExperimentLifecycleCommand(true), newExperimentLifecycleCommand(false), newExperimentProposalCommand(), newExperimentTreatmentsCommand())
	return root
}

func experimentTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("version", "", "selected App Store version string")
	cmd.Flags().String("platform", "IOS", "App Store platform")
}
func experimentIDFlag(cmd *cobra.Command) { cmd.Flags().String("experiment", "", "v2 experiment ID") }
func experimentConfirmFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("confirm", false, "confirm this write")
}
func requireExperimentConfirm(cmd *cobra.Command) error {
	ok, _ := cmd.Flags().GetBool("confirm")
	if !ok {
		return errors.New("experiment write requires --confirm")
	}
	return nil
}

func resolveExperimentTarget(ctx context.Context, c *asc.Client, cmd *cobra.Command, bundle string) (appID, versionID, platform string, err error) {
	version, _ := cmd.Flags().GetString("version")
	platform, _ = cmd.Flags().GetString("platform")
	if version == "" || platform == "" {
		return "", "", "", errors.New("--version and --platform are required")
	}
	appID, err = resolveAppID(ctx, c, bundle)
	if err != nil {
		return "", "", "", err
	}
	versionID, err = resolveUniqueAppStoreVersionID(ctx, c, appID, version, platform)
	if err != nil {
		return "", "", "", err
	}
	return appID, versionID, platform, nil
}

func selectedProductExperiment(ctx context.Context, c *asc.Client, appID, versionID, platform, experimentID string) (asc.ProductExperiment, error) {
	if experimentID == "" {
		return asc.ProductExperiment{}, errors.New("--experiment is required")
	}
	list, err := asc.ListVersionExperiments(ctx, c, appID, versionID)
	if err != nil {
		return asc.ProductExperiment{}, err
	}
	count := 0
	for i := range list {
		if list[i].ID == experimentID {
			count++
		}
	}
	if count != 1 {
		return asc.ProductExperiment{}, errors.New("experiment not uniquely associated with selected App Store version")
	}
	x, err := asc.GetProductExperiment(ctx, c, appID, experimentID)
	if err != nil {
		return asc.ProductExperiment{}, err
	}
	if x.Platform != platform {
		return asc.ProductExperiment{}, errors.New("experiment platform differs from selected version")
	}
	return x, nil
}

func newExperimentListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Args: cobra.ExactArgs(1), Short: "List v2 experiments for one version"}
	experimentTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, _, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		items, err := asc.ListVersionExperiments(cmd.Context(), c, appID, versionID)
		if err != nil {
			return err
		}
		return Render(ExperimentListResult{Experiments: items}, outputMode())
	}
	return cmd
}
func newExperimentGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Args: cobra.ExactArgs(1), Short: "Read a selected v2 experiment"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		item, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id)
		if err != nil {
			return err
		}
		return Render(item, outputMode())
	}
	return cmd
}
func newExperimentCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Args: cobra.ExactArgs(1), Short: "Create an app-bound v2 experiment"}
	experimentTargetFlags(cmd)
	experimentConfirmFlag(cmd)
	cmd.Flags().String("name", "", "internal experiment name")
	cmd.Flags().Int("traffic", 0, "traffic proportion, 1..100")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		traffic, _ := cmd.Flags().GetInt("traffic")
		if name == "" || traffic < 1 || traffic > 100 {
			return errors.New("--name and --traffic 1..100 required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, _, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		out, err := createOrReuseProductExperiment(cmd.Context(), c, appID, name, platform, traffic)
		if err != nil {
			return err
		}
		return Render(out, outputMode())
	}
	return cmd
}

func createOrReuseProductExperiment(ctx context.Context, c *asc.Client, appID, name, platform string, traffic int) (ExperimentActionResult, error) {
	items, err := asc.ListAppExperiments(ctx, c, appID)
	if err != nil {
		return ExperimentActionResult{}, err
	}
	var existing *asc.ProductExperiment
	for i := range items {
		if items[i].Name == name && items[i].Platform == platform {
			if existing != nil {
				return ExperimentActionResult{}, errors.New("multiple app experiments share selected name and platform")
			}
			existing = &items[i]
		}
	}
	if existing != nil {
		if existing.TrafficProportion != traffic {
			return ExperimentActionResult{}, fmt.Errorf("experiment %s already exists with different traffic", existing.ID)
		}
		return ExperimentActionResult{Action: "existing", Experiment: *existing, VersionAssociation: "not established by create API"}, nil
	}
	item, err := asc.CreateProductExperiment(ctx, c, appID, name, platform, traffic)
	if err != nil {
		return ExperimentActionResult{}, err
	}
	return ExperimentActionResult{Action: "created", Experiment: item, Changed: true, VersionAssociation: "not established by create API"}, nil
}
func newExperimentUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update <bundleId>", Args: cobra.ExactArgs(1), Short: "Update draft experiment name or traffic"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	experimentConfirmFlag(cmd)
	cmd.Flags().String("name", "", "new name")
	cmd.Flags().Int("traffic", 0, "new traffic proportion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		var name *string
		var traffic *int
		if cmd.Flags().Changed("name") {
			v, _ := cmd.Flags().GetString("name")
			name = &v
		}
		if cmd.Flags().Changed("traffic") {
			v, _ := cmd.Flags().GetInt("traffic")
			traffic = &v
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		if _, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id); err != nil {
			return err
		}
		item, changed, err := asc.UpdateProductExperiment(cmd.Context(), c, appID, id, name, traffic)
		if err != nil {
			return err
		}
		return Render(ExperimentActionResult{Action: "updated", Experiment: item, Changed: changed}, outputMode())
	}
	return cmd
}
func newExperimentDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Args: cobra.ExactArgs(1), Short: "Delete an unstarted experiment"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	experimentConfirmFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		item, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id)
		if err != nil {
			return err
		}
		if err := asc.DeleteProductExperiment(cmd.Context(), c, appID, id); err != nil {
			return err
		}
		return Render(ExperimentActionResult{Action: "deleted", Experiment: item, Changed: true}, outputMode())
	}
	return cmd
}
func newExperimentLifecycleCommand(start bool) *cobra.Command {
	verb := "stop"
	if start {
		verb = "start"
	}
	cmd := &cobra.Command{Use: verb + " <bundleId>", Args: cobra.ExactArgs(1), Short: verb + " a selected experiment"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	experimentConfirmFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		if _, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id); err != nil {
			return err
		}
		item, changed, err := asc.SetProductExperimentStarted(cmd.Context(), c, appID, id, start)
		if err != nil {
			return err
		}
		return Render(ExperimentActionResult{Action: verb, Experiment: item, Changed: changed}, outputMode())
	}
	return cmd
}
func newExperimentProposalCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "proposal <bundleId>", Args: cobra.ExactArgs(1), Short: "Read a review item proposal; does not attach or submit"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		if _, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id); err != nil {
			return err
		}
		proposal, err := asc.VerifyExperimentSubmissionProposal(cmd.Context(), c, appID, versionID, id)
		if err != nil {
			return err
		}
		return Render(ExperimentProposalResult{Proposal: proposal, VersionID: versionID}, outputMode())
	}
	return cmd
}
