package lint

import (
	"context"
	"strings"
	"testing"

	"github.com/ul0gic/skipper/internal/config"
)

func ptr(s string) *string { return &s }

func TestStrictFormatEmail_FiresOnReviewerDemo(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		ReviewerDemo: &config.ReviewerDemoSpec{ContactEmail: ptr("joe at example dot com")},
	}}
	got := strictFormatEmailRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "strict.format-email" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", got[0].Severity)
	}
}

func TestStrictFormatEmail_FiresOnTester(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"internal": {Testers: []config.TestFlightTester{{Email: "no-at-sign"}}},
		}},
	}}
	got := strictFormatEmailRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Path, "/testflight/groups/internal/testers/0/email") {
		t.Errorf("path = %q", got[0].Path)
	}
}

func TestStrictFormatEmail_NoOpOnValidEmails(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		ReviewerDemo: &config.ReviewerDemoSpec{ContactEmail: ptr("alice+ci@example.com")},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"internal": {Testers: []config.TestFlightTester{{Email: "tester@example.co.uk"}}},
		}},
	}}
	got := strictFormatEmailRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0: %+v", len(got), got)
	}
}

func TestStrictFormatEmail_SkipsEmptyEmail(t *testing.T) {
	// Empty is the strict.required-nonzero rule's domain — don't double-report.
	s := &config.State{Spec: config.StateSpec{
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"internal": {Testers: []config.TestFlightTester{{Email: ""}}},
		}},
	}}
	got := strictFormatEmailRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 (empty handled elsewhere): %+v", len(got), got)
	}
}
