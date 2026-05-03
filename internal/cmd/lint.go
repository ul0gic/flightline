// lint.go — `skipper lint <state.yaml>`.
//
// Offline-only preflight: load the state YAML, run every Mode=Offline rule
// in internal/lint, render diagnostics. Exit codes:
//
//	0 — clean (no diagnostics or info-only)
//	1 — at least one error-severity diagnostic
//	2 — only warnings (no errors)
//
// Output modes match the rest of Skipper: --output table | json. JSON is
// the LLM-stable contract; table is colorized humans.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ul0gic/skipper/internal/config"
	"github.com/ul0gic/skipper/internal/lint"
)

// LintResult is the JSON-stable envelope `lint` and `preflight` emit.
// Field names are the wire contract: adding fields is fine, renaming or
// removing is a breaking change.
type LintResult struct {
	BundleID    string            `json:"bundleId,omitempty"`
	Version     string            `json:"version,omitempty"`
	SourcePath  string            `json:"sourcePath,omitempty"`
	Mode        string            `json:"mode"`
	Diagnostics []lint.Diagnostic `json:"diagnostics"`
	Summary     LintResultSummary `json:"summary"`
}

// LintResultSummary counts diagnostics by severity for at-a-glance reads.
type LintResultSummary struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

// TableRows renders a one-row-per-diagnostic summary plus a footer with
// the summary counts.
func (r *LintResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"SEVERITY", "RULE", "PATH", "MESSAGE"}
	for _, d := range r.Diagnostics {
		rows = append(rows, []string{
			d.Severity.String(),
			d.RuleID,
			d.Path,
			d.Message,
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none)", "", "", "no diagnostics"})
	}
	return headers, rows
}

var lintCmd = &cobra.Command{
	Use:          "lint <state.yaml>",
	Short:        "Lint a state.yaml against Skipper's offline preflight rules",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runLint,
	Long: `lint runs every Skipper preflight rule that does not require live ASC
access against the supplied state.yaml. The check covers schema gaps the
JSON Schema validator cannot express (yes/no coercion, required-but-empty
fields, format: email shape) plus structural rules (localizations
completeness, screenshots required-devices).

Exit codes:
  0  clean (no diagnostics, or info-only)
  1  at least one error-severity diagnostic
  2  only warnings (no errors)

Use ` + "`--output json`" + ` for stable LLM/CI consumption; the table form is for
humans.`,
	Example: `  skipper lint state.yaml
  skipper lint state.yaml --output json | jq '.diagnostics[] | select(.severity=="error")'
  skipper lint state.yaml --output json | jq -r '.summary'`,
}

func init() {
	rootCmd.AddCommand(lintCmd)
}

func runLint(cmd *cobra.Command, args []string) error {
	src := args[0]
	abs, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve path %s: %w", src, err)
	}

	state, err := config.LoadState(abs)
	if err != nil {
		// Loader-level failures are file:line:col-anchored already; pass
		// them through so the user sees the parse problem before any rule
		// runs.
		return err
	}
	// Schema validation runs as part of lint so the user sees one merged
	// view: schema diagnostics first (mapped to the lint Diagnostic
	// shape), then rule diagnostics.
	schemaDiags := config.Validate(abs, state)

	rules := lint.Filter(lint.ModeOffline)
	runner := lint.NewRunner(rules)
	ctx := lint.CheckContext{
		State:      state,
		Live:       false,
		Ctx:        cmd.Context(),
		SourcePath: abs,
	}
	ruleDiags := runner.Run(ctx)

	merged := mergeSchemaIntoLint(schemaDiags, ruleDiags)

	out := &LintResult{
		BundleID:    state.Metadata.BundleID,
		Version:     state.Metadata.Version,
		SourcePath:  abs,
		Mode:        "lint",
		Diagnostics: merged,
		Summary:     summarize(merged),
	}
	if err := Render(out, outputMode()); err != nil {
		return err
	}

	// Exit code mapping. cobra's RunE returns drive a non-zero exit; we
	// use a custom error wrapper so main.go can map to 1 vs 2 in a
	// future iteration. For now: errors fail with `lint: errors present`,
	// warnings-only succeed silently (table renderer prints the warnings).
	if lint.HasErrors(merged) {
		return errLintErrors{count: out.Summary.Error}
	}
	if lint.HasWarnings(merged) {
		// Warnings: print to stderr but do not fail the command. Phase 5
		// gate will revisit exit-code-2 once main.go grows the mapping.
		fmt.Fprintf(os.Stderr, "lint: %d warning(s) — review diagnostics above\n", out.Summary.Warning)
	}
	return nil
}

// errLintErrors is the typed error returned when lint finds errors. main.go
// already exits 1 on any non-nil error, so the count is informational; a
// future change can map it to a distinct exit code.
type errLintErrors struct{ count int }

func (e errLintErrors) Error() string {
	if e.count == 1 {
		return "lint: 1 error-severity diagnostic — see output above"
	}
	return fmt.Sprintf("lint: %d error-severity diagnostics — see output above", e.count)
}

// mergeSchemaIntoLint converts config.Diagnostic (schema-validation output)
// into lint.Diagnostic so the JSON output is one shape. Schema findings are
// always SeverityError; we tag them with rule id "schema" so consumers can
// filter.
func mergeSchemaIntoLint(schema []config.Diagnostic, rules []lint.Diagnostic) []lint.Diagnostic {
	out := make([]lint.Diagnostic, 0, len(schema)+len(rules))
	for _, d := range schema {
		out = append(out, lint.Diagnostic{
			RuleID:   "schema",
			Severity: lint.SeverityError,
			Message:  d.Message,
			Path:     d.Path,
			FixHint: "fix the field to match the embedded JSON Schema. " +
				"`skipper lint <file>` shows every error before any wire call.",
			Reference: "schemas/skipper.schema.json",
		})
	}
	out = append(out, rules...)
	// Stable sort: rule, then path. Severity is already implicit in the
	// rule's id ordering. Schema diagnostics sort to the top because their
	// rule id is "schema" (alphabetically first among real rule prefixes).
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func summarize(d []lint.Diagnostic) LintResultSummary {
	s := LintResultSummary{}
	for _, x := range d {
		switch x.Severity {
		case lint.SeverityError:
			s.Error++
		case lint.SeverityWarning:
			s.Warning++
		case lint.SeverityInfo:
			s.Info++
		}
	}
	return s
}
