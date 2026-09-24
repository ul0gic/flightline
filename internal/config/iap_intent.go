package config

import (
	"sort"
	"strings"
	"unicode/utf8"
)

const iapDescriptionWriteLimit = 45

// ValidateIAPIntent rejects desired IAP/TestFlight changes that the public ASC
// write contract cannot represent. It deliberately permits read-only observed
// values when the desired state leaves them unchanged.
func ValidateIAPIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil {
		return nil
	}
	var diagnostics []Diagnostic
	validateIAPProducts(file, desired.Spec.IAP, liveIAP(live), &diagnostics)
	validateTestFlightGroups(file, desired.Spec.TestFlight, liveTestFlight(live), &diagnostics)
	return diagnostics
}

func validateIAPProducts(file string, desired, live *IAPSpec, out *[]Diagnostic) {
	if desired == nil {
		return
	}
	liveProducts := map[string]IAPProduct{}
	if live != nil {
		liveProducts = live.Products
	}
	for _, productID := range sortedIAPIntentKeys(desired.Products) {
		desiredProduct := desired.Products[productID]
		liveProduct, exists := liveProducts[productID]
		base := "/spec/iap/products/" + escapeIAPIntentPath(productID)
		if exists {
			validateExistingIAP(file, base, desiredProduct, liveProduct, out)
		} else {
			validateNewIAP(file, base, desiredProduct, out)
		}
		validateIAPLocalizations(file, base, desiredProduct, liveProduct, exists, out)
	}
}

func validateExistingIAP(file, base string, desired, live IAPProduct, out *[]Diagnostic) {
	if desired.Type != live.Type {
		appendIAPIntentDiagnostic(out, file, base+"/type", "inAppPurchaseType is immutable after creation")
	}
	if desired.ContentHosting != nil && (live.ContentHosting == nil || *desired.ContentHosting != *live.ContentHosting) {
		appendIAPIntentDiagnostic(out, file, base+"/contentHosting", "contentHosting is read-only and cannot be changed")
	}
}

func validateNewIAP(file, base string, desired IAPProduct, out *[]Diagnostic) {
	if desired.Name == nil {
		appendIAPIntentDiagnostic(out, file, base+"/name", "name is required when creating an in-app purchase")
	}
	if desired.ContentHosting != nil {
		appendIAPIntentDiagnostic(out, file, base+"/contentHosting", "contentHosting is read-only and cannot be set when creating an in-app purchase")
	}
}

func validateIAPLocalizations(file, base string, desired, live IAPProduct, productExists bool, out *[]Diagnostic) {
	for _, locale := range sortedIAPIntentKeys(desired.Localizations) {
		desiredLocalization := desired.Localizations[locale]
		liveLocalization, exists := live.Localizations[locale]
		path := base + "/localizations/" + escapeIAPIntentPath(locale)
		if !productExists || !exists {
			if desiredLocalization.Name == nil {
				appendIAPIntentDiagnostic(out, file, path+"/name", "name is required when creating an in-app purchase localization")
			}
		}
		if descriptionWillWrite(desiredLocalization.Description, liveLocalization.Description, productExists && exists) && utf8.RuneCountInString(*desiredLocalization.Description) > iapDescriptionWriteLimit {
			appendIAPIntentDiagnostic(out, file, path+"/description", "description exceeds Apple's 45-character write limit")
		}
	}
}

func descriptionWillWrite(desired, live *string, exists bool) bool {
	if desired == nil {
		return false
	}
	return !exists || live == nil || *desired != *live
}

func validateTestFlightGroups(file string, desired, live *TestFlightSpec, out *[]Diagnostic) {
	if desired == nil {
		return
	}
	liveGroups := map[string]TestFlightGroup{}
	if live != nil {
		liveGroups = live.Groups
	}
	for _, groupName := range sortedIAPIntentKeys(desired.Groups) {
		desiredGroup := desired.Groups[groupName]
		liveGroup, exists := liveGroups[groupName]
		path := "/spec/testflight/groups/" + escapeIAPIntentPath(groupName) + "/isInternal"
		if !exists && desiredGroup.IsInternal == nil {
			appendIAPIntentDiagnostic(out, file, path, "isInternal is required when creating a TestFlight group")
		}
		if exists && desiredGroup.IsInternal != nil && (liveGroup.IsInternal == nil || *desiredGroup.IsInternal != *liveGroup.IsInternal) {
			appendIAPIntentDiagnostic(out, file, path, "isInternal is immutable after group creation")
		}
	}
}

func liveIAP(state *State) *IAPSpec {
	if state == nil {
		return nil
	}
	return state.Spec.IAP
}

func liveTestFlight(state *State) *TestFlightSpec {
	if state == nil {
		return nil
	}
	return state.Spec.TestFlight
}

func appendIAPIntentDiagnostic(out *[]Diagnostic, file, path, message string) {
	*out = append(*out, Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message})
}

func sortedIAPIntentKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapeIAPIntentPath(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
