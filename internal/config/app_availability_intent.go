package config

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// ValidateAppAvailabilityIntent rejects writes outside Apple's documented pre-order scope.
func ValidateAppAvailabilityIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil || desired.Spec.AppAvailability == nil {
		return nil
	}
	want := desired.Spec.AppAvailability
	var observed *AppAvailabilitySpec
	if live != nil {
		observed = live.Spec.AppAvailability
	}
	diagnostics := make([]Diagnostic, 0)
	if want.AvailableInNewTerritories != nil && (observed == nil || !equalBoolPointers(want.AvailableInNewTerritories, observed.AvailableInNewTerritories)) {
		diagnostics = append(diagnostics, appAvailabilityDiagnostic(file, "/spec/appAvailability/availableInNewTerritories", "availableInNewTerritories is observed and cannot be changed"))
	}
	keys := make([]string, 0, len(want.Territories))
	for territoryID := range want.Territories {
		keys = append(keys, territoryID)
	}
	sort.Strings(keys)
	for _, territoryID := range keys {
		var current TerritoryAvailabilitySpec
		found := observed != nil && observed.Territories != nil
		if found {
			current, found = observed.Territories[territoryID]
		}
		diagnostics = append(diagnostics, validateAppAvailabilityTerritory(file, territoryID, want.Territories[territoryID], current, found)...)
	}
	return diagnostics
}

func validateAppAvailabilityTerritory(file, territoryID string, want, current TerritoryAvailabilitySpec, found bool) []Diagnostic {
	base := "/spec/appAvailability/territories/" + territoryID
	if strings.TrimSpace(territoryID) == "" {
		return []Diagnostic{appAvailabilityDiagnostic(file, base, "territory ID is required")}
	}
	diagnostics := make([]Diagnostic, 0)
	if want.ReleaseDate != nil && !validAppAvailabilityDate(*want.ReleaseDate) {
		diagnostics = append(diagnostics, appAvailabilityDiagnostic(file, base+"/releaseDate", "releaseDate must be a nonempty YYYY-MM-DD date; clearing with null is not supported"))
	}
	if !found {
		return append(diagnostics, appAvailabilityDiagnostic(file, base, "territory is not observed; creation is unsupported"))
	}
	diagnostics = append(diagnostics, validateObservedAppAvailabilityFields(file, base, want, current)...)
	diagnostics = append(diagnostics, validateAppAvailabilityWritableFields(file, base, want, current)...)
	return diagnostics
}

func validateObservedAppAvailabilityFields(file, base string, want, current TerritoryAvailabilitySpec) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	if want.PreOrderEnabled != nil && !equalBoolPointers(want.PreOrderEnabled, current.PreOrderEnabled) {
		diagnostics = append(diagnostics, appAvailabilityDiagnostic(file, base+"/preOrderEnabled", "preOrderEnabled requires an explicit preorder action and cannot be reconciled"))
	}
	if want.PreOrderPublishDate != nil && !equalStringPointers(want.PreOrderPublishDate, current.PreOrderPublishDate) {
		diagnostics = append(diagnostics, appAvailabilityDiagnostic(file, base+"/preOrderPublishDate", "preOrderPublishDate is observed and cannot be changed"))
	}
	if want.ContentStatuses != nil && !equalContentStatuses(want.ContentStatuses, current.ContentStatuses) {
		diagnostics = append(diagnostics, appAvailabilityDiagnostic(file, base+"/contentStatuses", "contentStatuses are observed and cannot be changed"))
	}
	return diagnostics
}

func validateAppAvailabilityWritableFields(file, base string, want, current TerritoryAvailabilitySpec) []Diagnostic {
	if (want.Available != nil && !equalBoolPointers(want.Available, current.Available)) || (want.ReleaseDate != nil && !equalStringPointers(want.ReleaseDate, current.ReleaseDate)) {
		if current.PreOrderEnabled == nil || !*current.PreOrderEnabled {
			return []Diagnostic{appAvailabilityDiagnostic(file, base, "available and releaseDate writes are qualified only for an observed active pre-order territory; ordinary availability changes are unsupported")}
		}
	}
	return nil
}

func appAvailabilityDiagnostic(file, path, message string) Diagnostic {
	return Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message}
}

func validAppAvailabilityDate(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func equalBoolPointers(a, b *bool) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func equalStringPointers(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func equalContentStatuses(a, b []string) bool {
	return (len(a) == 0 && len(b) == 0) || slices.Equal(a, b)
}
