package lint

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ul0gic/skipper/internal/config"
)

// stubRule is a fixed-output Rule used to drive Runner tests without standing
// up real check logic.
type stubRule struct {
	id    string
	sev   Severity
	mode  Mode
	diags []Diagnostic
	panic any
}

func (s *stubRule) ID() string         { return s.id }
func (s *stubRule) Severity() Severity { return s.sev }
func (s *stubRule) Mode() Mode         { return s.mode }
func (s *stubRule) Check(_ CheckContext) []Diagnostic {
	if s.panic != nil {
		panic(s.panic)
	}
	return s.diags
}

func TestSeverity_String(t *testing.T) {
	cases := []struct {
		sev  Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
	}
	for _, tc := range cases {
		if got := tc.sev.String(); got != tc.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tc.sev, got, tc.want)
		}
	}
}

func TestSeverity_MarshalJSON(t *testing.T) {
	d := Diagnostic{RuleID: "x", Severity: SeverityWarning, Message: "m"}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back struct {
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Severity != "warning" {
		t.Errorf("severity round-trip = %q, want warning", back.Severity)
	}
}

func TestRunner_RunsRulesInIDOrder(t *testing.T) {
	a := &stubRule{id: "z.last", mode: ModeOffline, diags: []Diagnostic{{RuleID: "z.last", Severity: SeverityWarning, Message: "z"}}}
	b := &stubRule{id: "a.first", mode: ModeOffline, diags: []Diagnostic{{RuleID: "a.first", Severity: SeverityError, Message: "a"}}}
	r := NewRunner([]Rule{a, b})
	got := r.Run(CheckContext{Ctx: context.Background()})
	if len(got) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(got))
	}
	if got[0].RuleID != "a.first" || got[1].RuleID != "z.last" {
		t.Errorf("rule order = [%s, %s], want [a.first, z.last]", got[0].RuleID, got[1].RuleID)
	}
}

func TestRunner_PanicTrappedAsErrorDiag(t *testing.T) {
	r := NewRunner([]Rule{&stubRule{id: "boom", mode: ModeOffline, panic: "kaboom"}})
	got := r.Run(CheckContext{Ctx: context.Background()})
	if len(got) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(got))
	}
	if got[0].Severity != SeverityError {
		t.Errorf("panic diag severity = %v, want error", got[0].Severity)
	}
	if got[0].RuleID != "boom" {
		t.Errorf("panic diag rule id = %q, want boom", got[0].RuleID)
	}
}

func TestHasErrors_HasWarnings(t *testing.T) {
	d := []Diagnostic{
		{RuleID: "a", Severity: SeverityInfo},
		{RuleID: "b", Severity: SeverityWarning},
	}
	if HasErrors(d) {
		t.Error("HasErrors with warning-only = true, want false")
	}
	if !HasWarnings(d) {
		t.Error("HasWarnings with warning = false, want true")
	}
	d = append(d, Diagnostic{RuleID: "c", Severity: SeverityError})
	if !HasErrors(d) {
		t.Error("HasErrors with error = false, want true")
	}
}

func TestRegistry_RegisterFilterAll(t *testing.T) {
	t.Cleanup(reset)
	reset()
	off := &stubRule{id: "off", mode: ModeOffline}
	live := &stubRule{id: "live", mode: ModeLive}
	both := &stubRule{id: "both", mode: ModeBoth}
	Register(off)
	Register(live)
	Register(both)

	if got := len(All()); got != 3 {
		t.Fatalf("All() len = %d, want 3", got)
	}
	if got := len(Filter(ModeOffline)); got != 2 {
		t.Errorf("Filter(Offline) = %d rules, want 2 (off+both)", got)
	}
	if got := len(Filter(ModeLive)); got != 2 {
		t.Errorf("Filter(Live) = %d rules, want 2 (live+both)", got)
	}
}

func TestRegistry_ReregisterReplacesByID(t *testing.T) {
	t.Cleanup(reset)
	reset()
	Register(&stubRule{id: "x", sev: SeverityWarning, mode: ModeOffline})
	Register(&stubRule{id: "x", sev: SeverityError, mode: ModeOffline})
	all := All()
	if len(all) != 1 {
		t.Fatalf("len(All) = %d, want 1 (re-register replaces)", len(all))
	}
	if all[0].Severity() != SeverityError {
		t.Errorf("severity after re-register = %v, want error (latest wins)", all[0].Severity())
	}
}

// _ avoids unused-import warning when no rule files are present yet.
var _ = config.State{}
