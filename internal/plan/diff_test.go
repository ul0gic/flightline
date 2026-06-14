package plan

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func intPtr(i int) *int { return &i }

// TestDiff_IdenticalIsZero: Diff(s, s) is empty.
func TestDiff_IdenticalIsZero(t *testing.T) {
	s := &config.State{
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: strPtr("© 2026")},
			AgeRating: &config.AgeRatingSpec{
				Gambling: boolPtr(false),
			},
		},
	}
	if got := Diff(s, s); len(got) != 0 {
		t.Errorf("identical states produced %d changes: %+v", len(got), got)
	}
}

// TestDiff_NilDesiredFieldNotManaged: desired=nil for a sub-spec means
// "leave it alone"; the diff must produce zero changes.
func TestDiff_NilDesiredFieldNotManaged(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{}}
	live := &config.State{
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: strPtr("© 2025")},
		},
	}
	got := Diff(desired, live)
	if len(got) != 0 {
		t.Errorf("expected nil-desired = no changes; got %+v", got)
	}
}

// TestDiff_VersionCopyrightUpdate: single leaf-level change.
func TestDiff_VersionCopyrightUpdate(t *testing.T) {
	desired := &config.State{
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: strPtr("© 2026")},
		},
	}
	live := &config.State{
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: strPtr("© 2025")},
		},
	}
	got := Diff(desired, live)
	if len(got) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.Op != OpUpdate {
		t.Errorf("op = %q, want update", c.Op)
	}
	if c.Path != "/spec/version/copyright" {
		t.Errorf("path = %q", c.Path)
	}
	if c.From != "© 2025" || c.To != "© 2026" {
		t.Errorf("from/to: %v -> %v", c.From, c.To)
	}
}

// TestDiff_VersionCopyrightCreate: live has no value, desired sets one.
func TestDiff_VersionCopyrightCreate(t *testing.T) {
	desired := &config.State{
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: strPtr("© 2026")},
		},
	}
	live := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{}}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one create; got %+v", got)
	}
}

// TestDiff_StableOrdering: Path-sorted output is deterministic.
func TestDiff_StableOrdering(t *testing.T) {
	desired := &config.State{
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: strPtr("© 2026"), ReleaseType: strPtr("MANUAL")},
			Categories: &config.CategoriesSpec{
				Primary: strPtr("EDUCATION"),
			},
		},
	}
	got := Diff(desired, &config.State{})
	paths := make([]string, len(got))
	for i, c := range got {
		paths[i] = c.Path
	}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(paths, sorted) {
		t.Errorf("paths not sorted:\n got: %v\nwant: %v", paths, sorted)
	}
}

// TestDiff_NilLive: first apply against an unmanaged app: every
// desired field becomes a create.
func TestDiff_NilLive(t *testing.T) {
	desired := &config.State{
		Spec: config.StateSpec{
			Pricing: &config.PricingSpec{
				BaseTerritory:   strPtr("USA"),
				AppPricePointID: strPtr("FREE"),
			},
		},
	}
	got := Diff(desired, nil)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Op != OpCreate {
			t.Errorf("expected all creates; got %s on %s", c.Op, c.Path)
		}
	}
}

// TestDiff_IAPCreate: net-new IAP product becomes a create entry.
func TestDiff_IAPCreate(t *testing.T) {
	desired := &config.State{
		Spec: config.StateSpec{
			IAP: &config.IAPSpec{
				Products: map[string]config.IAPProduct{
					"com.app.lifetime": {Type: "NON_CONSUMABLE", Name: strPtr("Lifetime")},
				},
			},
		},
	}
	got := Diff(desired, &config.State{Spec: config.StateSpec{IAP: &config.IAPSpec{}}})
	if len(got) != 1 || got[0].Op != OpCreate || got[0].Path != "/spec/iap/products/com.app.lifetime" {
		t.Fatalf("expected one create on the IAP path: %+v", got)
	}
}

