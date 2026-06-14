package state

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// TestRoundTrip_FetchMarshalLoadRefetchDiffEmpty is the keystone L2 contract:
// fetch → YAML → reload → re-fetch → diff must be empty.
func TestRoundTrip_FetchMarshalLoadRefetchDiffEmpty(t *testing.T) {
	srv := httptest.NewServer(fullCoverageHandler(t))
	defer srv.Close()
	c := fixtureClient(t, srv)

	ctx := context.Background()
	bundleID := "com.example.app"
	opts := FetchOpts{Version: "1.0", Platform: "IOS"}

	first, err := Fetch(ctx, c, bundleID, opts)
	if err != nil {
		t.Fatalf("Fetch #1: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(first); err != nil {
		t.Fatalf("encode yaml: %v", err)
	}
	_ = enc.Close()
	if err := writeFileTest(t, path, buf.Bytes()); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	reloaded, err := config.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if diags := config.Validate(path, reloaded); len(diags) > 0 {
		for _, d := range diags {
			t.Errorf("schema validation: %s", d)
		}
		t.FailNow()
	}

	second, err := Fetch(ctx, c, bundleID, opts)
	if err != nil {
		t.Fatalf("Fetch #2: %v", err)
	}

	changes := plan.Diff(reloaded, second)
	if len(changes) != 0 {
		for _, ch := range changes {
			t.Errorf("phantom diff: %s %s: %v -> %v", ch.Op, ch.Path, ch.From, ch.To)
		}
		t.Fatalf("expected zero diffs after round-trip, got %d", len(changes))
	}

	if changes := plan.Diff(first, second); len(changes) != 0 {
		for _, ch := range changes {
			t.Errorf("non-deterministic Fetch: %s %s: %v -> %v", ch.Op, ch.Path, ch.From, ch.To)
		}
	}
}

// TestRoundTrip_AllSurfacesPopulated pins surface-level coverage; a silent regression that drops
// a whole surface would fool the zero-diff invariant above (both paths would skip the surface).
func TestRoundTrip_AllSurfacesPopulated(t *testing.T) {
	reloaded := fetchEncodeReload(t)
	for _, ck := range surfaceChecks(reloaded) {
		if !ck.ok {
			t.Errorf("post-roundtrip: %s not populated", ck.name)
		}
	}
}

type surfaceCheck struct {
	name string
	ok   bool
}

func surfaceChecks(s *config.State) []surfaceCheck {
	v := s.Spec.Version
	ar := s.Spec.AgeRating
	cat := s.Spec.Categories
	meta := s.Spec.Metadata
	iap := s.Spec.IAP
	tf := s.Spec.TestFlight
	return []surfaceCheck{
		{"spec.version", v != nil && derefEq(v.Copyright, "© 2026")},
		{"spec.version.releaseType", v != nil && derefEq(v.ReleaseType, "MANUAL")},
		{"spec.build.number", s.Spec.Build != nil && s.Spec.Build.Number == "42"},
		{"spec.metadata.locales[en-US].description", meta != nil && meta.Locales["en-US"].Description != nil},
		{"spec.metadata.locales[en-US].name (cross-resource appInfoLoc)", meta != nil && meta.Locales["en-US"].Name != nil},
		{"spec.screenshots.locales[en-US][APP_IPHONE_69]", s.Spec.Screenshots != nil && len(s.Spec.Screenshots.Locales["en-US"]["APP_IPHONE_69"]) > 0},
		{"spec.iap.products[com.x.lifetime]", iap != nil && iap.Products["com.x.lifetime"].Type == "NON_CONSUMABLE"},
		{"spec.iap.products[com.x.lifetime].localizations[en-US]", iap != nil && len(iap.Products["com.x.lifetime"].Localizations) > 0},
		{"spec.ageRating.cartoonOrFantasyViolence", ar != nil && derefEq(ar.CartoonOrFantasyViolence, "NONE")},
		{"spec.ageRating.gambling", ar != nil && derefEq(ar.Gambling, false)},
		{"spec.exportCompliance.usesNonExemptEncryption", s.Spec.ExportCompliance != nil && s.Spec.ExportCompliance.UsesNonExemptEncryption != nil},
		{"spec.reviewerDemo.contactEmail", s.Spec.ReviewerDemo != nil && s.Spec.ReviewerDemo.ContactEmail != nil},
		{"spec.categories.primary", cat != nil && derefEq(cat.Primary, "EDUCATION")},
		{"spec.categories.secondary", cat != nil && derefEq(cat.Secondary, "REFERENCE")},
		{"spec.pricing.baseTerritory", s.Spec.Pricing != nil && s.Spec.Pricing.BaseTerritory != nil},
		{"spec.testflight.groups[family]", tf != nil && len(tf.Groups) > 0},
		{"spec.testflight.groups[family].testers", tf != nil && len(tf.Groups["family"].Testers) > 0},
		{"spec.customProductPages[summer-2026]", s.Spec.CustomProductPages != nil && (*s.Spec.CustomProductPages)["summer-2026"].Visible != nil},
	}
}

func derefEq[T comparable](p *T, want T) bool {
	return p != nil && *p == want
}

func writeFileTest(t *testing.T, path string, data []byte) error {
	t.Helper()
	return os.WriteFile(path, data, 0o600)
}

// fetchEncodeReload fetches the full-coverage fixture, encodes to state.yaml, reloads, and validates.
func fetchEncodeReload(t *testing.T) *config.State {
	t.Helper()
	srv := httptest.NewServer(fullCoverageHandler(t))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	first, err := Fetch(context.Background(), c, "com.example.app",
		FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	path := filepath.Join(t.TempDir(), "state.yaml")
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(first); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = enc.Close()
	if err := writeFileTest(t, path, buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}

	reloaded, err := config.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if diags := config.Validate(path, reloaded); len(diags) != 0 {
		for _, d := range diags {
			t.Errorf("schema diag: %s", d)
		}
		t.FailNow()
	}
	return reloaded
}
