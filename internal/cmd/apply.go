// apply.go — `skipper apply <state.yaml> [--confirm] [--resume] [--dry-run]`.
//
// Without --confirm, apply prints the plan and refuses to write — the
// terraform-style safety gate. With --confirm, it dispatches each
// change to the L1 write surface via state.Apply, persisting a
// per-change checkpoint so a Ctrl-C resumes cleanly.

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ul0gic/skipper/internal/config"
	"github.com/ul0gic/skipper/internal/plan"
	"github.com/ul0gic/skipper/internal/state"
)

// ApplyResult is the JSON-stable envelope `apply` emits in --output
// json mode. Stable: adding fields fine, removing/renaming a breaking
// change.
type ApplyResult struct {
	BundleID string              `json:"bundleId"`
	Version  string              `json:"version,omitempty"`
	DryRun   bool                `json:"dryRun,omitempty"`
	Applied  []plan.Change       `json:"applied"`
	Skipped  []plan.Change       `json:"skipped,omitempty"`
	Errors   []state.ChangeError `json:"errors,omitempty"`
}

// TableRows renders a one-row-per-change summary.
func (r *ApplyResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"STATUS", "OP", "PATH"}
	for _, c := range r.Applied {
		rows = append(rows, []string{"applied", string(c.Op), c.Path})
	}
	for _, c := range r.Skipped {
		rows = append(rows, []string{"skipped", string(c.Op), c.Path})
	}
	for _, e := range r.Errors {
		rows = append(rows, []string{"error", string(e.Change.Op), e.Change.Path})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none)", "", "no changes"})
	}
	return headers, rows
}

func init() {
	cmd := &cobra.Command{
		Use:   "apply <state.yaml>",
		Short: "Reconcile App Store Connect to match a state file",
		Long: `Loads <state.yaml>, validates it against the schema, fetches live
state, computes the diff, and writes the changes back to ASC.

Without --confirm, apply prints the plan and refuses to write — same
guardrail as terraform plan. With --confirm, every leaf-level change
dispatches to its L1 writer, with a checkpoint persisted after every
success so a Ctrl-C / crash mid-apply resumes cleanly via --resume.

--dry-run computes the dispatch path but never hits the wire; useful
for preview without --confirm.

Examples:
  skipper apply state.yaml                 # plan only, refuses to write
  skipper apply state.yaml --confirm       # write changes
  skipper apply state.yaml --confirm --resume   # continue after Ctrl-C
  skipper apply state.yaml --dry-run --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runApply,
	}
	cmd.Flags().Bool("confirm", false, "actually write changes (without this, apply is a plan)")
	cmd.Flags().Bool("resume", false, "resume from a previous interrupted apply")
	cmd.Flags().Bool("dry-run", false, "compute dispatch but make no API calls")
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

	live, err := state.Fetch(cmd.Context(), c, desired.Metadata.BundleID, state.FetchOpts{
		Version: version, Platform: platform,
	})
	if err != nil {
		return err
	}

	changes := plan.Diff(desired, live)
	confirm, _ := cmd.Flags().GetBool("confirm")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	resume, _ := cmd.Flags().GetBool("resume")

	// Without --confirm and without --dry-run, apply is plan + refuse.
	if !confirm && !dryRun {
		fmt.Fprintln(os.Stderr, "skipper: apply without --confirm prints the plan and exits — pass --confirm to write.")
		return Render(&PlanResult{
			BundleID: desired.Metadata.BundleID,
			Version:  version,
			Changes:  changes,
		}, outputMode())
	}

	state.SetApplyContext(desired.Metadata.BundleID, version, platform)
	defer state.ResetApplyContext()

	res, err := state.Apply(cmd.Context(), c, changes, state.ApplyOpts{
		Confirm:  confirm,
		Resume:   resume,
		DryRun:   dryRun,
		BundleID: desired.Metadata.BundleID,
		Logger: func(c plan.Change, status string) {
			fmt.Fprintf(os.Stderr, "skipper: %s %s %s\n", status, c.Op, c.Path)
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
