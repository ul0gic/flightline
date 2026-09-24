package config

import "testing"

func TestF5B_ValidateAppAvailabilityIntentGuardsReadOnlyAndOrdinaryTerritories(t *testing.T) {
	trueValue, falseValue := true, false
	live := &State{Spec: StateSpec{AppAvailability: &AppAvailabilitySpec{
		AvailableInNewTerritories: &trueValue,
		Territories: map[string]TerritoryAvailabilitySpec{
			"USA": {Available: &trueValue, PreOrderEnabled: &falseValue, ContentStatuses: []string{"AVAILABLE"}},
			"GBR": {Available: &falseValue, PreOrderEnabled: &trueValue, ContentStatuses: []string{"AVAILABLE"}},
		},
	}}}
	date := "2027-02-03"
	desired := &State{Spec: StateSpec{AppAvailability: &AppAvailabilitySpec{
		AvailableInNewTerritories: &falseValue,
		Territories: map[string]TerritoryAvailabilitySpec{
			"USA": {Available: &falseValue},
			"GBR": {ReleaseDate: &date, PreOrderEnabled: &falseValue, ContentStatuses: []string{"MISSING_RATING"}},
			"JPN": {Available: &trueValue},
		},
	}}}
	diagnostics := ValidateAppAvailabilityIntent("state.yaml", desired, live)
	if len(diagnostics) != 5 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	paths := map[string]bool{}
	for _, diagnostic := range diagnostics {
		paths[diagnostic.Path] = true
	}
	for _, path := range []string{
		"/spec/appAvailability/availableInNewTerritories",
		"/spec/appAvailability/territories/USA",
		"/spec/appAvailability/territories/GBR/preOrderEnabled",
		"/spec/appAvailability/territories/GBR/contentStatuses",
		"/spec/appAvailability/territories/JPN",
	} {
		if !paths[path] {
			t.Errorf("missing diagnostic for %s: %#v", path, diagnostics)
		}
	}
}

func TestF5B_ValidateAppAvailabilityIntentAcceptsObservedPreOrderUpdateAndRejectsDateClear(t *testing.T) {
	trueValue, falseValue := true, false
	live := &State{Spec: StateSpec{AppAvailability: &AppAvailabilitySpec{Territories: map[string]TerritoryAvailabilitySpec{
		"GBR": {Available: &falseValue, PreOrderEnabled: &trueValue},
	}}}}
	date := "2027-02-03"
	accepted := &State{Spec: StateSpec{AppAvailability: &AppAvailabilitySpec{Territories: map[string]TerritoryAvailabilitySpec{
		"GBR": {Available: &trueValue, ReleaseDate: &date, PreOrderEnabled: &trueValue},
	}}}}
	if diagnostics := ValidateAppAvailabilityIntent("state.yaml", accepted, live); len(diagnostics) != 0 {
		t.Fatalf("pre-order update diagnostics = %#v", diagnostics)
	}
	empty := ""
	accepted.Spec.AppAvailability.Territories["GBR"] = TerritoryAvailabilitySpec{ReleaseDate: &empty}
	diagnostics := ValidateAppAvailabilityIntent("state.yaml", accepted, live)
	if len(diagnostics) != 1 || diagnostics[0].Path != "/spec/appAvailability/territories/GBR/releaseDate" {
		t.Fatalf("clear diagnostics = %#v", diagnostics)
	}
}