// TestDiff_TestFlightTesterAddRemove: set diff on tester roster.
func TestDiff_TestFlightTesterAddRemove(t *testing.T) {
	desired := &config.State{
		Spec: config.StateSpec{
			TestFlight: &config.TestFlightSpec{
				Groups: map[string]config.TestFlightGroup{
					"family": {Testers: []config.TestFlightTester{{Email: "new@x.com"}}},
				},
			},
		},
	}
	live := &config.State{
		Spec: config.StateSpec{
			TestFlight: &config.TestFlightSpec{
				Groups: map[string]config.TestFlightGroup{
					"family": {Testers: []config.TestFlightTester{{Email: "old@x.com"}}},
				},
			},
		},
	}
	got := Diff(desired, live)
	var creates, deletes int
	for _, c := range got {
		switch c.Op {
		case OpCreate:
			creates++
		case OpDelete:
			deletes++
		}
	}
	if creates != 1 || deletes != 1 {
		t.Errorf("expected 1 create + 1 delete; got %+v", got)
	}
}

// TestEmptyToNil covers the emptyToNil helper at 0% coverage.
func TestEmptyToNil(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{name: "empty string returns nil", input: "", want: nil},
		{name: "non-empty returns string", input: "1.2.3", want: "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emptyToNil(tt.input)
			if got != tt.want {
				t.Errorf("emptyToNil(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestDiff_NilDesired: nil desired returns empty slice, not nil.
func TestDiff_NilDesired(t *testing.T) {
	live := &config.State{Spec: config.StateSpec{
		Version: &config.VersionSpec{Copyright: strPtr("© 2025")},
	}}
	got := Diff(nil, live)
	if len(got) != 0 {
		t.Errorf("nil desired: got %d changes, want 0", len(got))
	}
}

// TestDiff_BuildCreate: desired build number with empty live becomes OpCreate.
func TestDiff_BuildCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Build: &config.BuildSpec{Number: "42"},
	}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.Op != OpCreate {
		t.Errorf("op = %q, want create", c.Op)
	}
	if c.From != nil {
		t.Errorf("From = %v, want nil", c.From)
	}
	if c.To != "42" {
		t.Errorf("To = %v, want 42", c.To)
	}
}

// TestDiff_BuildUpdate: desired build number differs from live: OpUpdate.
func TestDiff_BuildUpdate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Build: &config.BuildSpec{Number: "43"},
	}}
	live := &config.State{Spec: config.StateSpec{
		Build: &config.BuildSpec{Number: "42"},
	}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpUpdate {
		t.Fatalf("expected one update; got %+v", got)
	}
}

// TestDiff_BuildNilDesired: nil Build spec produces no changes.
func TestDiff_BuildNilDesired(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{}}
	live := &config.State{Spec: config.StateSpec{
		Build: &config.BuildSpec{Number: "42"},
	}}
	got := Diff(desired, live)
	if len(got) != 0 {
		t.Errorf("nil Build desired: got %d changes, want 0: %+v", len(got), got)
	}
}

// TestDiff_MetadataCreate: new locale in desired, absent in live.
func TestDiff_MetadataCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Metadata: &config.MetadataSpec{
			Locales: map[string]config.MetadataLocale{
				"en-US": {Name: strPtr("My App")},
			},
		},
	}}
	got := Diff(desired, &config.State{})
	if len(got) == 0 {
		t.Fatal("expected changes for new locale, got none")
	}
	for _, c := range got {
		if c.Op != OpCreate {
			t.Errorf("expected all creates; got %s on %s", c.Op, c.Path)
		}
	}
}

// TestDiff_ScreenshotsCreate: desired screenshots with empty live.
func TestDiff_ScreenshotsCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Screenshots: &config.ScreenshotsSpec{
			Locales: map[string]map[string][]config.ScreenshotFile{
				"en-US": {
					"APP_IPHONE_67": {{Path: "screens/home.png"}},
				},
			},
		},
	}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one create; got %+v", got)
	}
}

// TestDiff_ScreenshotsUpdate: desired screenshots differ from live.
func TestDiff_ScreenshotsUpdate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Screenshots: &config.ScreenshotsSpec{
			Locales: map[string]map[string][]config.ScreenshotFile{
				"en-US": {"APP_IPHONE_67": {{Path: "new.png"}}},
			},
		},
	}}
	live := &config.State{Spec: config.StateSpec{
		Screenshots: &config.ScreenshotsSpec{
			Locales: map[string]map[string][]config.ScreenshotFile{
				"en-US": {"APP_IPHONE_67": {{Path: "old.png"}}},
			},
		},
	}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpUpdate {
		t.Fatalf("expected one update; got %+v", got)
	}
}

