package plan

import (
	"reflect"
	"slices"

	"github.com/ul0gic/flightline/internal/config"
)

func diffIAPCommerce(productID string, desired, live *config.IAPCommerceSpec, out *[]Change) {
	if desired == nil {
		return
	}
	base := "/spec/iap/products/" + productID + "/commerce/"
	if desired.Pricing != nil {
		var observed *config.IAPPriceSpec
		if live != nil {
			observed = live.Pricing
		}
		if observed == nil || *desired.Pricing != *observed {
			op := OpUpdate
			if observed == nil {
				op = OpCreate
			}
			*out = append(*out, Change{Op: op, Resource: "iap.commerce.pricing", Path: base + "pricing", From: observed, To: *desired.Pricing, Hint: "set IAP current base price for " + productID})
		}
	}
	if desired.Availability != nil {
		var observed *config.IAPAvailabilitySpec
		if live != nil {
			observed = live.Availability
		}
		merged := mergeIAPAvailability(desired.Availability, observed)
		if !equalIAPAvailability(merged, observed) {
			op := OpUpdate
			if observed == nil {
				op = OpCreate
			}
			*out = append(*out, Change{Op: op, Resource: "iap.commerce.availability", Path: base + "availability", From: observed, To: merged, Hint: "set IAP availability for " + productID})
		}
	}
}

func mergeIAPAvailability(desired, live *config.IAPAvailabilitySpec) config.IAPAvailabilitySpec {
	merged := *desired
	if live != nil {
		if merged.AvailableInNewTerritories == nil {
			merged.AvailableInNewTerritories = live.AvailableInNewTerritories
		}
		if merged.AvailableTerritories == nil {
			merged.AvailableTerritories = live.AvailableTerritories
		}
	}
	if merged.AvailableTerritories != nil {
		territories := slices.Clone(*merged.AvailableTerritories)
		slices.Sort(territories)
		merged.AvailableTerritories = &territories
	}
	return merged
}

func equalIAPAvailability(desired config.IAPAvailabilitySpec, live *config.IAPAvailabilitySpec) bool {
	if live == nil {
		return false
	}
	if !reflect.DeepEqual(desired.AvailableInNewTerritories, live.AvailableInNewTerritories) {
		return false
	}
	if desired.AvailableTerritories == nil || live.AvailableTerritories == nil {
		return desired.AvailableTerritories == nil && live.AvailableTerritories == nil
	}
	have := slices.Clone(*live.AvailableTerritories)
	slices.Sort(have)
	return slices.Equal(*desired.AvailableTerritories, have)
}
