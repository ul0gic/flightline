package cmd

import (
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

// TestPlanResult_TableRowsHeaders — the JSON-stable contract: column
// headers don't drift.
func TestPlanResult_TableRowsHeaders(t *testing.T) {
	pr := &PlanResult{}
	headers, rows := pr.TableRows()
	wantHeaders := []string{"OP", "PATH", "FROM", "TO"}
	if len(headers) != len(wantHeaders) {
		t.Fatalf("headers len %d, want %d", len(headers), len(wantHeaders))
	}
	for i, h := range wantHeaders {
		if headers[i] != h {
			t.Errorf("headers[%d]=%q want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 || rows[0][0] != "(none)" {
		t.Errorf("expected single (none) row for empty plan; got %+v", rows)
	}
}

// TestPlanResult_TableRowsContents — change values render through the
// truncating formatter.
func TestPlanResult_TableRowsContents(t *testing.T) {
	pr := &PlanResult{Changes: []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/version/copyright", From: "© 2025", To: "© 2026"},
	}}
	_, rows := pr.TableRows()
	if len(rows) != 1 {
		t.Fatalf("rows len %d, want 1", len(rows))
	}
	if rows[0][0] != "update" {
		t.Errorf("op cell = %q, want update", rows[0][0])
	}
	if rows[0][1] != "/spec/version/copyright" {
		t.Errorf("path cell = %q", rows[0][1])
	}
}

func TestTruncForTable(t *testing.T) {
	if got := truncForTable("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("x", 100)
	got := truncForTable(long, 10)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("trunc result missing ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, "xxxxxxxxx") { // 9 x's then "…"
		t.Errorf("trunc result wrong prefix: %q", got)
	}
}
