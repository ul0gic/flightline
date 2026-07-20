package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
	"github.com/ul0gic/flightline/internal/state"
)

// ApplyResult is the stable JSON envelope for `apply`; renaming or removing a field breaks consumers.
type ApplyResult struct {
	BundleID string              `json:"bundleId"`
	Version  string              `json:"version,omitempty"`
	DryRun   bool                `json:"dryRun,omitempty"`
	Applied  []plan.Change       `json:"applied"`
	Skipped  []plan.Change       `json:"skipped,omitempty"`
	Errors   []state.ChangeError `json:"errors,omitempty"`
}

func (r *ApplyResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"STATUS", "OP", "PATH", "ERROR"}
	for _, c := range r.Applied {
		rows = append(rows, []string{"applied", string(c.Op), c.Path, ""})
	}
	for _, c := range r.Skipped {
		rows = append(rows, []string{"skipped", string(c.Op), c.Path, ""})
	}
	for i := range r.Errors {
		e := &r.Errors[i]
		rows = append(rows, []string{"error", string(e.Change.Op), e.Change.Path, e.MessageText()})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none)", "", "no changes", ""})
	}
	return headers, rows
}

func init() {
	cmd := &cobra.Command{
		Use:   "apply <state.yaml>",
		Short: "Reconcile App Store Connect to match a state file",
		Long: `Loads <state.yaml>, validates it against the schema, fetches live
state, computes the diff, and writes the changes back to ASC.

Without --confirm, apply prints the plan and refuses to write: same
guardrail as terraform plan. With --confirm, every leaf-level change
dispatches to its L1 writer, with a checkpoint persisted after every
success so a Ctrl-C / crash mid-apply resumes cleanly via --resume.

--dry-run fetches live state and computes the dispatch path, but sends
no mutating API requests. It requires credentials and network access.

Examples:
  flightline apply state.yaml                 # plan only, refuses to write
  flightline apply state.yaml --confirm       # write changes
  flightline apply state.yaml --confirm --resume   # continue after Ctrl-C
  flightline apply state.yaml --dry-run --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runApply,
	}
	cmd.Flags().Bool("confirm", false, "actually write changes (without this, apply is a plan)")
	cmd.Flags().Bool("resume", false, "resume from a previous interrupted apply")
	cmd.Flags().Bool("dry-run", false, "fetch live state and compute dispatch without mutating ASC")
	cmd.Flags().String("version", "", "override metadata.version from the state file")
	cmd.Flags().String("platform", "", "override metadata.platform from the state file")
	rootCmd.AddCommand(cmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	stateFile := args[0]

	desired, err := config.LoadState(stateFile)
	if err != nil {
		return err
	}
	if diags := config.Validate(stateFile, desired); len(diags) > 0 {
		for _, d := range diags {
			fmt.Fprintln(os.Stderr, d.String())
		}
		return fmt.Errorf("apply: %s failed schema validation (%d diagnostics)", stateFile, len(diags))
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	return runApplyWithClient(cmd, stateFile, desired, c)
}

func runApplyWithClient(cmd *cobra.Command, stateFile string, desired *config.State, c *asc.Client) error {
	versionOverride, _ := cmd.Flags().GetString("version")
	platformOverride, _ := cmd.Flags().GetString("platform")
	version := desired.Metadata.Version
	if versionOverride != "" {
		version = versionOverride
	}
	platform := desired.Metadata.Platform
	if platformOverride != "" {
		platform = platformOverride
	}
	if platform == "" {
		platform = "IOS"
	}

	live, err := state.Fetch(cmd.Context(), c, desired.Metadata.BundleID, state.FetchOpts{
		Version: version, Platform: platform, RequireEditable: true,
	})
	if err != nil {
		return err
	}

	changes := plan.Diff(desired, live)
	confirm, _ := cmd.Flags().GetBool("confirm")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	resume, _ := cmd.Flags().GetBool("resume")

	if !confirm && !dryRun {
		fmt.Fprintln(os.Stderr, "flightline: apply without --confirm prints the plan and exits: pass --confirm to write.")
		return Render(&PlanResult{
			BundleID: desired.Metadata.BundleID,
			Version:  version,
			Changes:  changes,
		}, outputMode())
	}

	stateDir, _ := filepath.Abs(filepath.Dir(stateFile))
	res, err := state.Apply(cmd.Context(), c, changes, state.ApplyOpts{
		Context: state.ApplyContext{
			BundleID: desired.Metadata.BundleID,
			Version:  version,
			Platform: platform,
			StateDir: stateDir,
		},
		Confirm: confirm,
		Resume:  resume,
		DryRun:  dryRun,
		Logger: func(c plan.Change, status string) {
			fmt.Fprintf(os.Stderr, "flightline: %s %s %s\n", status, c.Op, c.Path)
		},
	})
	out := &ApplyResult{
		BundleID: desired.Metadata.BundleID,
		Version:  version,
		DryRun:   dryRun,
	}
	if res != nil {
		out.Applied = res.Applied
		out.Skipped = res.Skipped
		out.Errors = res.Errors
	}
	if rerr := Render(out, outputMode()); rerr != nil && err == nil {
		return rerr
	}
	return err
}
