package config

import "sort"

// ValidateWriteIntent compares user intent with a complete live snapshot. Read
// schema validation stays separate so observed values can round-trip unchanged.
func ValidateWriteIntent(file string, desired, live *State) []Diagnostic {
	diagnostics := ValidatePhasedReleaseIntent(file, desired, live)
	diagnostics = append(diagnostics, ValidateIAPIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidateIAPCommerceIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidateAppAvailabilityIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidateBetaIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidateAccessibilityIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidatePricingIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidateExportIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidatePreviewsIntent(file, desired, live)...)
	diagnostics = append(diagnostics, ValidateRightsIntent(file, desired, live)...)
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		return diagnostics[i].Message < diagnostics[j].Message
	})
	return diagnostics
}
