package plan

import (
	"github.com/ul0gic/flightline/internal/config"
	"testing"
)

func TestF7A_PhasedReleaseDiffIsAtomicAndMerged(t *testing.T) {
	trueValue, active, paused := true, "ACTIVE", "PAUSED"
	live := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{PhasedRelease: &config.PhasedReleaseSpec{Enabled: &trueValue, State: &active}}}}
	desired := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{PhasedRelease: &config.PhasedReleaseSpec{State: &paused}}}}
	var changes []Change
	diffPhasedRelease(desired, live, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/version/phasedRelease" || changes[0].Op != OpUpdate {
		t.Fatalf("changes=%+v", changes)
	}
	to, ok := changes[0].To.(config.PhasedReleaseSpec)
	if !ok || to.Enabled == nil || !*to.Enabled || to.State == nil || *to.State != paused {
		t.Fatalf("to=%+v", changes[0].To)
	}
}
