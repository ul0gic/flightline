package cmd

import (
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

func TestC3A_PlannedApplyRowsAreNotApplied(t *testing.T) {
	changes := []plan.Change{{Op: plan.OpCreate, Path: "/spec/pricing"}, {Op: plan.OpUpdate, Path: "/spec/version/copyright"}}
	rows := plannedApplyRows(changes)
	if len(rows) != 2 || rows[0][0] != "planned" || rows[0][1] != "create" || rows[0][2] != "/spec/pricing" || rows[1][0] != "planned" {
		t.Fatalf("rows=%v", rows)
	}
}
