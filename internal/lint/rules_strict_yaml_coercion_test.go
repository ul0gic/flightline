package lint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestStrictYAMLCoercion_FiresOnUnquotedYesForBoolField(t *testing.T) {
	p := writeTempYAML(t, `apiVersion: skipper.corelift.io/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0.1"
spec:
  ageRating:
    gambling: yes
`)
	got := strictYAMLCoercionRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "strict.yaml-coercion" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "gambling") {
		t.Errorf("message did not include field name: %q", got[0].Message)
	}
}

func TestStrictYAMLCoercion_FiresOnQuotedYesForBoolField(t *testing.T) {
	p := writeTempYAML(t, `apiVersion: skipper.corelift.io/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0.1"
spec:
  ageRating:
    gambling: "yes"
`)
	got := strictYAMLCoercionRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "quoted") {
		t.Errorf("message should call out quoted-string mismatch: %q", got[0].Message)
	}
}

func TestStrictYAMLCoercion_NoOpOnTrueFalse(t *testing.T) {
	p := writeTempYAML(t, `apiVersion: skipper.corelift.io/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0.1"
spec:
  ageRating:
    gambling: false
`)
	got := strictYAMLCoercionRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0: %+v", len(got), got)
	}
}

func TestStrictYAMLCoercion_NoOpOnUnquotedYesForNonBoolField(t *testing.T) {
	// `name` is a string field; `yes` written there is the user's actual
	// content. We do not flag because that would be too noisy.
	p := writeTempYAML(t, `apiVersion: skipper.corelift.io/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0.1"
spec:
  iap:
    products:
      com.example.x.lifetime:
        type: NON_CONSUMABLE
        name: yes
`)
	got := strictYAMLCoercionRule{}.Check(CheckContext{Ctx: context.Background(), SourcePath: p})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 (non-bool field): %+v", len(got), got)
	}
}

func TestStrictYAMLCoercion_NoOpWhenNoSourcePath(t *testing.T) {
	got := strictYAMLCoercionRule{}.Check(CheckContext{Ctx: context.Background()})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 with empty SourcePath", len(got))
	}
}
