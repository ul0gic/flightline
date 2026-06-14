package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/lint"
	"github.com/ul0gic/flightline/internal/state"
)

var preflightCmd = &cobra.Command{
	Use:          "preflight <bundleId>",
	Short:        "Run live + offline preflight rules against an App Store version",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPreflight,
	Long: `preflight runs every Flightline rejection-prevention rule against a live
App Store version. Live rules query the ASC API for IAP attachment,
build state, age-rating completeness, and screenshot device coverage.
Offline rules run too when --state-file is provided so authoring
mistakes are caught alongside live ones.

Without --state-file the live state is fetched and used as the rule
input: useful for "is the version actually submittable right now?"
checks against any app you have credentials for. With --state-file the
user-authored YAML is the input for offline rules and the live ASC
state is consulted for live rules.

Exit codes:
  0  clean (no diagnostics, or info-only)
  1  at least one error-severity diagnostic
  2  only warnings (no errors)`,
	Example: `  flightline preflight com.example.myapp --version 1.0.1
  flightline preflight com.example.myapp --version 1.0.1 --state-file state.yaml
  flightline preflight com.example.myapp --version 1.0.1 --output json | jq '.summary'`,
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
		return lintFailedError{count: out.Summary.Error}
	}
	return nil
}

// With --state-file: load + schema-validate the YAML. Without: fetch live
// ASC state; offline rules guard on "is X managed?" so they no-op on it.
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
