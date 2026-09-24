package config

import "reflect"

type AccessibilityAnswerChange struct {
	Field string
	From  *bool
	To    *bool
}

func AccessibilityAnswerChanges(desired, live AccessibilityDeclarationSpec) []AccessibilityAnswerChange {
	pairs := []AccessibilityAnswerChange{
		{"supportsAudioDescriptions", live.SupportsAudioDescriptions, desired.SupportsAudioDescriptions},
		{"supportsCaptions", live.SupportsCaptions, desired.SupportsCaptions},
		{"supportsDarkInterface", live.SupportsDarkInterface, desired.SupportsDarkInterface},
		{"supportsDifferentiateWithoutColorAlone", live.SupportsDifferentiateWithoutColorAlone, desired.SupportsDifferentiateWithoutColorAlone},
		{"supportsLargerText", live.SupportsLargerText, desired.SupportsLargerText},
		{"supportsReducedMotion", live.SupportsReducedMotion, desired.SupportsReducedMotion},
		{"supportsSufficientContrast", live.SupportsSufficientContrast, desired.SupportsSufficientContrast},
		{"supportsVoiceControl", live.SupportsVoiceControl, desired.SupportsVoiceControl},
		{"supportsVoiceover", live.SupportsVoiceover, desired.SupportsVoiceover},
	}
	changes := make([]AccessibilityAnswerChange, 0, len(pairs))
	for _, pair := range pairs {
		if pair.To != nil && !reflect.DeepEqual(pair.From, pair.To) {
			changes = append(changes, pair)
		}
	}
	return changes
}

func AccessibilityHasAnswers(spec AccessibilityDeclarationSpec) bool {
	return len(AccessibilityAnswerChanges(spec, AccessibilityDeclarationSpec{})) > 0
}

func ValidateAccessibilityIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil || desired.Spec.AccessibilityDeclarations == nil {
		return nil
	}
	var liveFamilies map[string]AccessibilityDeclarationSpec
	if live != nil && live.Spec.AccessibilityDeclarations != nil {
		liveFamilies = live.Spec.AccessibilityDeclarations.Families
	}
	var diagnostics []Diagnostic
	for family, declaration := range desired.Spec.AccessibilityDeclarations.Families {
		diagnostics = append(diagnostics, validateAccessibilityFamily(file, family, declaration, liveFamilies)...)
	}
	return diagnostics
}

func validateAccessibilityFamily(file, family string, declaration AccessibilityDeclarationSpec, liveFamilies map[string]AccessibilityDeclarationSpec) []Diagnostic {
	base := "/spec/accessibilityDeclarations/families/" + family
	if !validAccessibilityDeviceFamily(family) {
		return []Diagnostic{accessibilityDiagnostic(file, base, "unsupported device family")}
	}
	observed, exists := liveFamilies[family]
	var diagnostics []Diagnostic
	if declaration.State != nil && (!exists || observed.State == nil || *declaration.State != *observed.State) {
		diagnostics = append(diagnostics, accessibilityDiagnostic(file, base+"/state", "state is observed and cannot be changed"))
	}
	if !exists && !AccessibilityHasAnswers(declaration) {
		diagnostics = append(diagnostics, accessibilityDiagnostic(file, base, "new declaration requires at least one explicit support answer"))
	}
	if exists && observed.State != nil && *observed.State == "PUBLISHED" && len(AccessibilityAnswerChanges(declaration, observed)) > 0 {
		diagnostics = append(diagnostics, accessibilityDiagnostic(file, base, "published declaration cannot be edited; create a new draft explicitly with the CLI"))
	}
	return diagnostics
}

func accessibilityDiagnostic(file, path, message string) Diagnostic {
	return Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message}
}

func validAccessibilityDeviceFamily(family string) bool {
	switch family {
	case "IPHONE", "IPAD", "APPLE_TV", "APPLE_WATCH", "MAC", "VISION":
		return true
	default:
		return false
	}
}
