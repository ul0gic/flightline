package asc

// State persistence for async-poll lifecycles.
//
// The analytics request → poll → download flow can take minutes to hours per
// request, so an interruption (Ctrl-C, ssh disconnect, machine reboot) must
// not lose work already done. This file owns the JSON-on-disk format and
// the atomic-rename writer that keeps it safe.
//
// On-disk layout:
//
//   $XDG_STATE_HOME/skipper/<bundleId>/<reportClass>.json
//
// where <reportClass> ∈ {"analytics", "sales", "finance"} and bundleId is
// the dotted reverse-DNS app identifier (e.g. "com.example.app"). The path
// pattern is intentional: state is per-app and per-class, so concurrent
// flows across multiple apps don't collide and a finance fetch can't trash
// an in-flight analytics poll.
//
// XDG_STATE_HOME is the standard for "data files held to know how to restore
// previous state of the application" (per the XDG base-dir spec). On macOS
// and Linux, $HOME/.local/state/skipper/ is the canonical fallback. We do
// NOT use os.UserCacheDir(): cache implies the OS may delete it, and losing
// a multi-hour analytics request mid-poll is exactly what we're guarding
// against.

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

// AsyncStateSchemaVersion is the on-disk JSON schema version. Bump when the
// shape of AsyncState changes; LoadAsyncState rejects files with an
// unrecognised SchemaVersion (forward-incompat by design — a Skipper from
// the future shouldn't silently misread state from an older Skipper).
const AsyncStateSchemaVersion = 1

// ReportClass enumerates the three async-poll report families. Used as the
// state-file basename so callers can't collide flows.
type ReportClass string

// Report-class literals. Keep in sync with the file naming convention.
const (
	ReportClassAnalytics ReportClass = "analytics"
	ReportClassSales     ReportClass = "sales"
	ReportClassFinance   ReportClass = "finance"
)

// PersistedAnalyticsReport is the on-disk shape of one report row inside
// AsyncState. Mirrors AnalyticsReport but stays JSON-stable across versions
// of the in-memory struct.
type PersistedAnalyticsReport struct {
	ID       ReportID          `json:"id"`
	Name     string            `json:"name,omitempty"`
	Category AnalyticsCategory `json:"category,omitempty"`
}

// AsyncState is the on-disk shape persisted between poll runs.
//
// Stable JSON contract:
//   - SchemaVersion gates forward-compat.
//   - BundleID + ReportClass identify the flow.
//   - RequestID is the Apple-assigned analytics request ID (empty for
//     sales/finance, which are synchronous and persist only the most-recent
//     fetch metadata).
//   - SubmittedAt / LastPollAt are RFC3339 UTC timestamps.
//   - Status mirrors Apple's request state (free-form string; Apple has
//     no public enum but the values seen are "queued", "processing",
//     "completed", "failed", plus our own "stopped" for ONGOING-inactivity).
//   - Reports is the de-dup list of reports observed via PollAnalyticsReport.
//   - DownloadedSegments tracks segment IDs already downloaded so the resume
//     path can skip them.
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

// ErrStateCorrupt is returned by LoadAsyncState when the file exists but
// cannot be decoded (truncated write, JSON corruption, schema-version
// mismatch).
var ErrStateCorrupt = errors.New("asc: async state file is corrupt or unreadable")

// PersistAsyncState writes state to disk under
// $XDG_STATE_HOME/skipper/<bundleId>/<reportClass>.json using an atomic
// rename. If a Ctrl-C lands mid-write, the original file is preserved
// untouched.
//
// Validates that BundleID and ReportClass are present (those are the file
// path components — empty values would write to a wrong location).
//
// SchemaVersion is forced to AsyncStateSchemaVersion regardless of caller
// intent. Callers should never set it manually; PersistAsyncState owns the
// invariant.
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

// LoadAsyncState reads the state file for (bundleID, reportClass) and
// returns the decoded AsyncState. If no file exists, returns
// (zero, fs.ErrNotExist) — callers should branch on errors.Is(err,
// os.ErrNotExist) for the "no prior state" path.
//
// On a malformed file (truncated write, JSON syntax error, unknown schema
// version), returns ErrStateCorrupt. Callers should NOT silently fall back
// to a fresh state — corruption is a signal to ask the user, not to
// pretend the prior request never happened.
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
		return AsyncState{}, fmt.Errorf("%w: %s: %v", ErrStateCorrupt, path, err)
	}
	if state.SchemaVersion == 0 || state.SchemaVersion > AsyncStateSchemaVersion {
		return AsyncState{}, fmt.Errorf(
			"%w: %s: schemaVersion %d is unsupported (this build understands version %d)",
			ErrStateCorrupt, path, state.SchemaVersion, AsyncStateSchemaVersion,
		)
	}
	return state, nil
}

// StateFilePath returns the absolute on-disk path where (bundleID,
// reportClass) state would live. Exposed for tests and for diagnostic
// commands like `skipper analytics resume --where`. Validates that
// bundleID is well-formed (no path traversal).
func StateFilePath(bundleID string, reportClass ReportClass) (string, error) {
	return stateFilePath(bundleID, reportClass)
}

// stateFilePath composes the absolute path. Returns an error when the
// bundleID contains characters that would escape the per-app subdirectory
// (path separators, "..").
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

// validateBundleIDForPath rejects bundle IDs that would escape the
// per-app subdirectory or otherwise produce surprising paths. Apple's
// bundle IDs are dotted reverse-DNS strings; anything with a path separator
// or ".." is hostile.
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
		return fmt.Errorf("asc: bundleID contains NUL byte")
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

// stateRoot returns $XDG_STATE_HOME/skipper, falling back to
// $HOME/.local/state/skipper when XDG_STATE_HOME is unset (per the XDG
// base-dir spec). Tests override via SKIPPER_STATE_HOME for hermetic
// behavior; that env var is intentionally undocumented in user-facing
// surfaces — it's a test escape hatch only.
func stateRoot() (string, error) {
	// Test escape hatch — first because it must override even XDG values.
	if override := os.Getenv("SKIPPER_STATE_HOME"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "skipper"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("asc: resolve home dir: %w", err)
	}
	// On Windows the XDG fallback isn't standard; we still write under
	// $USERPROFILE/.local/state/skipper for consistency with the rest of
	// Skipper's macOS/Linux-first ergonomics. Cross-platform polish is a
	// separate concern.
	_ = runtime.GOOS
	return filepath.Join(home, ".local", "state", "skipper"), nil
}
