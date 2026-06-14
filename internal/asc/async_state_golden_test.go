package asc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const goldenAsyncDir = "testdata/golden/async"

// readGoldenAsync returns a fixture's bytes, failing the test if it's missing.
// A missing golden file must fail loudly, not silently degrade to a happy path.
func readGoldenAsync(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(goldenAsyncDir, name)
	b, err := os.ReadFile(path) //nolint:gosec // test-only path constant
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return b
}

// installGoldenAsync writes a fixture to the path LoadAsyncState resolves for (bundleID, class).
// Returns the install path so callers can byte-compare after a Persist round-trip.
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

// TestAsyncState_GoldenRoundTrip asserts a re-persisted canonical fixture is byte-identical,
// catching struct-tag renames, MarshalIndent format changes, and field-order swaps.
func TestAsyncState_GoldenRoundTrip(t *testing.T) {
	root := withStateRoot(t)
	canonical := readGoldenAsync(t, "state_canonical.json")
	installPath := installGoldenAsync(t, root, "com.example.testapp", ReportClassAnalytics, canonical)

	loaded, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}

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

// TestAsyncState_GoldenCorruptionShapes asserts all three corruption modes wrap ErrStateCorrupt,
// so callers can branch on errors.Is rather than parsing error strings.
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

// TestAsyncState_AtomicWriteFailurePreservesOriginal forces a write failure by chmod'ing the state
// dir to 0500 (so CreateTemp fails) and asserts the pre-existing file is byte-unchanged.
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

	before, err := os.ReadFile(statePath) //nolint:gosec // test-only path
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}

	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatalf("chmod 0500: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

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

	// No orphan .tmp-* may survive the cleanup defer. CreateTemp failing before it creates
	// a file is fine (no orphan); we fail only if one persists.
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
