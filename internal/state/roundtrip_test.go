// roundtrip_test.go — the keystone L2 invariant test.
//
// fetch → marshal YAML → reload → re-fetch → diff(reloaded, refetched)
// must be empty. If this test fails, the L2 user contract is broken:
// users editing state.yaml will see phantom diffs that don't exist.
//
// The fixture server is deterministic (single fullCoverageHandler from
// fetch_surfaces_test.go) so both fetch passes return byte-identical
// responses.

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

// TestRoundTrip_FetchMarshalLoadRefetchDiffEmpty exercises the
// keystone L2 contract: fetch live state, write it to disk, reload,
// re-fetch, diff. The diff must be empty. Drift here means the
// projection in fetch.go differs from the consumption in the diff
// engine — phantom changes the user can't act on.
func TestRoundTrip_FetchMarshalLoadRefetchDiffEmpty(t *testing.T) {
	srv := httptest.NewServer(fullCoverageHandler(t))
	defer srv.Close()
	c := fixtureClient(t, srv)

	ctx := context.Background()
	bundleID := "com.example.app"
	opts := FetchOpts{Version: "1.0", Platform: "IOS"}

	// 1. First fetch.
	first, err := Fetch(ctx, c, bundleID, opts)
	if err != nil {
		t.Fatalf("Fetch #1: %v", err)
	}

	// 2. Marshal to YAML on disk.
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

	// 3. Reload via the loader (the same code cmd/plan and cmd/apply use).
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

	// 4. Re-fetch.
	second, err := Fetch(ctx, c, bundleID, opts)
	if err != nil {
		t.Fatalf("Fetch #2: %v", err)
	}

	// 5. Diff(reloaded, second) must be empty. This is the user
	// invariant: editing-the-yaml-and-applying must round-trip clean.
	changes := plan.Diff(reloaded, second)
	if len(changes) != 0 {
		for _, ch := range changes {
			t.Errorf("phantom diff: %s %s: %v -> %v", ch.Op, ch.Path, ch.From, ch.To)
		}
		t.Fatalf("expected zero diffs after round-trip, got %d", len(changes))
	}

	// 6. Sanity: Diff(first, second) is also empty (live → live).
	if changes := plan.Diff(first, second); len(changes) != 0 {
		for _, ch := range changes {
			t.Errorf("non-deterministic Fetch: %s %s: %v -> %v", ch.Op, ch.Path, ch.From, ch.To)
		}
	}
}

// TestRoundTrip_AllSurfacesPopulated asserts every L2 spec.* surface
// survives the fetch -> YAML -> LoadState pipeline non-zero. The
// keystone TestRoundTrip_FetchMarshalLoadRefetchDiffEmpty above proves
// the *invariant* (zero diffs) but a regression that drops a whole
// surface (e.g. fetch projection stops emitting customProductPages)
// could be silently consistent — both paths would skip the surface.
//
// This second test pins the surface-level coverage: after the reload,
// every populated section in fullCoverageHandler must show up in
// reloaded.Spec.* with the exact values the fixture produced.
func TestRoundTrip_AllSurfacesPopulated(t *testing.T) {
	srv := httptest.NewServer(fullCoverageHandler(t))
	defer srv.Close()
	c := fixtureClient(t, srv)

	first, err := Fetch(context.Background(), c, "com.example.app",
		FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
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

	// Every L2 surface in the schema (privacyLabels intentionally
	// absent — see PRD § "What Flightline is NOT" + ISSUE-002).
	checks := []struct {
		name string
		ok   bool
	}{
		{"spec.version", reloaded.Spec.Version != nil &&
			reloaded.Spec.Version.Copyright != nil &&
			*reloaded.Spec.Version.Copyright == "© 2026"},
		{"spec.version.releaseType", reloaded.Spec.Version != nil &&
			reloaded.Spec.Version.ReleaseType != nil &&
			*reloaded.Spec.Version.ReleaseType == "MANUAL"},
		{"spec.build.number", reloaded.Spec.Build != nil &&
			reloaded.Spec.Build.Number == "42"},
		{"spec.metadata.locales[en-US].description", reloaded.Spec.Metadata != nil &&
			reloaded.Spec.Metadata.Locales["en-US"].Description != nil},
		{"spec.metadata.locales[en-US].name (cross-resource appInfoLoc)",
			reloaded.Spec.Metadata != nil &&
				reloaded.Spec.Metadata.Locales["en-US"].Name != nil},
		{"spec.screenshots.locales[en-US][APP_IPHONE_69]",
			reloaded.Spec.Screenshots != nil &&
				len(reloaded.Spec.Screenshots.Locales["en-US"]["APP_IPHONE_69"]) > 0},
		{"spec.iap.products[com.x.lifetime]", reloaded.Spec.IAP != nil &&
			reloaded.Spec.IAP.Products["com.x.lifetime"].Type == "NON_CONSUMABLE"},
		{"spec.iap.products[com.x.lifetime].localizations[en-US]",
			reloaded.Spec.IAP != nil &&
				len(reloaded.Spec.IAP.Products["com.x.lifetime"].Localizations) > 0},
		{"spec.ageRating.cartoonOrFantasyViolence",
			reloaded.Spec.AgeRating != nil &&
				reloaded.Spec.AgeRating.CartoonOrFantasyViolence != nil &&
				*reloaded.Spec.AgeRating.CartoonOrFantasyViolence == "NONE"},
		{"spec.ageRating.gambling", reloaded.Spec.AgeRating != nil &&
			reloaded.Spec.AgeRating.Gambling != nil &&
			!*reloaded.Spec.AgeRating.Gambling},
		{"spec.exportCompliance.usesNonExemptEncryption",
			reloaded.Spec.ExportCompliance != nil &&
				reloaded.Spec.ExportCompliance.UsesNonExemptEncryption != nil},
		{"spec.reviewerDemo.contactEmail", reloaded.Spec.ReviewerDemo != nil &&
			reloaded.Spec.ReviewerDemo.ContactEmail != nil},
		{"spec.categories.primary", reloaded.Spec.Categories != nil &&
			reloaded.Spec.Categories.Primary != nil &&
			*reloaded.Spec.Categories.Primary == "EDUCATION"},
		{"spec.categories.secondary", reloaded.Spec.Categories != nil &&
			reloaded.Spec.Categories.Secondary != nil &&
			*reloaded.Spec.Categories.Secondary == "REFERENCE"},
		{"spec.pricing.baseTerritory", reloaded.Spec.Pricing != nil &&
			reloaded.Spec.Pricing.BaseTerritory != nil},
		{"spec.testflight.groups[family]", reloaded.Spec.TestFlight != nil &&
			len(reloaded.Spec.TestFlight.Groups) > 0},
		{"spec.testflight.groups[family].testers", reloaded.Spec.TestFlight != nil &&
			len(reloaded.Spec.TestFlight.Groups["family"].Testers) > 0},
		{"spec.customProductPages[summer-2026]",
			reloaded.Spec.CustomProductPages != nil &&
				(*reloaded.Spec.CustomProductPages)["summer-2026"].Visible != nil},
	}
	for _, ck := range checks {
		if !ck.ok {
			t.Errorf("post-roundtrip: %s not populated", ck.name)
		}
	}
}

// writeFileTest is a tiny test helper that writes a file at mode 0600
// and surfaces any I/O error to the caller.
func writeFileTest(t *testing.T, path string, data []byte) error {
	t.Helper()
	return os.WriteFile(path, data, 0o600)
}
