package config

import "fmt"

func ValidateIAPCommerceIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil || desired.Spec.IAP == nil {
		return nil
	}
	var diagnostics []Diagnostic
	for productID, product := range desired.Spec.IAP.Products {
		if product.Commerce == nil {
			continue
		}
		var observed *IAPCommerceSpec
		if live != nil && live.Spec.IAP != nil {
			if current, ok := live.Spec.IAP.Products[productID]; ok {
				observed = current.Commerce
			}
		}
		diagnostics = append(diagnostics, validateIAPProductCommerce(file, productID, product.Commerce, observed)...)
	}
	return diagnostics
}

func validateIAPProductCommerce(file, productID string, desired, live *IAPCommerceSpec) []Diagnostic {
	base := "/spec/iap/products/" + productID + "/commerce"
	var diagnostics []Diagnostic
	if desired.Pricing != nil && (desired.Pricing.BaseTerritory == "" || desired.Pricing.PricePointID == "") {
		diagnostics = append(diagnostics, iapCommerceDiagnostic(file, base+"/pricing", "pricing requires both baseTerritory and pricePointId"))
	}
	if desired.Availability != nil {
		var observed *IAPAvailabilitySpec
		if live != nil {
			observed = live.Availability
		}
		diagnostics = append(diagnostics, validateIAPAvailabilityIntent(file, base, desired.Availability, observed)...)
	}
	return diagnostics
}

func validateIAPAvailabilityIntent(file, base string, availability, observed *IAPAvailabilitySpec) []Diagnostic {
	var diagnostics []Diagnostic
	missingBool := availability.AvailableInNewTerritories == nil && (observed == nil || observed.AvailableInNewTerritories == nil)
	missingTerritories := availability.AvailableTerritories == nil && (observed == nil || observed.AvailableTerritories == nil)
	if missingBool || missingTerritories {
		diagnostics = append(diagnostics, iapCommerceDiagnostic(file, base+"/availability", "availability requires availableInNewTerritories and availableTerritories, explicitly or from verified live state"))
	}
	if availability.AvailableTerritories == nil {
		return diagnostics
	}
	seen := make(map[string]bool)
	for _, territory := range *availability.AvailableTerritories {
		if territory == "" || seen[territory] {
			diagnostics = append(diagnostics, iapCommerceDiagnostic(file, base+"/availability/availableTerritories", fmt.Sprintf("invalid or duplicate territory %q", territory)))
		}
		seen[territory] = true
	}
	return diagnostics
}

func iapCommerceDiagnostic(file, path, message string) Diagnostic {
	return Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message}
}
