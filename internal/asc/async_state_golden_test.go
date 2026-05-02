package asc

// Golden-table tests for AsyncState persistence. Round-trip a hand-crafted
// canonical state file through Load + Persist to catch any accidental field
// renames or marshal-order drift, and verify that the three corruption shapes
// (truncated write, future schema, syntactically invalid JSON) all surface as
// ErrStateCorrupt. The atomic-rename torture case forces a real os.Rename
// failure and asserts the original file is byte-equivalent to its pre-state.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// goldenAsyncDir is the on-disk root for the canonical / corrupted state
// fixtures. Tests load these via os.ReadFile, install them into the per-test
// SKIPPER_STATE_HOME tree, and call LoadAsyncState against the install path.
const goldenAsyncDir = "testdata/golden/async"

// readGoldenAsync returns the bytes of a fixture under testdata/golden/async/.
// Fails the test cleanly if the fixture is missing — golden files are
// load-bearing, a missing one should not silently degrade to a happy path.
func readGoldenAsync(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(goldenAsyncDir, name)
	b, err := os.ReadFile(path) //nolint:gosec // test-only path constant
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return b
}

// installGoldenAsync drops a golden fixture into the per-test
// SKIPPER_STATE_HOME tree at the path that LoadAsyncState will look for given
// (bundleID, reportClass). Returns the absolute install path so the caller
// can compare bytes against the on-disk file after a Persist round-trip.
func installGoldenAsync(t *testing.T, root, bundleID string, class ReportClass, contents []byte) string {
	t.Helper()
	dir := filepath.Join(root, bundleID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, string(class)+".json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write golden to %s: %v", path, err)
	}
	return path
}

// TestAsyncState_GoldenRoundTrip loads the canonical fixture, re-persists it
// via PersistAsyncState, and asserts the on-disk file is byte-equivalent to
// the original. This catches:
//   - JSON field renames (struct tag drift)
//   - MarshalIndent format changes (Go stdlib swap)
//   - Field-order swaps in AsyncState (json/encoding emits in struct order)
//
// The fixture lives under testdata/golden/async/state_canonical.json.
func TestAsyncState_GoldenRoundTrip(t *testing.T) {
	root := withStateRoot(t)
	canonical := readGoldenAsync(t, "state_canonical.json")
	installPath := installGoldenAsync(t, root, "com.example.testapp", ReportClassAnalytics, canonical)

	loaded, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}

	// Sanity-check a representative sample of fields before re-persisting.
	if loaded.SchemaVersion != AsyncStateSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", loaded.SchemaVersion, AsyncStateSchemaVersion)
	}
	if loaded.RequestID != "REQ-CMD-1" {
		t.Errorf("RequestID = %q, want REQ-CMD-1", loaded.RequestID)
	}
	if len(loaded.Reports) != 2 {
		t.Fatalf("Reports = %d, want 2", len(loaded.Reports))
	}
	if loaded.Reports[0].Category != CategoryAppUsage {
		t.Errorf("Reports[0].Category = %q, want %q", loaded.Reports[0].Category, CategoryAppUsage)
	}
	if len(loaded.DownloadedSegments) != 2 {
		t.Errorf("DownloadedSegments = %v, want len 2", loaded.DownloadedSegments)
	}

	if err := PersistAsyncState(loaded); err != nil {
		t.Fatalf("PersistAsyncState: %v", err)
	}

	got, err := os.ReadFile(installPath) //nolint:gosec // test-only path
	if err != nil {
		t.Fatalf("re-read after persist: %v", err)
	}
	if !bytes.Equal(canonical, got) {
		t.Fatalf("round-trip drift detected.\nwant (canonical fixture):\n%s\n\ngot (after Persist):\n%s",
			canonical, got)
	}
}

// TestAsyncState_GoldenCorruptionShapes exercises the three documented
// corruption modes in a single table. All must wrap ErrStateCorrupt so
// callers can branch on errors.Is(err, ErrStateCorrupt) rather than parsing
// error strings.
func TestAsyncState_GoldenCorruptionShapes(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
	}{
		{"truncated mid-array", "state_truncated.json"},
		{"future schema version", "state_wrong_schema.json"},
		{"invalid json", "state_invalid_json.json"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			root := withStateRoot(t)
			body := readGoldenAsync(t, tc.fixture)
			installGoldenAsync(t, root, "com.example.testapp", ReportClassAnalytics, body)

			_, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
			if err == nil {
				t.Fatalf("LoadAsyncState accepted corrupt fixture %s", tc.fixture)
			}
			if !errors.Is(err, ErrStateCorrupt) {
				t.Fatalf("err = %v, want errors.Is ErrStateCorrupt (fixture %s)", err, tc.fixture)
			}
		})
	}
}

// TestAsyncState_AtomicWriteFailurePreservesOriginal forces os.Rename to
// fail and verifies the pre-existing state file is byte-equivalent to its
// state before the failed write. Approach: seed the canonical fixture, then
// chmod the parent directory to 0500 (read+execute only) so CreateTemp +
// Rename inside it fails. The defer in PersistAsyncState should clean up the
// tmp file; the canonical file must be unchanged.
//
// Skipped on Windows (chmod semantics differ) and when running as root
// (root bypasses POSIX directory permissions).
func TestAsyncState_AtomicWriteFailurePreservesOriginal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX chmod semantics required to force rename failure")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}

	root := withStateRoot(t)
	canonical := readGoldenAsync(t, "state_canonical.json")
	stateDir := filepath.Join(root, "com.example.testapp")
	statePath := installGoldenAsync(t, root, "com.example.testapp", ReportClassAnalytics, canonical)

	// Snapshot before the failed write so we can compare bytes after.
	before, err := os.ReadFile(statePath) //nolint:gosec // test-only path
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}

	// Lock down the directory so CreateTemp inside it fails.
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatalf("chmod 0500: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	// Construct a different state — if Persist somehow succeeded, the file
	// would change.
	mutation := AsyncState{
		BundleID:    "com.example.testapp",
		ReportClass: ReportClassAnalytics,
		RequestID:   "REQ-DIFFERENT",
		Status:      "completed",
	}
	mutation.SubmittedAt = mustTime(t, "2026-06-01T00:00:00Z")

	err = PersistAsyncState(mutation)
	if err == nil {
		t.Fatal("PersistAsyncState succeeded despite read-only dir; expected an error")
	}

	// Restore dir perms so we can verify the file.
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}

	after, err := os.ReadFile(statePath) //nolint:gosec // test-only path
	if err != nil {
		t.Fatalf("read after failed persist: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("failed Persist corrupted the original file\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// Ensure no orphan .tmp-* files were left behind in the directory after
	// the cleanup defer ran. (CreateTemp may itself fail before any file is
	// created — that path produces no orphan; we only fail if one persists.)
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "analytics.json" {
			continue
		}
		t.Errorf("orphan file in state dir after failed persist: %s", e.Name())
	}
}

// mustTime parses an RFC3339 timestamp; only the test's seed timestamps
// flow through this so a parse error is a test bug, not a runtime concern.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tt
}
