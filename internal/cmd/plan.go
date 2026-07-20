package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
	"github.com/ul0gic/flightline/internal/state"
)

// PlanResult is the JSON-stable envelope the `plan` command emits.
type PlanResult struct {
	BundleID string        `json:"bundleId"`
	Version  string        `json:"version,omitempty"`
	Changes  []plan.Change `json:"changes"`
}

func (p *PlanResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"OP", "PATH", "FROM", "TO"}
	rows = make([][]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		rows = append(rows, []string{
			string(c.Op),
			c.Path,
			truncForTable(fmt.Sprintf("%v", c.From), 40),
			truncForTable(fmt.Sprintf("%v", c.To), 40),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none)", "no changes", "", ""})
	}
	return headers, rows
}

func truncForTable(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func init() {
	cmd := &cobra.Command{
		Use:   "plan <state.yaml>",
		Short: "Diff a state file against live ASC state",
		Long: `Loads <state.yaml>, validates it against the embedded JSON Schema,
fetches live ASC state for the same bundleId/version, and prints the
change set the apply command would make.

Read-only: never writes. Exit code 0 always (diffs aren't errors)
unless --exit-on-changes is set, in which case the command returns
exit code 2 when changes exist (useful in CI hooks).

Examples:
  flightline plan state.yaml
  flightline plan state.yaml --output json | jq '.changes | length'
  flightline plan state.yaml --exit-on-changes`,
		Args: cobra.ExactArgs(1),
		RunE: runPlan,
	}
	cmd.Flags().Bool("exit-on-changes", false, "exit 2 when changes exist (for CI)")
	cmd.Flags().String("version", "", "override metadata.version from the state file")
	cmd.Flags().String("platform", "", "override metadata.platform from the state file")
	rootCmd.AddCommand(cmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	stateFile := args[0]

	desired, err := config.LoadState(stateFile)
	if err != nil {
		return err
	}
	if diags := config.Validate(stateFile, desired); len(diags) > 0 {
		for _, d := range diags {
			fmt.Fprintln(os.Stderr, d.String())
		}
		return fmt.Errorf("plan: %s failed schema validation (%d diagnostics)", stateFile, len(diags))
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
		Version: version, Platform: platform, RequireEditable: true,
	})
	if err != nil {
		return err
	}

	changes := plan.Diff(desired, live)
	result := &PlanResult{
		BundleID: desired.Metadata.BundleID,
		Version:  version,
		Changes:  changes,
	}

	if err := Render(result, outputMode()); err != nil {
		return err
	}

	exitOnChanges, _ := cmd.Flags().GetBool("exit-on-changes")
	if exitOnChanges && len(changes) > 0 {
		return &ExitError{Code: 2, Message: fmt.Sprintf("%d change(s): exiting 2 per --exit-on-changes", len(changes))}
	}
	return nil
}
