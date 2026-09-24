package config

import (
	"strings"
	"testing"
)

func TestF5A_IAPCommerceIntentRequiresCompleteInitialValues(t *testing.T) {
	desired := &State{}
	desired.Spec.IAP = &IAPSpec{Products: map[string]IAPProduct{"com.example.unlock": {Commerce: &IAPCommerceSpec{
		Pricing:      &IAPPriceSpec{BaseTerritory: "USA"},
		Availability: &IAPAvailabilitySpec{AvailableInNewTerritories: new(false)},
	}}}}
	diagnostics := ValidateIAPCommerceIntent("state.yaml", desired, nil)
	if len(diagnostics) != 2 || !strings.Contains(diagnostics[0].Path+diagnostics[1].Path, "/commerce/pricing") || !strings.Contains(diagnostics[0].Path+diagnostics[1].Path, "/commerce/availability") {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
}

func TestF5A_IAPCommerceIntentPreservesOmittedAvailabilityFields(t *testing.T) {
	territories := []string{"USA", "CAN"}
	live := &State{}
	live.Spec.IAP = &IAPSpec{Products: map[string]IAPProduct{"com.example.unlock": {Commerce: &IAPCommerceSpec{Availability: &IAPAvailabilitySpec{
		AvailableInNewTerritories: new(true), AvailableTerritories: &territories,
	}}}}}
	desired := &State{}
	desired.Spec.IAP = &IAPSpec{Products: map[string]IAPProduct{"com.example.unlock": {Commerce: &IAPCommerceSpec{Availability: &IAPAvailabilitySpec{
		AvailableInNewTerritories: new(false),
	}}}}}
	if diagnostics := ValidateIAPCommerceIntent("state.yaml", desired, live); len(diagnostics) != 0 {
		t.Fatalf("partial desired diagnostics=%+v", diagnostics)
	}
	empty := []string{}
	desired.Spec.IAP.Products["com.example.unlock"] = IAPProduct{Commerce: &IAPCommerceSpec{Availability: &IAPAvailabilitySpec{AvailableTerritories: &empty}}}
	if diagnostics := ValidateIAPCommerceIntent("state.yaml", desired, live); len(diagnostics) != 0 {
		t.Fatalf("explicit empty set diagnostics=%+v", diagnostics)
	}
}
