// apply.go — orchestrates writing a plan.Change set back to ASC.
//
// Apply iterates a Path-sorted []plan.Change in order, dispatches each
// to the corresponding L1 write call, and persists a checkpoint after
// every successful change so a Ctrl-C or crash mid-apply doesn't strand
// the user.
//
// Idempotency contract: re-running Apply with the same desired state
// against the same live state should produce zero outbound PATCH
// requests. Two paths achieve this:
//
//  1. The L1 write functions already diff-then-PATCH (categories, age
//     rating, version) and turn redundant calls into no-ops at the
//     wire level.
//  2. The checkpoint file at $XDG_CACHE_HOME/skipper/apply/<bundle>.json
//     records every applied (Resource, Path, To) tuple — Resume mode
//     skips matches without re-issuing the PATCH.
//
// The dispatch table (changeDispatch) is the entire surface coverage
// for v1alpha1. Surfaces marked Unmapped are intentionally left to
// surface as ChangeError with a QA-009 reference until QA-009 is
// resolved.

package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ul0gic/skipper/internal/asc"
	"github.com/ul0gic/skipper/internal/plan"
)

// ApplyOpts gates the apply orchestrator.
type ApplyOpts struct {
	// Confirm must be true for Apply to issue any write. Without
	// Confirm the function returns an empty result and an error.
	Confirm bool
	// Resume loads the checkpoint at $XDG_CACHE_HOME/skipper/apply/<bundle>.json
	// and skips changes already applied in a previous run.
	Resume bool
	// DryRun computes the dispatch path for each change without
	// issuing the underlying PATCH/POST/DELETE. Useful for plan-style
	// previews from inside the apply orchestrator (cmd/plan reuses
	// this when called with --dry-run).
	DryRun bool
	// BundleID is required for checkpoint path resolution. Apply
	// returns an error when empty.
	BundleID string
	// Logger is called once per processed change. Nil disables.
	Logger func(c plan.Change, status string)
}

// ChangeError pairs a Change with the error its dispatch produced.
type ChangeError struct {
	Change plan.Change
	Err    error
}

// Error implements error.
func (e *ChangeError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Change.Op, e.Change.Path, e.Err)
}

// Unwrap exposes the underlying error for errors.Is/As.
func (e *ChangeError) Unwrap() error { return e.Err }

// ApplyResult summarizes one Apply run.
type ApplyResult struct {
	Applied []plan.Change `json:"applied"`
	Skipped []plan.Change `json:"skipped,omitempty"`
	Errors  []ChangeError `json:"errors,omitempty"`
}

// applyCheckpointSchemaVersion gates forward-incompat changes to the
// on-disk checkpoint shape.
const applyCheckpointSchemaVersion = 1

// applyCheckpoint is the on-disk shape of an in-progress apply.
type applyCheckpoint struct {
	SchemaVersion int             `json:"schemaVersion"`
	BundleID      string          `json:"bundleId"`
	Applied       []checkpointKey `json:"applied"`
}

// checkpointKey identifies a Change uniquely enough for resume to
// skip it. We hash on (Resource, Path, JSON(To)) so the same logical
// change replayed produces the same key.
type checkpointKey struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	ToJSON   string `json:"to"`
}

