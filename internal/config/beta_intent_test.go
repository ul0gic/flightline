package config

import "testing"

func TestF5C_ValidateBetaIntentRejectsDuplicateAndIncompleteBuilds(t *testing.T) {
	selector := BetaBuildSelector{Number: "42", Version: "1.2", Platform: "IOS"}
	groupBuilds := []BetaBuildSelector{selector, selector, {Number: "43", Version: "", Platform: "IOS"}}
	desired := &State{Spec: StateSpec{TestFlight: &TestFlightSpec{
		Metadata: &BetaMetadataSpec{Builds: []BetaBuildMetadataSpec{{Build: selector}, {Build: selector}}},
		Groups:   map[string]TestFlightGroup{"Beta": {Builds: &groupBuilds}},
	}}}
	diagnostics := ValidateBetaIntent("state.yaml", desired, nil)
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
}

func TestF5C_ValidateBetaIntentRejectsUncreatableReviewDetails(t *testing.T) {
	email := "beta@example.com"
	desired := &State{Spec: StateSpec{TestFlight: &TestFlightSpec{Metadata: &BetaMetadataSpec{
		ReviewDetails: &BetaReviewDetailsSpec{ContactEmail: &email},
	}}}}
	if diagnostics := ValidateBetaIntent("state.yaml", desired, nil); len(diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	live := &State{Spec: StateSpec{TestFlight: &TestFlightSpec{Metadata: &BetaMetadataSpec{ReviewDetails: &BetaReviewDetailsSpec{}}}}}
	if diagnostics := ValidateBetaIntent("state.yaml", desired, live); len(diagnostics) != 0 {
		t.Fatalf("existing beta review detail rejected: %+v", diagnostics)
	}
}
