package plan

import "github.com/ul0gic/flightline/internal/config"

func diffPhasedRelease(desired, live *config.State, out *[]Change) {
	if desired == nil || desired.Spec.Version == nil || desired.Spec.Version.PhasedRelease == nil {
		return
	}
	want := desired.Spec.Version.PhasedRelease
	var current *config.PhasedReleaseSpec
	if live != nil && live.Spec.Version != nil {
		current = live.Spec.Version.PhasedRelease
	}
	if current == nil {
		if want.Enabled == nil || !*want.Enabled {
			return
		}
		*out = append(*out, Change{Op: OpCreate, Resource: "phasedRelease", Path: "/spec/version/phasedRelease", To: mergePhasedRelease(want, nil), Hint: "enable an inactive phased release"})
		return
	}
	if want.State == nil || (current.State != nil && *want.State == *current.State) {
		return
	}
	*out = append(*out, Change{Op: OpUpdate, Resource: "phasedRelease", Path: "/spec/version/phasedRelease", From: mergePhasedRelease(current, nil), To: mergePhasedRelease(want, current), Hint: "change phased release state"})
}

func mergePhasedRelease(want, current *config.PhasedReleaseSpec) config.PhasedReleaseSpec {
	merged := config.PhasedReleaseSpec{}
	if current != nil {
		merged.Enabled = clonePhasedBool(current.Enabled)
		merged.State = clonePhasedString(current.State)
		merged.StartDate = clonePhasedString(current.StartDate)
		merged.TotalPauseDuration = clonePhasedInt(current.TotalPauseDuration)
		merged.CurrentDayNumber = clonePhasedInt(current.CurrentDayNumber)
	}
	if want == nil {
		return merged
	}
	if want.Enabled != nil {
		merged.Enabled = clonePhasedBool(want.Enabled)
	}
	if want.State != nil {
		merged.State = clonePhasedString(want.State)
	}
	if want.StartDate != nil {
		merged.StartDate = clonePhasedString(want.StartDate)
	}
	if want.TotalPauseDuration != nil {
		merged.TotalPauseDuration = clonePhasedInt(want.TotalPauseDuration)
	}
	if want.CurrentDayNumber != nil {
		merged.CurrentDayNumber = clonePhasedInt(want.CurrentDayNumber)
	}
	return merged
}

func clonePhasedBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
func clonePhasedString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
func clonePhasedInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
