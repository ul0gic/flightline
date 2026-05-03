package plan

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ul0gic/skipper/internal/config"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// TestDiff_IdenticalIsZero — Diff(s, s) is empty.
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

// TestDiff_NilDesiredFieldNotManaged — desired=nil for a sub-spec means
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

// TestDiff_VersionCopyrightUpdate — single leaf-level change.
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

// TestDiff_VersionCopyrightCreate — live has no value, desired sets one.
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

// TestDiff_StableOrdering — Path-sorted output is deterministic.
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

// TestDiff_NilLive — first apply against an unmanaged app: every
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

// TestDiff_IAPCreate — net-new IAP product becomes a create entry.
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

// TestDiff_TestFlightTesterAddRemove — set diff on tester roster.
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
