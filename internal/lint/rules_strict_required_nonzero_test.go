package lint

import (
	"context"
	"strings"
	"testing"
)

func TestStrictRequiredNonzero_FiresWhenTesterMissingEmail(t *testing.T) {
	p := writeTempYAML(t, `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0"
spec:
  testflight:
    groups:
      internal:
        testers:
          - firstName: NoEmail
`)
	got := strictRequiredNonzeroRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "strict.required-nonzero" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if !strings.Contains(got[0].Path, "/spec/testflight/groups/internal/testers/0/email") {
		t.Errorf("path = %q", got[0].Path)
	}
}

func TestStrictRequiredNonzero_FiresWhenTesterEmailEmpty(t *testing.T) {
	p := writeTempYAML(t, `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0"
spec:
  testflight:
    groups:
      external:
        testers:
          - firstName: Sam
            email: ""
`)
	got := strictRequiredNonzeroRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
}

func TestStrictRequiredNonzero_NoOpWhenAllTestersHaveEmail(t *testing.T) {
	p := writeTempYAML(t, `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0"
spec:
  testflight:
    groups:
      internal:
        testers:
          - email: a@example.com
          - email: b@example.com
`)
	got := strictRequiredNonzeroRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0: %+v", len(got), got)
	}
}

func TestStrictRequiredNonzero_NoOpWithoutSourcePath(t *testing.T) {
	got := strictRequiredNonzeroRule{}.Check(CheckContext{Ctx: context.Background()})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 with empty SourcePath", len(got))
	}
}
