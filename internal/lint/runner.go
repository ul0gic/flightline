// Package lint implements Flightline's preflight rule engine.
// Panicking rules are trapped and converted to SeverityError so one bad rule never aborts a full run.
package lint

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// Severity classifies a diagnostic. Exit-code mapping: any Error => 1, only Warnings => 2, otherwise 0.
type Severity int

// Severity levels.
const (
	// SeverityInfo surfaces a hint. Never gates submission.
	SeverityInfo Severity = iota
	// SeverityWarning flags something Apple is unlikely to reject for; doesn't fail preflight if alone.
	SeverityWarning
	// SeverityError flags a known rejection cause or structural problem that blocks apply.
	SeverityError
)

// String returns the lowercase token used in JSON output and table rendering.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return fmt.Sprintf("severity(%d)", int(s))
	}
}

// MarshalJSON emits the lowercase string token so the JSON contract stays string-typed.
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON accepts the lowercase token so the JSON contract round-trips.
func (s *Severity) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return errors.New("severity: empty payload")
	}
	v := string(b)
	if v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	switch v {
	case "info":
		*s = SeverityInfo
	case "warning":
		*s = SeverityWarning
	case "error":
		*s = SeverityError
	default:
		return fmt.Errorf("severity: unknown token %q", v)
	}
	return nil
}

// Mode is a bitmask of the contexts a rule runs in.
type Mode int

// Mode bits.
const (
	// ModeOffline runs during `flightline lint` (no ASC client).
	ModeOffline Mode = 1 << iota
	// ModeLive runs during `flightline preflight` (ASC client available).
	ModeLive
	// ModeBoth runs in both contexts; rule should branch on CheckContext.Live.
	ModeBoth = ModeOffline | ModeLive
)

// CheckContext is the input every Rule receives. Client is nil during offline runs.
type CheckContext struct {
	State      *config.State // parsed YAML or projected live state
	Client     *asc.Client   // nil during offline runs
	BundleID   string
	Version    string
	Live       bool
	Ctx        context.Context
	SourcePath string // empty when no backing file (live-only preflight)
}

// Diagnostic is one finding produced by a rule. FixHint explains the remedy; Reference cites the guideline.
type Diagnostic struct {
	RuleID    string   `json:"ruleId"`
	Severity  Severity `json:"severity"`
	Message   string   `json:"message"`
	Path      string   `json:"path,omitempty"`
	FixHint   string   `json:"fixHint,omitempty"`
	Reference string   `json:"reference,omitempty"`
}

// Rule is one preflight check. IDs are stable kebab-case; renaming breaks consumers.
type Rule interface {
	ID() string
	Severity() Severity
	Mode() Mode
	// Doc is a one-line, plain-language description of the rejection this rule catches; it feeds the generated catalog.
	Doc() string
	Check(ctx CheckContext) []Diagnostic
}

// Runner executes rules in deterministic ID order and aggregates their diagnostics.
type Runner struct {
	rules []Rule
}

// NewRunner constructs a Runner over the given rules, sorted by ID for stable output.
func NewRunner(rules []Rule) *Runner {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID() < sorted[j].ID() })
	return &Runner{rules: sorted}
}

// Run evaluates every rule against ctx and returns aggregated diagnostics in stable ID order.
func (r *Runner) Run(ctx CheckContext) []Diagnostic {
	out := make([]Diagnostic, 0, len(r.rules))
	for _, rule := range r.rules {
		diags := safeCheck(rule, ctx)
		out = append(out, diags...)
	}
	return out
}

func safeCheck(rule Rule, ctx CheckContext) (diags []Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = []Diagnostic{{
				RuleID:   rule.ID(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("rule panicked: %v", r),
				FixHint:  "this is a Flightline bug; please file an issue at https://github.com/ul0gic/flightline/issues with the rule ID and your state.yaml.",
			}}
		}
	}()
	return rule.Check(ctx)
}

// HasErrors reports whether any diagnostic in d has SeverityError.
func HasErrors(d []Diagnostic) bool {
	for _, x := range d {
		if x.Severity == SeverityError {
			return true
		}
	}
	return false
}

// HasWarnings reports whether any diagnostic in d has SeverityWarning.
func HasWarnings(d []Diagnostic) bool {
	for _, x := range d {
		if x.Severity == SeverityWarning {
			return true
		}
	}
	return false
}
