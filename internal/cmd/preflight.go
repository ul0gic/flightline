// preflight.go — `skipper preflight <bundleId> --version <v> [--state-file path]`.
//
// Live preflight: builds an authenticated ASC client, fetches the live
// state for the version (or loads --state-file when provided), and runs
// every Mode=Live + Mode=Both rule.
//
// Output and exit-code conventions match `skipper lint` so users can pipe
// either through the same tooling.

package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ul0gic/skipper/internal/asc"
	"github.com/ul0gic/skipper/internal/config"
	"github.com/ul0gic/skipper/internal/lint"
	"github.com/ul0gic/skipper/internal/state"
)

var preflightCmd = &cobra.Command{
	Use:          "preflight <bundleId>",
	Short:        "Run live + offline preflight rules against an App Store version",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPreflight,
	Long: `preflight runs every Skipper rejection-prevention rule against a live
App Store version. Live rules query the ASC API for IAP attachment,
build state, age-rating completeness, and screenshot device coverage.
Offline rules run too when --state-file is provided so authoring
mistakes are caught alongside live ones.

Without --state-file the live state is fetched and used as the rule
input — useful for "is the version actually submittable right now?"
checks against any app you have credentials for. With --state-file the
user-authored YAML is the input for offline rules and the live ASC
state is consulted for live rules.

Exit codes:
  0  clean (no diagnostics, or info-only)
  1  at least one error-severity diagnostic
  2  only warnings (no errors)`,
	Example: `  skipper preflight com.example.myapp --version 1.0.1
  skipper preflight com.example.myapp --version 1.0.1 --state-file state.yaml
  skipper preflight com.example.myapp --version 1.0.1 --output json | jq '.summary'`,
}

var (
	preflightVersion   string
	preflightPlatform  string
	preflightStateFile string
)

func init() {
	preflightCmd.Flags().StringVar(&preflightVersion, "version", "", "App Store version string (e.g. 1.0.1)")
	preflightCmd.Flags().StringVar(&preflightPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	preflightCmd.Flags().StringVar(&preflightStateFile, "state-file", "", "optional state.yaml; offline rules also run against it")
	_ = preflightCmd.MarkFlagRequired("version")
	rootCmd.AddCommand(preflightCmd)
}

func runPreflight(cmd *cobra.Command, args []string) error {
	bundleID := strings.TrimSpace(args[0])
	versionStr := strings.TrimSpace(preflightVersion)
	platform := strings.TrimSpace(preflightPlatform)
	if platform == "" {
		platform = "IOS"
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	stateInput, sourcePath, schemaDiags, err := resolvePreflightState(cmd.Context(), c, bundleID, versionStr, platform)
	if err != nil {
		return err
	}

	// Run every registered rule. ModeBoth rules branch on Live; live-only
	// rules see Live=true and the populated client; offline rules see
	// State and a SourcePath when --state-file was provided.
	rules := lint.All()
	runner := lint.NewRunner(rules)
	checkCtx := lint.CheckContext{
		State:      stateInput,
		Client:     c,
		BundleID:   bundleID,
		Version:    versionStr,
		Live:       true,
		Ctx:        cmd.Context(),
		SourcePath: sourcePath,
	}
	ruleDiags := runner.Run(checkCtx)
	merged := mergeSchemaIntoLint(schemaDiags, ruleDiags)

	out := &LintResult{
		BundleID:    bundleID,
		Version:     versionStr,
		SourcePath:  sourcePath,
		Mode:        "preflight",
		Diagnostics: merged,
		Summary:     summarize(merged),
	}
	if err := Render(out, outputMode()); err != nil {
		return err
	}
	if lint.HasErrors(merged) {
		return errLintErrors{count: out.Summary.Error}
	}
	return nil
}

// resolvePreflightState picks the *State to feed the rules.
//
// Two paths:
//  1. --state-file given: load + schema-validate the YAML, return the
//     parsed *State and the absolute source path so strict rules can
//     read it.
//  2. no --state-file: fetch live ASC state and use it as the input.
//     Offline rules guard with "is X managed?" so they no-op cleanly
//     when fed live state.
func resolvePreflightState(ctx context.Context, c *asc.Client, bundleID, versionStr, platform string) (*config.State, string, []config.Diagnostic, error) {
	if preflightStateFile != "" {
		abs, err := filepath.Abs(preflightStateFile)
		if err != nil {
			return nil, "", nil, fmt.Errorf("resolve --state-file: %w", err)
		}
		st, err := config.LoadState(abs)
		if err != nil {
			return nil, "", nil, err
		}
		return st, abs, config.Validate(abs, st), nil
	}
	live, err := state.Fetch(ctx, c, bundleID, state.FetchOpts{
		Version:  versionStr,
		Platform: platform,
	})
	if err != nil {
		return nil, "", nil, fmt.Errorf("fetch live state: %w", err)
	}
	return live, "", nil, nil
}
