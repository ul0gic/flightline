package state

import (
	"errors"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestC3A_ValidateChangesCollectsErrorsWithoutDispatch(t *testing.T) {
	changes := []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/version/copyright", To: "2026 Example"},
		{Op: plan.OpUpdate, Path: "/spec/unknown/leaf", To: "x"},
		{Op: plan.OpUpdate, Path: "/spec/categories/notAField", To: "x"},
	}
	errs := ValidateChanges(changes)
	if len(errs) != 2 || errs[0].Change.Path != changes[1].Path || errs[1].Change.Path != changes[2].Path {
		t.Fatalf("errors=%+v", errs)
	}
	if !errors.Is(errs[0].Err, ErrUnmappedChange) || !errors.Is(errs[1].Err, ErrUnmappedChange) {
		t.Fatalf("wrong error types: %+v", errs)
	}
}

func TestC3A_ValidateChangesRejectsUnsupportedOperationsAndValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		ch   plan.Change
		want string
	}{
		{"delete scalar", plan.Change{Op: plan.OpDelete, Path: "/spec/version/copyright", To: "x"}, "operation"},
		{"age enum", plan.Change{Op: plan.OpUpdate, Path: "/spec/ageRating/prolongedGraphicSadisticRealisticViolence", To: "OFTEN"}, "frequency"},
		{"derived age", plan.Change{Op: plan.OpUpdate, Path: "/spec/ageRating/seventeenPlus", To: true}, "derived"},
		{"bad metadata leaf", plan.Change{Op: plan.OpUpdate, Path: "/spec/metadata/locales/en-US/unknown", To: "x"}, "unsupported"},
		{"iap immutable", plan.Change{Op: plan.OpUpdate, Path: "/spec/iap/products/p1/type", To: "CONSUMABLE"}, "not writable"},
		{"iap localization incomplete", plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/p1/localizations/en-US", To: config.IAPLocalization{}}, "complete name"},
		{"iap description long", plan.Change{Op: plan.OpUpdate, Path: "/spec/iap/products/p1/localizations/en-US/description", To: strings.Repeat("x", 46)}, "45-character"},
		{"iap hosting", plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/p1", To: config.IAPProduct{Type: "CONSUMABLE", Name: c3aString("p1"), ContentHosting: c3aString("host")}}, "read-only"},
		{"testflight escape", plan.Change{Op: plan.OpCreate, Path: "/spec/testflight/groups/a~2b", To: config.TestFlightGroup{IsInternal: c3aBool(false)}}, "segment"},
		{"testflight kind", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/groups/a/isInternal", To: true}, "immutable"},
		{"testflight tester op", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/groups/a/testers/a~1b@example.com", To: "x"}, "create or delete"},
		{"pricing incomplete", plan.Change{Op: plan.OpCreate, Path: "/spec/pricing", To: config.PricingSpec{BaseTerritory: c3aString("USA")}}, "complete"},
		{"export incomplete", plan.Change{Op: plan.OpCreate, Path: "/spec/exportCompliance/declaration", To: config.ExportComplianceDeclaration{}}, "required"},
		{"cpp leaf", plan.Change{Op: plan.OpUpdate, Path: "/spec/customProductPages/foo/localizations/en-US/unknown", To: "x"}, "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateChanges([]plan.Change{tc.ch})
			if len(errs) != 1 || !strings.Contains(errs[0].MessageText(), tc.want) {
				t.Fatalf("errors=%+v", errs)
			}
		})
	}
}

func TestC3A_ValidateChangesAcceptsSupportedShapes(t *testing.T) {
	changes := []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/ageRating/prolongedGraphicSadisticRealisticViolence", To: "FREQUENT"},
		{Op: plan.OpCreate, Path: "/spec/pricing", To: config.PricingSpec{BaseTerritory: c3aString("USA"), AppPricePointID: c3aString("PP1")}},
		{Op: plan.OpCreate, Path: "/spec/testflight/groups/A~1B", To: config.TestFlightGroup{IsInternal: c3aBool(false)}},
		{Op: plan.OpCreate, Path: "/spec/testflight/groups/A~1B/testers/a@example.com", To: "a@example.com"},
		{Op: plan.OpCreate, Path: "/spec/iap/products/p1/localizations/en-US", To: config.IAPLocalization{Name: c3aString("Example")}},
		{Op: plan.OpCreate, Path: "/spec/iap/products/p1/reviewScreenshot", To: &config.IAPReviewScreenshot{Path: "review.png"}},
		{Op: plan.OpUpdate, Path: "/spec/categories/primarySubcategories", To: []string{}},
	}
	if errs := ValidateChanges(changes); len(errs) != 0 {
		t.Fatalf("valid changes rejected: %+v", errs)
	}
}

func TestC3A_ValidateActualDiffShapes(t *testing.T) {
	pages := config.CustomProductPagesSpec{"page": {Visible: c3aBool(true), Localizations: map[string]config.CustomProductPageLocale{
		"en-US": {PromotionalText: c3aString("Try it"), Screenshots: map[string][]config.ScreenshotFile{"APP_IPHONE_67": {{Path: "page.png"}}}},
	}}}
	desired := &config.State{Spec: config.StateSpec{
		IAP: &config.IAPSpec{Products: map[string]config.IAPProduct{"p1": {
			Type: "CONSUMABLE", Name: c3aString("Product"), ReviewScreenshot: &config.IAPReviewScreenshot{Path: "review.png"},
			Localizations: map[string]config.IAPLocalization{"en-US": {Name: c3aString("Product")}},
		}}},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{"A/B": {
			IsInternal: c3aBool(false), Testers: []config.TestFlightTester{{Email: "a@example.com"}},
		}}},
		CustomProductPages: &pages,
		Categories:         &config.CategoriesSpec{PrimarySubcategories: []string{}},
		AgeRating:          &config.AgeRatingSpec{ProlongedGraphicSadisticRealisticViolence: c3aString("NONE")},
	}}
	live := &config.State{Spec: config.StateSpec{Categories: &config.CategoriesSpec{PrimarySubcategories: []string{"OLD"}}}}
	changes := plan.Diff(desired, live)
	if len(changes) < 7 {
		t.Fatalf("expected multiple real diff shapes, got %+v", changes)
	}
	if errs := ValidateChanges(changes); len(errs) != 0 {
		t.Fatalf("actual diff rejected: %+v", errs)
	}
}

func c3aString(s string) *string { return &s }
func c3aBool(b bool) *bool       { return &b }
