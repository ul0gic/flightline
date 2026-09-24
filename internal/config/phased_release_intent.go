package config

// ValidatePhasedReleaseIntent rejects rollout mutations outside the frozen transition contract.
func ValidatePhasedReleaseIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil || desired.Spec.Version == nil || desired.Spec.Version.PhasedRelease == nil {
		return nil
	}
	want := desired.Spec.Version.PhasedRelease
	var current *PhasedReleaseSpec
	if live != nil && live.Spec.Version != nil {
		current = live.Spec.Version.PhasedRelease
	}
	diagnostics := make([]Diagnostic, 0)
	if want.Enabled != nil && !*want.Enabled {
		diagnostics = append(diagnostics, phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/enabled", "disabling or deleting a phased release is unsupported"))
	}
	if current == nil {
		return append(diagnostics, validateNewPhasedReleaseIntent(file, want, observedVersionState(live))...)
	}
	diagnostics = append(diagnostics, validatePhasedReleaseObservedFields(file, want, current)...)
	diagnostics = append(diagnostics, validatePhasedReleaseStateIntent(file, want.State, current.State, observedVersionState(live))...)
	return diagnostics
}

func validateNewPhasedReleaseIntent(file string, want *PhasedReleaseSpec, versionState string) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	if want.Enabled == nil || !*want.Enabled {
		diagnostics = append(diagnostics, phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/enabled", "enabling a new phased release requires enabled: true"))
	}
	if want.State != nil {
		diagnostics = append(diagnostics, phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/state", "new phased releases are created INACTIVE; omit state"))
	}
	if !phasedReleaseEnableVersionState(versionState) {
		diagnostics = append(diagnostics, phasedReleaseDiagnostic(file, "/spec/version/phasedRelease", "current version state is not eligible to enable a phased release"))
	}
	if want.StartDate != nil || want.TotalPauseDuration != nil || want.CurrentDayNumber != nil {
		diagnostics = append(diagnostics, phasedReleaseDiagnostic(file, "/spec/version/phasedRelease", "startDate, totalPauseDuration, and currentDayNumber are observed after creation and cannot be authored"))
	}
	return diagnostics
}

func validatePhasedReleaseObservedFields(file string, want, current *PhasedReleaseSpec) []Diagnostic {
	checks := []struct {
		path  string
		match bool
	}{
		{"startDate", equalPhasedString(want.StartDate, current.StartDate)},
		{"totalPauseDuration", equalPhasedInt(want.TotalPauseDuration, current.TotalPauseDuration)},
		{"currentDayNumber", equalPhasedInt(want.CurrentDayNumber, current.CurrentDayNumber)},
	}
	diagnostics := make([]Diagnostic, 0)
	for _, check := range checks {
		if !check.match && phasedFieldPresent(want, check.path) {
			diagnostics = append(diagnostics, phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/"+check.path, check.path+" is observed and cannot be changed"))
		}
	}
	return diagnostics
}

func validatePhasedReleaseStateIntent(file string, want, current *string, versionState string) []Diagnostic {
	if want == nil || equalPhasedString(want, current) {
		return nil
	}
	if *want != "ACTIVE" && *want != "PAUSED" {
		return []Diagnostic{phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/state", "only ACTIVE and PAUSED may be changed; INACTIVE and COMPLETE are observed-only")}
	}
	if current == nil || (*current != "ACTIVE" && *current != "PAUSED") || *want == *current {
		return []Diagnostic{phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/state", "state may change only between observed ACTIVE and PAUSED")}
	}
	if versionState != "READY_FOR_DISTRIBUTION" {
		return []Diagnostic{phasedReleaseDiagnostic(file, "/spec/version/phasedRelease/state", "pause or resume requires observed appVersionState READY_FOR_DISTRIBUTION")}
	}
	return nil
}

func observedVersionState(live *State) string {
	if live == nil {
		return ""
	}
	return live.ObservedVersionState
}

func phasedReleaseEnableVersionState(state string) bool {
	switch state {
	case "PREPARE_FOR_SUBMISSION", "WAITING_FOR_REVIEW", "IN_REVIEW", "WAITING_FOR_EXPORT_COMPLIANCE", "PENDING_DEVELOPER_RELEASE", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED":
		return true
	default:
		return false
	}
}

func equalPhasedString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalPhasedInt(left, right *int) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func phasedFieldPresent(spec *PhasedReleaseSpec, field string) bool {
	if spec == nil {
		return false
	}
	switch field {
	case "startDate":
		return spec.StartDate != nil
	case "totalPauseDuration":
		return spec.TotalPauseDuration != nil
	case "currentDayNumber":
		return spec.CurrentDayNumber != nil
	default:
		return false
	}
}

func phasedReleaseDiagnostic(file, path, message string) Diagnostic {
	return Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message}
}