// TestDiff_IAPDelete: IAP product in desired as update triggers per-field diff.
func TestDiff_IAPUpdate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		IAP: &config.IAPSpec{
			Products: map[string]config.IAPProduct{
				"com.app.pro": {Type: "NON_CONSUMABLE", Name: strPtr("Pro")},
			},
		},
	}}
	live := &config.State{Spec: config.StateSpec{
		IAP: &config.IAPSpec{
			Products: map[string]config.IAPProduct{
				"com.app.pro": {Type: "NON_CONSUMABLE", Name: strPtr("Old Pro")},
			},
		},
	}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpUpdate {
		t.Fatalf("expected one name update; got %+v", got)
	}
	if got[0].Path != "/spec/iap/products/com.app.pro/name" {
		t.Errorf("unexpected path %q", got[0].Path)
	}
}

// TestDiff_CustomProductPageCreate: new CPP in desired.
func TestDiff_CustomProductPageCreate(t *testing.T) {
	cpp := config.CustomProductPagesSpec{
		"summer": {Visible: boolPtr(true)},
	}
	desired := &config.State{Spec: config.StateSpec{CustomProductPages: &cpp}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one CPP create; got %+v", got)
	}
}

// TestDiff_CustomProductPageUpdate: CPP visible flag change.
func TestDiff_CustomProductPageUpdate(t *testing.T) {
	desiredCPP := config.CustomProductPagesSpec{"summer": {Visible: boolPtr(true)}}
	liveCPP := config.CustomProductPagesSpec{"summer": {Visible: boolPtr(false)}}
	desired := &config.State{Spec: config.StateSpec{CustomProductPages: &desiredCPP}}
	live := &config.State{Spec: config.StateSpec{CustomProductPages: &liveCPP}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpUpdate {
		t.Fatalf("expected one update; got %+v", got)
	}
}

// TestDiff_TestFlightGroupCreate: new group in desired.
func TestDiff_TestFlightGroupCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		TestFlight: &config.TestFlightSpec{
			Groups: map[string]config.TestFlightGroup{
				"beta": {IsInternal: boolPtr(false)},
			},
		},
	}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one group create; got %+v", got)
	}
}

// TestDiff_ExportComplianceCreate: desired export compliance with empty live.
func TestDiff_ExportComplianceCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		ExportCompliance: &config.ExportComplianceSpec{
			UsesNonExemptEncryption: boolPtr(false),
		},
	}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one create; got %+v", got)
	}
}

// TestDiff_ReviewerDemoCreate: desired reviewer demo with empty live.
func TestDiff_ReviewerDemoCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		ReviewerDemo: &config.ReviewerDemoSpec{
			Username: strPtr("demo@example.com"),
		},
	}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one create; got %+v", got)
	}
}

// TestDiff_CategoriesSubcategoriesCreate: subcategory list with empty live.
func TestDiff_CategoriesSubcategoriesCreate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{
			PrimarySubcategories: []string{"GAMES_ACTION", "GAMES_ADVENTURE"},
		},
	}}
	got := Diff(desired, &config.State{})
	if len(got) != 1 || got[0].Op != OpCreate {
		t.Fatalf("expected one create; got %+v", got)
	}
}

// TestDiff_CategoriesSubcategoriesUpdate: subcategory list change.
func TestDiff_CategoriesSubcategoriesUpdate(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{
			PrimarySubcategories: []string{"GAMES_ACTION"},
		},
	}}
	live := &config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{
			PrimarySubcategories: []string{"GAMES_ADVENTURE"},
		},
	}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpUpdate {
		t.Fatalf("expected one update; got %+v", got)
	}
}

// TestDiff_TestFlightPublicLinkLimit: int pointer field change.
func TestDiff_TestFlightPublicLinkLimit(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{
		TestFlight: &config.TestFlightSpec{
			Groups: map[string]config.TestFlightGroup{
				"beta": {PublicLinkLimit: intPtr(100)},
			},
		},
	}}
	live := &config.State{Spec: config.StateSpec{
		TestFlight: &config.TestFlightSpec{
			Groups: map[string]config.TestFlightGroup{
				"beta": {PublicLinkLimit: intPtr(50)},
			},
		},
	}}
	got := Diff(desired, live)
	if len(got) != 1 || got[0].Op != OpUpdate {
		t.Fatalf("expected one update; got %+v", got)
	}
}
