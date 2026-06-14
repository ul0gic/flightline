package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/plan"
)

// ApplyContext carries the per-invocation app/version coordinates the
// dispatcher needs to resolve resource IDs. No package-level state.
type ApplyContext struct {
	BundleID string
	Version  string
	Platform string

	// StateDir resolves relative spec paths (screenshots, passwordFile).
	StateDir string
}

// ApplyOpts gates the apply orchestrator.
type ApplyOpts struct {
	Context ApplyContext

	// Confirm must be true for Apply to issue any write.
	Confirm bool
	// Resume skips changes already recorded in the checkpoint.
	Resume bool
	// DryRun resolves the dispatch path without issuing any write.
	DryRun bool
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

// checkpointKey keys a Change by (Resource, Path, JSON(To)) so a
// replayed change produces the same key for resume to skip.
type checkpointKey struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	ToJSON   string `json:"to"`
}

// Apply dispatches each change in order, checkpointing after every
// success; the first failure short-circuits and Resume continues it.
func Apply(ctx context.Context, c *asc.Client, changes []plan.Change, opts ApplyOpts) (*ApplyResult, error) {
	if c == nil {
		return nil, errors.New("state: Apply: client is nil")
	}
	if opts.Context.BundleID == "" {
		return nil, errors.New("state: Apply: opts.Context.BundleID is required (for checkpoint path)")
	}
	if !opts.Confirm && !opts.DryRun {
		return nil, errors.New("state: Apply: --confirm is required for non-dry-run writes")
	}
	bundleID := opts.Context.BundleID

	res := &ApplyResult{}
	var loaded *applyCheckpoint
	if opts.Resume && !opts.DryRun {
		cp, err := loadApplyCheckpoint(bundleID)
		switch {
		case err == nil:
			loaded = cp
		case errors.Is(err, os.ErrNotExist):
			// no checkpoint: first run, nothing to resume
		default:
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

		if err := dispatch(ctx, c, opts.Context, ch); err != nil {
			res.Errors = append(res.Errors, ChangeError{Change: ch, Err: err})
			prog(ch, "error")
			// Persist what we have before returning so resume works.
			_ = persistApplyCheckpoint(bundleID, append(checkpointKeys(res.Applied), checkpointKeys(res.Skipped)...))
			return res, &ChangeError{Change: ch, Err: err}
		}
		res.Applied = append(res.Applied, ch)
		prog(ch, "applied")

		// Persist after every success so a kill mid-loop loses at most one change.
		_ = persistApplyCheckpoint(bundleID, append(checkpointKeys(res.Applied), checkpointKeys(res.Skipped)...))
	}

	return res, nil
}

// dispatch routes a Change to its L1 write; returns ErrUnmappedChange for unknown paths.
func dispatch(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	for _, e := range dispatchTable {
		if e.match(ch.Path) {
			return e.fn(ctx, c, actx, ch)
		}
	}
	return errUnmapped(ch)
}

// dispatchEntry pairs a path predicate with its dispatcher. Table scanned in order; exact
// matches must precede sibling prefixes (e.g. usesNonExemptEncryption before declaration/).
type dispatchEntry struct {
	match func(string) bool
	fn    func(context.Context, *asc.Client, ApplyContext, plan.Change) error
}

func eq(want string) func(string) bool { return func(p string) bool { return p == want } }
func prefix(p string) func(string) bool {
	return func(s string) bool { return strings.HasPrefix(s, p) }
}
func anyOf(paths ...string) func(string) bool {
	return func(s string) bool {
		for _, p := range paths {
			if s == p {
				return true
			}
		}
		return false
	}
}

var dispatchTable = []dispatchEntry{
	{anyOf(
		"/spec/version/copyright",
		"/spec/version/releaseType",
		"/spec/version/earliestReleaseDate",
		"/spec/version/downloadable",
	), applyVersionField},
	{eq("/spec/build/number"), applyBuildAttach},
	{prefix("/spec/metadata/locales/"), applyMetadataField},
	{prefix("/spec/screenshots/locales/"), applyScreenshotSet},
	{prefix("/spec/iap/products/"), applyIAPField},
	{prefix("/spec/ageRating/"), applyAgeRatingField},
	{eq("/spec/exportCompliance/usesNonExemptEncryption"), applyEncryptionFlag},
	{prefix("/spec/exportCompliance/declaration/"), applyEncryptionDeclaration},
	{prefix("/spec/reviewerDemo/"), applyReviewerDemoField},
	{prefix("/spec/categories/"), applyCategoriesField},
	{prefix("/spec/pricing/"), applyPricingField},
	{prefix("/spec/testflight/groups/"), applyTestFlightField},
	{prefix("/spec/customProductPages/"), applyCustomProductPageField},
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

func applyVersionField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, platform)
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
	if _, err := asc.Patch[asc.Single[asc.VersionAttributes]](
		ctx, c, "/v1/appStoreVersions/"+versionID, nil, body,
	); err != nil {
		return fmt.Errorf("apply version %s: %w", ch.Path, err)
	}
	return nil
}

// applyAgeRatingField PATCHes one field on the ageRatingDeclaration.
func applyAgeRatingField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	appID, err := resolveAppID(ctx, c, actx.BundleID)
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
	if _, err := asc.Patch[asc.Single[asc.AgeRatingDeclarationAttributes]](
		ctx, c, "/v1/ageRatingDeclarations/"+declID, nil, body,
	); err != nil {
		return fmt.Errorf("apply ageRating %s: %w", wireKey, err)
	}
	return nil
}

// applyEncryptionFlag PATCHes the build's usesNonExemptEncryption.
func applyEncryptionFlag(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, platform)
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
	if _, err := asc.Patch[asc.Single[asc.BuildAttributes]](
		ctx, c, "/v1/builds/"+buildID, nil, body,
	); err != nil {
		return fmt.Errorf("apply exportCompliance: %w", err)
	}
	return nil
}

// ageRatingSchemaToWire maps schema leaf names to Apple's wire field names.
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

// schemaToWireAgeRating resolves a schema JSON-Pointer to Apple's wire field.
// seventeenPlus is rejected: Apple derives it; users cannot set it directly.
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

func applyCheckpointPath(bundleID string) (string, error) {
	if bundleID == "" {
		return "", errors.New("state: applyCheckpointPath: bundleID is required")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("state: resolve user cache dir: %w", err)
	}
	return filepath.Join(cache, "flightline", "apply", bundleID+".json"), nil
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

// persistApplyCheckpoint writes the checkpoint atomically via rename-on-close.
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

// loadApplyCheckpoint reads the on-disk checkpoint; returns (nil, fs.ErrNotExist) when absent.
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
