package config

import "testing"

func TestC2A_ValidateIAPIntent(t *testing.T) {
	observed := "Every film stock and feature forever. No subscriptions"
	changed := observed + "!"
	newName := "Lifetime"
	liveType := "NON_CONSUMABLE"
	consumable := "CONSUMABLE"
	hosted := "HOSTED"
	nonHosted := "NON_HOSTED"
	internal := true
	external := false
	live := &State{Spec: StateSpec{
		IAP: &IAPSpec{Products: map[string]IAPProduct{
			"com.example.lifetime": {Type: liveType, ContentHosting: &hosted, Localizations: map[string]IAPLocalization{
				"en-US": {Name: &newName, Description: &observed},
			}},
		}},
		TestFlight: &TestFlightSpec{Groups: map[string]TestFlightGroup{"existing": {IsInternal: &internal}}},
	}}
	desired := &State{Spec: StateSpec{
		IAP: &IAPSpec{Products: map[string]IAPProduct{
			"com.example.lifetime": {Type: consumable, ContentHosting: &nonHosted, Localizations: map[string]IAPLocalization{
				"en-US": {Name: &newName, Description: &changed},
			}},
			"com.example.new": {Type: liveType, ContentHosting: &hosted, Localizations: map[string]IAPLocalization{
				"en-US": {Description: &changed},
			}},
		}},
		TestFlight: &TestFlightSpec{Groups: map[string]TestFlightGroup{
			"existing": {IsInternal: &external}, "new": {},
		}},
	}}

	diags := ValidateIAPIntent("state.yaml", desired, live)
	assertC2A_DiagnosticPath(t, diags, "/spec/iap/products/com.example.lifetime/type")
	assertC2A_DiagnosticPath(t, diags, "/spec/iap/products/com.example.lifetime/contentHosting")
	assertC2A_DiagnosticPath(t, diags, "/spec/iap/products/com.example.lifetime/localizations/en-US/description")
	assertC2A_DiagnosticPath(t, diags, "/spec/iap/products/com.example.new/name")
	assertC2A_DiagnosticPath(t, diags, "/spec/iap/products/com.example.new/contentHosting")
	assertC2A_DiagnosticPath(t, diags, "/spec/iap/products/com.example.new/localizations/en-US/name")
	assertC2A_DiagnosticPath(t, diags, "/spec/testflight/groups/existing/isInternal")
	assertC2A_DiagnosticPath(t, diags, "/spec/testflight/groups/new/isInternal")
}

func TestC2A_ObservedLongDescriptionIsNoOp(t *testing.T) {
	observed := "Every film stock and feature forever. No subscriptions"
	name := "Lifetime"
	typeValue := "NON_CONSUMABLE"
	state := &State{Spec: StateSpec{IAP: &IAPSpec{Products: map[string]IAPProduct{
		"com.example.lifetime": {Type: typeValue, Localizations: map[string]IAPLocalization{
			"en-US": {Name: &name, Description: &observed},
		}},
	}}}}
	for _, diagnostic := range ValidateIAPIntent("state.yaml", state, state) {
		if diagnostic.Path == "/spec/iap/products/com.example.lifetime/localizations/en-US/description" {
			t.Fatalf("unchanged observed description rejected: %+v", diagnostic)
		}
	}
}

func assertC2A_DiagnosticPath(t *testing.T, diagnostics []Diagnostic, want string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Path == want {
			return
		}
	}
	t.Fatalf("missing diagnostic %q in %+v", want, diagnostics)
}
