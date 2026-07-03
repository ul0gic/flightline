package cmd

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/lint"
)

// LintResult is the JSON-stable envelope `lint` and `preflight` emit.
type LintResult struct {
	BundleID    string            `json:"bundleId,omitempty"`
	Version     string            `json:"version,omitempty"`
	SourcePath  string            `json:"sourcePath,omitempty"`
	Mode        string            `json:"mode"`
	Diagnostics []lint.Diagnostic `json:"diagnostics"`
	Summary     LintResultSummary `json:"summary"`
}

type LintResultSummary struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

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
	Short:        "Lint a state.yaml against Flightline's offline preflight rules",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runLint,
	Long: `lint runs every Flightline preflight rule that does not require live ASC
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
	Example: `  flightline lint state.yaml
  flightline lint state.yaml --output json | jq '.diagnostics[] | select(.severity=="error")'
  flightline lint state.yaml --output json | jq -r '.summary'`,
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
		return err
	}
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
	return diagnosticsExit(out.Mode, out.Summary)
}

// mergeSchemaIntoLint converts schema diagnostics into lint.Diagnostic so the
// JSON output is one shape; schema findings are always SeverityError, rule id "schema".
func mergeSchemaIntoLint(schema []config.Diagnostic, rules []lint.Diagnostic) []lint.Diagnostic {
	out := make([]lint.Diagnostic, 0, len(schema)+len(rules))
	for _, d := range schema {
		out = append(out, lint.Diagnostic{
			RuleID:   "schema",
			Severity: lint.SeverityError,
			Message:  d.Message,
			Path:     d.Path,
			FixHint: "fix the field to match the embedded JSON Schema. " +
				"`flightline lint <file>` shows every error before any wire call.",
			Reference: "schemas/flightline.schema.json",
		})
	}
	out = append(out, rules...)
	// Sort by rule then path; "schema" sorts to the top alphabetically.
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
