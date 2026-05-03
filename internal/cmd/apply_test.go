package cmd

import (
	"errors"
	"testing"

	"github.com/ul0gic/skipper/internal/plan"
	"github.com/ul0gic/skipper/internal/state"
)

// TestApplyResult_TableEmpty — no changes renders the (none) row.
func TestApplyResult_TableEmpty(t *testing.T) {
	r := &ApplyResult{}
	headers, rows := r.TableRows()
	if len(headers) != 3 {
		t.Errorf("headers len %d, want 3", len(headers))
	}
	if len(rows) != 1 || rows[0][0] != "(none)" {
		t.Errorf("expected (none) row; got %+v", rows)
	}
}

// TestApplyResult_TableMixed — applied + skipped + errors render in
// stable order with status labels.
func TestApplyResult_TableMixed(t *testing.T) {
	r := &ApplyResult{
		Applied: []plan.Change{{Op: plan.OpUpdate, Path: "/spec/version/copyright"}},
		Skipped: []plan.Change{{Op: plan.OpUpdate, Path: "/spec/version/releaseType"}},
		Errors: []state.ChangeError{{
			Change: plan.Change{Op: plan.OpUpdate, Path: "/spec/iap/products/x/name"},
			Err:    errors.New("boom"),
		}},
	}
	_, rows := r.TableRows()
	if len(rows) != 3 {
		t.Fatalf("rows len %d, want 3", len(rows))
	}
	if rows[0][0] != "applied" || rows[1][0] != "skipped" || rows[2][0] != "error" {
		t.Errorf("status column drift: %+v", rows)
	}
}
