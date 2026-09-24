package config

// CompletePricingPair resolves omitted desired fields from an observed live
// pair. A fresh unpriced app needs both fields in desired state.
func CompletePricingPair(desired, live *PricingSpec) (PricingSpec, bool) {
	var pair PricingSpec
	if desired == nil {
		return pair, false
	}
	pair = *desired
	if live != nil {
		if pair.BaseTerritory == nil {
			pair.BaseTerritory = live.BaseTerritory
		}
		if pair.AppPricePointID == nil {
			pair.AppPricePointID = live.AppPricePointID
		}
	}
	return pair, pair.BaseTerritory != nil && *pair.BaseTerritory != "" && pair.AppPricePointID != nil && *pair.AppPricePointID != ""
}

// ValidatePricingIntent rejects incomplete local intent before a write can
// create an app price schedule. Server-side point/territory checks still run at apply.
func ValidatePricingIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil || desired.Spec.Pricing == nil {
		return nil
	}
	var livePricing *PricingSpec
	if live != nil {
		livePricing = live.Spec.Pricing
	}
	if livePricing != nil && livePricing.BaseTerritory != nil && desired.Spec.Pricing.BaseTerritory != nil &&
		*desired.Spec.Pricing.BaseTerritory != *livePricing.BaseTerritory && desired.Spec.Pricing.AppPricePointID == nil {
		return []Diagnostic{{
			File: file, Path: "/spec/pricing/appPricePointId", Severity: SeverityError,
			Message: "a baseTerritory change requires an explicit appPricePointId for the new territory",
		}}
	}
	_, complete := CompletePricingPair(desired.Spec.Pricing, livePricing)
	if complete || (desired.Spec.Pricing.BaseTerritory == nil && desired.Spec.Pricing.AppPricePointID == nil) {
		return nil
	}
	return []Diagnostic{{
		File: file, Path: "/spec/pricing", Severity: SeverityError,
		Message: "baseTerritory and appPricePointId are both required when no verified live counterpart exists",
	}}
}