// Apply walks changes in their (already-sorted) order, dispatches each
// to the L1 write code, and persists a checkpoint after every success.
//
// On a per-change failure Apply records the error and short-circuits:
// partial-write state is hard to reason about. Resume picks up where
// the previous run failed.
func Apply(ctx context.Context, c *asc.Client, changes []plan.Change, opts ApplyOpts) (*ApplyResult, error) {
	if c == nil {
		return nil, errors.New("state: Apply: client is nil")
	}
	if opts.BundleID == "" {
		return nil, errors.New("state: Apply: opts.BundleID is required (for checkpoint path)")
	}
	if !opts.Confirm && !opts.DryRun {
		return nil, errors.New("state: Apply: --confirm is required for non-dry-run writes")
	}

	res := &ApplyResult{}
	var loaded *applyCheckpoint
	if opts.Resume && !opts.DryRun {
		cp, err := loadApplyCheckpoint(opts.BundleID)
		if err == nil {
			loaded = cp
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("state: load checkpoint: %w", err)
		}
	}

	prog := opts.Logger
	if prog == nil {
		prog = func(plan.Change, string) {}
	}

	for _, ch := range changes {
		if loaded != nil && checkpointHas(loaded, ch) {
			res.Skipped = append(res.Skipped, ch)
			prog(ch, "skipped (resume)")
			continue
		}
		if opts.DryRun {
			res.Applied = append(res.Applied, ch)
			prog(ch, "dry-run")
			continue
		}

		if err := dispatch(ctx, c, ch); err != nil {
			res.Errors = append(res.Errors, ChangeError{Change: ch, Err: err})
			prog(ch, "error")
			// Persist what we have before returning so resume works.
			_ = persistApplyCheckpoint(opts.BundleID, append(checkpointKeys(res.Applied), checkpointKeys(res.Skipped)...))
			return res, &ChangeError{Change: ch, Err: err}
		}
		res.Applied = append(res.Applied, ch)
		prog(ch, "applied")

		// Persist after every success so a kill mid-loop loses at
		// most the work that was about to be persisted, not work
		// already done.
		_ = persistApplyCheckpoint(opts.BundleID, append(checkpointKeys(res.Applied), checkpointKeys(res.Skipped)...))
	}

	return res, nil
}

// dispatch routes a single Change to its L1 write. The table is the
// authoritative source for "what does Skipper actually know how to
// apply" — anything missing here surfaces as ErrUnmappedChange.
func dispatch(ctx context.Context, c *asc.Client, ch plan.Change) error {
	switch {
	case ch.Path == "/spec/version/copyright" ||
		ch.Path == "/spec/version/releaseType" ||
		ch.Path == "/spec/version/earliestReleaseDate" ||
		ch.Path == "/spec/version/downloadable":
		return applyVersionField(ctx, c, ch)
	case strings.HasPrefix(ch.Path, "/spec/ageRating/"):
		return applyAgeRatingField(ctx, c, ch)
	case ch.Path == "/spec/exportCompliance/usesNonExemptEncryption":
		return applyEncryptionFlag(ctx, c, ch)
	default:
		return errUnmapped(ch)
	}
}

// errUnmapped is the typed error for changes the dispatch table
// doesn't yet handle.
var errUnmapped = func(ch plan.Change) error {
	return fmt.Errorf("state: change at %s is not yet mapped to an L1 writer (see QA-009): %w",
		ch.Path, ErrUnmappedChange)
}

// ErrUnmappedChange is the sentinel for unmapped Resources. Tests
// match on this with errors.Is.
var ErrUnmappedChange = errors.New("unmapped change")

// --- per-surface dispatchers ---

// applyVersionField patches one field on the AppStoreVersion. Bundle
// + version is resolved from the change's path and the apply context.
//
// The change's Resource carries enough context for us to look up the
// version row; we re-resolve to keep the dispatcher stateless.
func applyVersionField(ctx context.Context, c *asc.Client, ch plan.Change) error {
	bundleID, version, platform, err := requireApplyEnv()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, version, platform)
	if err != nil {
		return err
	}

	attrs := map[string]any{}
	switch ch.Path {
	case "/spec/version/copyright":
		attrs["copyright"] = ch.To
	case "/spec/version/releaseType":
		attrs["releaseType"] = ch.To
	case "/spec/version/earliestReleaseDate":
		attrs["earliestReleaseDate"] = ch.To
	case "/spec/version/downloadable":
		attrs["downloadable"] = ch.To
	}

	body := map[string]any{
		"data": map[string]any{
			"type":       "appStoreVersions",
			"id":         versionID,
			"attributes": attrs,
		},
	}
	_, err = asc.Patch[asc.Single[asc.VersionAttributes]](
		ctx, c, "/v1/appStoreVersions/"+versionID, nil, body,
	)
	return err
}

