package config

import (
	"fmt"
	"strings"
)

func ValidateBetaIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil || desired.Spec.TestFlight == nil {
		return nil
	}
	var diagnostics []Diagnostic
	metadata := desired.Spec.TestFlight.Metadata
	if metadata != nil {
		diagnostics = append(diagnostics, validateBetaMetadataIntent(file, metadata, live)...)
	}
	diagnostics = append(diagnostics, validateBetaGroupBuildsIntent(file, desired.Spec.TestFlight.Groups)...)
	return diagnostics
}

func validateBetaMetadataIntent(file string, metadata *BetaMetadataSpec, live *State) []Diagnostic {
	var diagnostics []Diagnostic
	if metadata.ReviewDetails != nil && betaReviewDetailsHasIntent(metadata.ReviewDetails) &&
		(live == nil || live.Spec.TestFlight == nil || live.Spec.TestFlight.Metadata == nil || live.Spec.TestFlight.Metadata.ReviewDetails == nil) {
		diagnostics = append(diagnostics, Diagnostic{File: file, Path: "/spec/testflight/metadata/reviewDetails", Severity: SeverityError,
			Message: "beta app review detail does not exist; Apple exposes PATCH but no create endpoint"})
	}
	seen := map[BetaBuildSelector]bool{}
	for index, build := range metadata.Builds {
		path := fmt.Sprintf("/spec/testflight/metadata/builds/%d/build", index)
		diagnostics = append(diagnostics, validateBetaSelector(file, path, build.Build)...)
		if seen[build.Build] {
			diagnostics = append(diagnostics, Diagnostic{File: file, Path: path, Severity: SeverityError, Message: "duplicate beta build selector"})
		}
		seen[build.Build] = true
	}
	return diagnostics
}

func validateBetaGroupBuildsIntent(file string, groups map[string]TestFlightGroup) []Diagnostic {
	var diagnostics []Diagnostic
	for name, group := range groups {
		if group.Builds == nil {
			continue
		}
		path := "/spec/testflight/groups/" + escapeIAPIntentPath(name) + "/builds"
		seen := map[BetaBuildSelector]bool{}
		for index, build := range *group.Builds {
			itemPath := fmt.Sprintf("%s/%d", path, index)
			diagnostics = append(diagnostics, validateBetaSelector(file, itemPath, build)...)
			if seen[build] {
				diagnostics = append(diagnostics, Diagnostic{File: file, Path: itemPath, Severity: SeverityError, Message: "duplicate beta group build selector"})
			}
			seen[build] = true
		}
	}
	return diagnostics
}

func validateBetaSelector(file, path string, selector BetaBuildSelector) []Diagnostic {
	if strings.TrimSpace(selector.Number) != "" && strings.TrimSpace(selector.Version) != "" && strings.TrimSpace(selector.Platform) != "" {
		return nil
	}
	return []Diagnostic{{File: file, Path: path, Severity: SeverityError, Message: "beta build selector requires number, version, and platform"}}
}

func betaReviewDetailsHasIntent(detail *BetaReviewDetailsSpec) bool {
	return detail.ContactFirstName != nil || detail.ContactLastName != nil || detail.ContactPhone != nil || detail.ContactEmail != nil ||
		detail.DemoAccountName != nil || detail.DemoAccountRequired != nil || detail.Notes != nil
}
