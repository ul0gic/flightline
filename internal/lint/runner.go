// Package lint implements Skipper's L3 preflight rule engine — the
// rejection-prevention layer.
//
// Two execution shapes:
//
//  1. Offline (skipper lint <state.yaml>) — pure structural checks against the
//     authored YAML. No network. Catches authoring mistakes before any wire
//     call.
//  2. Live (skipper preflight <bundleId> --version <v>) — fetches the live
//     state from ASC and runs Live + Both rules against it. Catches the
//     "READY_TO_SUBMIT but not in submission items" class of bug that only
//     surfaces against real Apple state.
//
// Rules self-register via package init() into a process-wide registry; see
// rules.go. The Runner here is the executor — it iterates rules in stable ID
// order, invokes Check, and collects diagnostics.
//
// A single rule that panics MUST NOT bring down preflight; the Runner traps
// panics and converts them to SeverityError diagnostics. Apple-rejection
// prevention is the user-facing promise; partial coverage with one bad rule
// is strictly better than no coverage at all.
package lint

import (
	"context"
	"fmt"
	"sort"

	"github.com/ul0gic/skipper/internal/asc"
	"github.com/ul0gic/skipper/internal/config"
)

// Severity classifies a diagnostic. The Runner's exit-code mapping uses these:
// any Error => exit 1, only Warnings => exit 2, otherwise 0.
type Severity int

// Severity levels.
const (
	// SeverityInfo surfaces a hint or context entry. Never gates submission.
	SeverityInfo Severity = iota
	// SeverityWarning flags something the user should address but Apple is
	// unlikely to reject for. Still printed; doesn't fail preflight if alone.
	SeverityWarning
	// SeverityError flags a known rejection cause or a structural problem
	// that will block apply. Preflight exits non-zero.
	SeverityError
)

// String renders a Severity as the canonical lowercase token used in JSON
// output and table rendering.
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

// MarshalJSON emits the lowercase token form so the JSON --output contract
// stays string-typed (consumers never have to map ints to names).
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON accepts the lowercase token form so the JSON contract
// round-trips through json.Decode. Test code that re-parses the output of
// `skipper lint --output json` relies on this.
func (s *Severity) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return fmt.Errorf("severity: empty payload")
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

// Mode is a bitmask of the contexts a rule runs in. Offline-only rules
// declare ModeOffline; live-only declare ModeLive; rules that work in both
// shapes declare ModeBoth and inspect CheckContext.Live to specialize.
type Mode int

// Mode bits.
const (
	// ModeOffline runs during `skipper lint` (no ASC client).
	ModeOffline Mode = 1 << iota
	// ModeLive runs during `skipper preflight` (ASC client available).
	ModeLive
	// ModeBoth runs in both shapes. The rule should branch on
	// CheckContext.Live when its check requires live data.
	ModeBoth = ModeOffline | ModeLive
)

// CheckContext is the input every Rule receives.
//
// State carries the parsed YAML (offline) or the projected live state
// (preflight with --state-file unset). When --state-file is provided to
// preflight, both authored State and live ASC are available — the rule
// inspects whichever it needs.
//
// Client is nil during offline runs. Live rules MUST guard with `if
// c.Client == nil` and return no diagnostics rather than panicking — a
// future caller could pass a Live mode without a client.
//
// BundleID and Version are populated during live runs so rules can target
// the right resource; offline rules can read State.Metadata for the same.
type CheckContext struct {
	// State is the parsed YAML or projected live state. Never nil during a
	// well-formed run; rules should still guard before deref.
	State *config.State
	// Client is the ASC HTTP client. nil during offline runs.
	Client *asc.Client
	// BundleID is the ASC bundle identifier (e.g. com.example.myapp).
	BundleID string
	// Version is the App Store version string (e.g. 1.0.1).
	Version string
	// Live indicates which mode the rule is running under. ModeBoth rules
	// branch on this to decide whether to issue live calls.
	Live bool
	// Ctx carries request-scoped cancellation for live calls.
	Ctx context.Context
	// SourcePath is the absolute filesystem path of the state.yaml the
	// rules are linting, when known. The "strict" rules (yaml-coercion,
	// required-nonzero, format-email) read it to access the raw bytes
	// before the structural decode strips comments/quoting/etc. Empty
	// when the runner is invoked without a backing file (e.g. live-only
	// preflight against fetched state).
	SourcePath string
}