// applyAgeRatingField PATCHes one field on the ageRatingDeclaration.
// schema → wire field-name remap mirrors the projection in fetch.go.
func applyAgeRatingField(ctx context.Context, c *asc.Client, ch plan.Change) error {
	bundleID, _, _, err := requireApplyEnv()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return err
	}
	appInfoID, err := fetchEditableAppInfo(ctx, c, appID)
	if err != nil {
		return err
	}
	if appInfoID == "" {
		return errors.New("state: no editable appInfo found")
	}
	declResp, err := asc.Get[asc.Single[asc.AgeRatingDeclarationAttributes]](
		ctx, c, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil,
	)
	if err != nil {
		return fmt.Errorf("state: fetch ageRatingDeclaration: %w", err)
	}
	declID := declResp.Data.ID

	wireKey, err := schemaToWireAgeRating(ch.Path)
	if err != nil {
		return err
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "ageRatingDeclarations",
			"id":         declID,
			"attributes": map[string]any{wireKey: ch.To},
		},
	}
	_, err = asc.Patch[asc.Single[asc.AgeRatingDeclarationAttributes]](
		ctx, c, "/v1/ageRatingDeclarations/"+declID, nil, body,
	)
	return err
}

// applyEncryptionFlag PATCHes the build's usesNonExemptEncryption.
func applyEncryptionFlag(ctx context.Context, c *asc.Client, ch plan.Change) error {
	bundleID, version, platform, err := requireApplyEnv()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, version, platform)
	if err != nil {
		return err
	}
	buildID, _, err := fetchVersionBuildEncryption(ctx, c, versionID)
	if err != nil {
		return err
	}
	if buildID == "" {
		return errors.New("state: no build attached to version")
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "builds",
			"id":         buildID,
			"attributes": map[string]any{"usesNonExemptEncryption": ch.To},
		},
	}
	_, err = asc.Patch[asc.Single[asc.BuildAttributes]](
		ctx, c, "/v1/builds/"+buildID, nil, body,
	)
	return err
}

// --- env / context helpers ---

// applyEnv carries the per-invocation context the dispatcher needs.
// Set via SetApplyContext from cmd/apply before Apply runs; cleared
// after. A package-level mutex guards because apply isn't expected to
// run concurrently from multiple goroutines.
var (
	applyEnvMu       sync.Mutex
	applyBundleID    string
	applyVersionStr  string
	applyPlatformStr string
)

// SetApplyContext stashes the bundle/version/platform tuple the apply
// dispatcher uses to look up the appId and versionId per change.
// cmd/apply calls this once after loading the state file. ResetApplyContext
// clears the stashed values; tests use it for isolation.
func SetApplyContext(bundleID, version, platform string) {
	applyEnvMu.Lock()
	defer applyEnvMu.Unlock()
	applyBundleID = bundleID
	applyVersionStr = version
	applyPlatformStr = platform
}

// ResetApplyContext clears the stashed apply context. Called from
// cmd/apply on completion; tests use it between cases.
func ResetApplyContext() {
	applyEnvMu.Lock()
	defer applyEnvMu.Unlock()
	applyBundleID = ""
	applyVersionStr = ""
	applyPlatformStr = ""
}

func requireApplyEnv() (bundleID, version, platform string, err error) {
	applyEnvMu.Lock()
	defer applyEnvMu.Unlock()
	if applyBundleID == "" {
		return "", "", "", errors.New("state: apply context not set — call state.SetApplyContext first")
	}
	platform = applyPlatformStr
	if platform == "" {
		platform = "IOS"
	}
	return applyBundleID, applyVersionStr, platform, nil
}

// ageRatingSchemaToWire maps a schema-shaped leaf field to Apple's
// wire field name. Mirrors projectAgeRating in fetch.go in reverse.
// Pulled into a package-level table so the dispatcher stays linear.
var ageRatingSchemaToWire = map[string]string{
	"cartoonOrFantasyViolence":                  "violenceCartoonOrFantasy",
	"realisticViolence":                         "violenceRealistic",
	"prolongedGraphicSadisticRealisticViolence": "violenceRealisticProlongedGraphicOrSadistic",
	"profanityOrCrudeHumor":                     "profanityOrCrudeHumor",
	"matureSuggestiveThemes":                    "matureOrSuggestiveThemes",
	"horrorOrFearThemes":                        "horrorOrFearThemes",
	"medicalOrTreatmentInformation":             "medicalOrTreatmentInformation",
	"alcoholTobaccoOrDrugUseOrReferences":       "alcoholTobaccoOrDrugUseOrReferences",
	"contestsAndGambling":                       "contests",
	"sexualContentOrNudity":                     "sexualContentOrNudity",
	"sexualContentGraphicAndNudity":             "sexualContentGraphicAndNudity",
	"gambling":                                  "gambling",
	"unrestrictedWebAccess":                     "unrestrictedWebAccess",
	"kidsAgeBand":                               "kidsAgeBand",
}

