package state

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG7_DistributedVersionPlanAllowsOnlyPhasedRelease(t *testing.T) {
	live := &config.State{ObservedVersionState: "READY_FOR_DISTRIBUTION"}
	phase := plan.Change{Op: plan.OpUpdate, Path: "/spec/version/phasedRelease"}
	metadata := plan.Change{Op: plan.OpUpdate, Path: "/spec/version/copyright", To: "changed"}
	if got := ValidateVersionChanges(live, []plan.Change{phase}); len(got) != 0 {
		t.Fatalf("phase rejected: %+v", got)
	}
	got := ValidateVersionChanges(live, []plan.Change{metadata, phase})
	if len(got) != 1 || got[0].Change.Path != metadata.Path {
		t.Fatalf("mixed guard=%+v", got)
	}
	live.ObservedVersionState = "PREPARE_FOR_SUBMISSION"
	if got := ValidateVersionChanges(live, []plan.Change{metadata, phase}); len(got) != 0 {
		t.Fatalf("editable version rejected: %+v", got)
	}
}
