package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestC2A_NewParentsEmitStableParentAndChildren(t *testing.T) {
	name := "Lifetime"
	changes := Diff(&config.State{Spec: config.StateSpec{
		IAP: &config.IAPSpec{Products: map[string]config.IAPProduct{
			"com.example.lifetime": {
				Type: "NON_CONSUMABLE", Name: &name,
				Localizations:    map[string]config.IAPLocalization{"en-US": {Name: &name}},
				ReviewScreenshot: &config.IAPReviewScreenshot{Path: "review.png"},
			},
		}},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"external": {Testers: []config.TestFlightTester{{Email: "b@example.com"}, {Email: "a@example.com"}}},
		}},
	}}, &config.State{})
	want := []string{
		"/spec/iap/products/com.example.lifetime",
		"/spec/iap/products/com.example.lifetime/localizations/en-US",
		"/spec/iap/products/com.example.lifetime/reviewScreenshot",
		"/spec/testflight/groups/external",
		"/spec/testflight/groups/external/testers/a@example.com",
		"/spec/testflight/groups/external/testers/b@example.com",
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want paths %v", changes, want)
	}
	for index, path := range want {
		if changes[index].Path != path {
			t.Fatalf("change paths = %+v, want %v", changes, want)
		}
	}
	iapParent, ok := changes[0].To.(config.IAPProduct)
	if !ok {
		t.Fatalf("IAP parent create type = %T, want config.IAPProduct", changes[0].To)
	}
	if iapParent.Localizations != nil || iapParent.ReviewScreenshot != nil {
		t.Fatalf("parent create contains children: %+v", iapParent)
	}
	groupParent, ok := changes[3].To.(config.TestFlightGroup)
	if !ok {
		t.Fatalf("group parent create type = %T, want config.TestFlightGroup", changes[3].To)
	}
	if groupParent.Testers != nil {
		t.Fatalf("group parent create contains testers: %+v", groupParent)
	}
	localization, ok := changes[1].To.(config.IAPLocalization)
	if !ok || localization.Name == nil {
		t.Fatalf("localization create is not a complete object: %#v", changes[1].To)
	}
}

func TestC2A_TestFlightEscapesGroupAndTesterPathSegments(t *testing.T) {
	group := "release/qa~canary"
	email := "tester/one~two@example.com"
	changes := Diff(&config.State{Spec: config.StateSpec{TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
		group: {Testers: []config.TestFlightTester{{Email: email}}},
	}}}}, &config.State{})
	want := []string{
		"/spec/testflight/groups/release~1qa~0canary",
		"/spec/testflight/groups/release~1qa~0canary/testers/tester~1one~0two@example.com",
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want paths %v", changes, want)
	}
	for index, path := range want {
		if changes[index].Path != path {
			t.Fatalf("change paths = %+v, want %v", changes, want)
		}
	}
}