// Diagnostic is one finding produced by a rule. Path is a JSON-Pointer-style
// reference into the state document (or empty for whole-document findings).
//
// FixHint is mandatory in spirit — every diagnostic should tell the user how
// to fix the problem, not just what's wrong. Reference points at the source
// of the rule (Apple guideline number, PRD section) so users can verify why
// the check exists.
type Diagnostic struct {
	RuleID    string   `json:"ruleId"`
	Severity  Severity `json:"severity"`
	Message   string   `json:"message"`
	Path      string   `json:"path,omitempty"`
	FixHint   string   `json:"fixHint,omitempty"`
	Reference string   `json:"reference,omitempty"`
}

// Rule is one preflight check. Implementations are typically tiny types with
// no fields — they declare ID/Severity/Mode and implement Check.
//
// IDs are stable, kebab-case, dot-separated by domain
// (e.g. "iap.attached-to-review-submission"). The ID is part of the
// JSON output contract — consumers grep on it. Renaming breaks consumers.
type Rule interface {
	// ID is the stable kebab-case identifier. Used to ask "did this rule
	// fire?" in tests, scripts, and CI gating.
	ID() string
	// Severity is the rule's default severity. Diagnostics may override per
	// finding (some rules emit multiple severities), but most match this.
	Severity() Severity
	// Mode is the bitmask of contexts where the rule should run.
	Mode() Mode
	// Check evaluates the rule and returns zero or more Diagnostics. Returning
	// nil means "no findings". Errors propagate as SeverityError diagnostics
	// (the Runner traps panics).
	Check(ctx CheckContext) []Diagnostic
}

// Runner executes a set of rules in deterministic ID order and aggregates
// their diagnostics. Use NewRunner directly with a filtered slice from the
// registry, or call Filter+NewRunner to compose.
type Runner struct {
	rules []Rule
}

// NewRunner constructs a Runner over the given rules. Rules are sorted by ID
// at construction time so Run always emits diagnostics in the same order
// across processes — the JSON output contract requires stable ordering.
func NewRunner(rules []Rule) *Runner {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID() < sorted[j].ID() })
	return &Runner{rules: sorted}
}

// Run evaluates every rule in the runner against ctx and returns the
// aggregated diagnostics. Diagnostics are returned in (ruleID, append-order)
// composite order: rules are visited by sorted ID, and each rule's findings
// appear in the order Check returned them.
//
// A panicking rule is converted to a SeverityError diagnostic carrying the
// rule's ID and the panic value, then Run continues. Rule authors that need
// fatal-error semantics should return a SeverityError diagnostic explicitly.
func (r *Runner) Run(ctx CheckContext) []Diagnostic {
	out := make([]Diagnostic, 0, len(r.rules))
	for _, rule := range r.rules {
		diags := safeCheck(rule, ctx)
		out = append(out, diags...)
	}
	return out
}

// safeCheck invokes rule.Check with a panic recovery so one bad rule can't
// take down preflight. The recovered value is rendered as a SeverityError
// diagnostic so the user sees what crashed.
func safeCheck(rule Rule, ctx CheckContext) (diags []Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = []Diagnostic{{
				RuleID:   rule.ID(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("rule panicked: %v", r),
				FixHint:  "this is a Skipper bug; please file an issue at https://github.com/ul0gic/skipper/issues with the rule ID and your state.yaml.",
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
