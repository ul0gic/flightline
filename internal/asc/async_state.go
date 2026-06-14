// State persists to $XDG_STATE_HOME/flightline/<bundleId>/<reportClass>.json using atomic rename.
// XDG_STATE_HOME is used (not cache): the OS may evict cache, and losing an in-flight poll is unrecoverable.
package asc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AsyncStateSchemaVersion is the on-disk JSON schema version.
// Bump when AsyncState's shape changes; LoadAsyncState rejects unrecognised versions (forward-incompat by design).
const AsyncStateSchemaVersion = 1

// ReportClass enumerates the three async-poll report families; used as the state-file basename.
type ReportClass string

const (
	ReportClassAnalytics ReportClass = "analytics"
	ReportClassSales     ReportClass = "sales"
	ReportClassFinance   ReportClass = "finance"
)

// PersistedAnalyticsReport is the on-disk shape of one report row inside AsyncState.
// Mirrors AnalyticsReport but stays JSON-stable across versions of the in-memory struct.
type PersistedAnalyticsReport struct {
	ID       ReportID          `json:"id"`
	Name     string            `json:"name,omitempty"`
	Category AnalyticsCategory `json:"category,omitempty"`
}

// AsyncState is the on-disk shape persisted between poll runs; BundleID+ReportClass compose the file path.
type AsyncState struct {
	SchemaVersion      int                        `json:"schemaVersion"`
	BundleID           string                     `json:"bundleId"`
	ReportClass        ReportClass                `json:"reportClass"`
	RequestID          RequestID                  `json:"requestId,omitempty"`
	SubmittedAt        time.Time                  `json:"submittedAt"`
	LastPollAt         time.Time                  `json:"lastPollAt,omitempty"`
	Status             string                     `json:"status,omitempty"`
	Reports            []PersistedAnalyticsReport `json:"reports,omitempty"`
	DownloadedSegments []string                   `json:"downloadedSegments,omitempty"`
}

// ErrStateCorrupt is returned by LoadAsyncState when the file exists but cannot be decoded
// (truncated write, JSON corruption, or schema-version mismatch).
var ErrStateCorrupt = errors.New("asc: async state file is corrupt or unreadable")

// PersistAsyncState writes state atomically (tmp + rename). SchemaVersion is always forced to AsyncStateSchemaVersion.
func PersistAsyncState(state AsyncState) error {
	if state.BundleID == "" {
		return errors.New("asc: PersistAsyncState: BundleID is required")
	}
	if state.ReportClass == "" {
		return errors.New("asc: PersistAsyncState: ReportClass is required")
	}
	state.SchemaVersion = AsyncStateSchemaVersion

	path, err := stateFilePath(state.BundleID, state.ReportClass)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("asc: create state dir: %w", err)
	}

	buf, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("asc: marshal async state: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("asc: create temp state file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename succeeds.
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("asc: write temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("asc: fsync temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("asc: close temp state file: %w", err)
	}
	// 0600 in case CreateTemp's default permission is wider than we want
	// (it usually creates 0600 on POSIX, but be explicit).
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("asc: chmod temp state file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("asc: rename state file: %w", err)
	}
	committed = true
	return nil
}

// LoadAsyncState reads state for (bundleID, reportClass). Returns os.ErrNotExist when no file exists.
// Returns ErrStateCorrupt on malformed or schema-mismatched files; do not silently fall back to fresh state.
func LoadAsyncState(bundleID string, reportClass ReportClass) (AsyncState, error) {
	if bundleID == "" {
		return AsyncState{}, errors.New("asc: LoadAsyncState: bundleID is required")
	}
	if reportClass == "" {
		return AsyncState{}, errors.New("asc: LoadAsyncState: reportClass is required")
	}

	path, err := stateFilePath(bundleID, reportClass)
	if err != nil {
		return AsyncState{}, err
	}
	buf, err := os.ReadFile(path) //nolint:gosec // path composed from validated components
	if err != nil {
		// Surface os.ErrNotExist verbatim so errors.Is works.
		return AsyncState{}, err
	}

	var state AsyncState
	if err := json.Unmarshal(buf, &state); err != nil {
		return AsyncState{}, fmt.Errorf("%w: %s: %w", ErrStateCorrupt, path, err)
	}
	if state.SchemaVersion == 0 || state.SchemaVersion > AsyncStateSchemaVersion {
		return AsyncState{}, fmt.Errorf(
			"%w: %s: schemaVersion %d is unsupported (this build understands version %d)",
			ErrStateCorrupt, path, state.SchemaVersion, AsyncStateSchemaVersion,
		)
	}
	return state, nil
}

// StateFilePath returns the absolute on-disk path for (bundleID, reportClass) state.
// Exposed for tests and diagnostic commands; validates bundleID (no path traversal).
func StateFilePath(bundleID string, reportClass ReportClass) (string, error) {
	return stateFilePath(bundleID, reportClass)
}

func stateFilePath(bundleID string, reportClass ReportClass) (string, error) {
	if err := validateBundleIDForPath(bundleID); err != nil {
		return "", err
	}
	if err := validateReportClass(reportClass); err != nil {
		return "", err
	}
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, bundleID, string(reportClass)+".json"), nil
}

// validateBundleIDForPath rejects bundle IDs that could escape the per-app subdirectory.
// Apple's are dotted reverse-DNS; a path separator or ".." is hostile.
func validateBundleIDForPath(bundleID string) error {
	if bundleID == "" {
		return errors.New("asc: bundleID is required")
	}
	if strings.ContainsAny(bundleID, `/\`) {
		return fmt.Errorf("asc: bundleID %q contains a path separator", bundleID)
	}
	if bundleID == "." || bundleID == ".." || strings.Contains(bundleID, "..") {
		return fmt.Errorf("asc: bundleID %q contains path-traversal segments", bundleID)
	}
	if strings.ContainsRune(bundleID, 0) {
		return errors.New("asc: bundleID contains NUL byte")
	}
	return nil
}

// validateReportClass rejects unknown report classes so a typo can't write
// state to a path the loader will never look at.
func validateReportClass(c ReportClass) error {
	switch c {
	case ReportClassAnalytics, ReportClassSales, ReportClassFinance:
		return nil
	default:
		return fmt.Errorf("asc: unknown reportClass %q (want one of: analytics, sales, finance)", c)
	}
}

// stateRoot returns $XDG_STATE_HOME/flightline, or $HOME/.local/state/flightline when unset.
// FLIGHTLINE_STATE_HOME is an undocumented test-only override.
func stateRoot() (string, error) {
	// Checked first: must override even XDG values.
	if override := os.Getenv("FLIGHTLINE_STATE_HOME"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "flightline"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("asc: resolve home dir: %w", err)
	}
	// Windows gets the same .local/state path; Flightline is macOS/Linux-first.
	_ = runtime.GOOS
	return filepath.Join(home, ".local", "state", "flightline"), nil
}