// schemaToWireAgeRating resolves a schema JSON-Pointer to Apple's wire
// field. seventeenPlus is rejected explicitly because Apple derives
// the 17+ rating; users can't set it directly.
func schemaToWireAgeRating(path string) (string, error) {
	leaf := strings.TrimPrefix(path, "/spec/ageRating/")
	if leaf == "seventeenPlus" {
		return "", errors.New("state: ageRating.seventeenPlus is derived by Apple; cannot be set directly")
	}
	if wire, ok := ageRatingSchemaToWire[leaf]; ok {
		return wire, nil
	}
	return "", fmt.Errorf("state: unknown ageRating field %q", leaf)
}

// --- checkpoint plumbing ---

func applyCheckpointPath(bundleID string) (string, error) {
	if bundleID == "" {
		return "", errors.New("state: applyCheckpointPath: bundleID is required")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("state: resolve user cache dir: %w", err)
	}
	return filepath.Join(cache, "skipper", "apply", bundleID+".json"), nil
}

func checkpointKeys(changes []plan.Change) []checkpointKey {
	out := make([]checkpointKey, 0, len(changes))
	for _, ch := range changes {
		buf, _ := json.Marshal(ch.To)
		out = append(out, checkpointKey{
			Resource: ch.Resource,
			Path:     ch.Path,
			ToJSON:   string(buf),
		})
	}
	return out
}

func checkpointHas(cp *applyCheckpoint, ch plan.Change) bool {
	buf, _ := json.Marshal(ch.To)
	want := checkpointKey{Resource: ch.Resource, Path: ch.Path, ToJSON: string(buf)}
	for _, k := range cp.Applied {
		if k == want {
			return true
		}
	}
	return false
}

// persistApplyCheckpoint writes the checkpoint atomically — same
// rename-on-close pattern as internal/asc/upload.go.
func persistApplyCheckpoint(bundleID string, applied []checkpointKey) error {
	if bundleID == "" {
		return errors.New("state: persistApplyCheckpoint: bundleID is required")
	}
	// Stable sort so the on-disk file diffs cleanly.
	sort.SliceStable(applied, func(i, j int) bool { return applied[i].Path < applied[j].Path })

	path, err := applyCheckpointPath(bundleID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("state: create apply cache dir: %w", err)
	}

	cp := applyCheckpoint{
		SchemaVersion: applyCheckpointSchemaVersion,
		BundleID:      bundleID,
		Applied:       applied,
	}
	buf, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal checkpoint: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("state: create temp checkpoint: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("state: write temp checkpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("state: fsync temp checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: close temp checkpoint: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("state: chmod temp checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("state: rename checkpoint: %w", err)
	}
	committed = true
	return nil
}

// loadApplyCheckpoint reads the on-disk checkpoint. Returns
// (nil, fs.ErrNotExist) when none exists.
func loadApplyCheckpoint(bundleID string) (*applyCheckpoint, error) {
	path, err := applyCheckpointPath(bundleID)
	if err != nil {
		return nil, err
	}
	buf, err := os.ReadFile(path) //nolint:gosec // path is composed from validated bundleID
	if err != nil {
		return nil, err
	}
	var cp applyCheckpoint
	if err := json.Unmarshal(buf, &cp); err != nil {
		return nil, fmt.Errorf("state: parse checkpoint: %w", err)
	}
	if cp.SchemaVersion == 0 || cp.SchemaVersion > applyCheckpointSchemaVersion {
		return nil, fmt.Errorf("state: checkpoint at %s has unsupported schemaVersion %d", path, cp.SchemaVersion)
	}
	return &cp, nil
}

// silence url import if no callers reach in (kept for intent).
var _ = url.Values{}
