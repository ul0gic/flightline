package config

import "testing"

func TestF7A_PhasedReleaseIntentGuardsObservedAndTransitions(t *testing.T) {
	trueValue, active, paused, ready := true, "ACTIVE", "PAUSED", "READY_FOR_DISTRIBUTION"
	live := &State{ObservedVersionState: ready, Spec: StateSpec{Version: &VersionSpec{PhasedRelease: &PhasedReleaseSpec{Enabled: &trueValue, State: &active}}}}
	desired := &State{Spec: StateSpec{Version: &VersionSpec{PhasedRelease: &PhasedReleaseSpec{Enabled: &trueValue, State: &paused}}}}
	if got := ValidatePhasedReleaseIntent("state.yaml", desired, live); len(got) != 0 {
		t.Fatalf("pause diagnostics=%+v", got)
	}
	zero := 0
	desired.Spec.Version.PhasedRelease.CurrentDayNumber = &zero
	if got := ValidatePhasedReleaseIntent("state.yaml", desired, live); len(got) == 0 {
		t.Fatal("observed field change accepted")
	}
	desired.Spec.Version.PhasedRelease.CurrentDayNumber = nil
	falseValue := false
	desired.Spec.Version.PhasedRelease.Enabled = &falseValue
	if got := ValidatePhasedReleaseIntent("state.yaml", desired, live); len(got) == 0 {
		t.Fatal("disable accepted")
	}
}
