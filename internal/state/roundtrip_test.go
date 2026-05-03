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

	"github.com/ul0gic/skipper/internal/config"
	"github.com/ul0gic/skipper/internal/plan"
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

// writeFileTest is a tiny test helper that writes a file at mode 0600
// and surfaces any I/O error to the caller.
func writeFileTest(t *testing.T, path string, data []byte) error {
	t.Helper()
	return os.WriteFile(path, data, 0o600)
}
