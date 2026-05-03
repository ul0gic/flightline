package lint

import (
	"context"
	"strings"
	"testing"
)

func TestVersionAccountDeletionAttested_AlwaysEmitsInfo(t *testing.T) {
	got := versionAccountDeletionAttestedRule{}.Check(CheckContext{Ctx: context.Background()})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "version.account-deletion-attested" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityInfo {
		t.Errorf("severity = %v, want info", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "Account Deletion") {
		t.Errorf("message did not reference Account Deletion: %q", got[0].Message)
	}
	if got[0].Reference != "Apple Guideline 5.1.1(v)" {
		t.Errorf("reference = %q", got[0].Reference)
	}
}

func TestVersionAccountDeletionAttested_LiveAndOfflineSame(t *testing.T) {
	off := versionAccountDeletionAttestedRule{}.Check(CheckContext{Ctx: context.Background(), Live: false})
	live := versionAccountDeletionAttestedRule{}.Check(CheckContext{Ctx: context.Background(), Live: true})
	if len(off) != len(live) {
		t.Fatalf("offline=%d live=%d, want equal", len(off), len(live))
	}
	if off[0].Message != live[0].Message {
		t.Errorf("messages differ between modes")
	}
}
