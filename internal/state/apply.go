package state

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
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
	// ResumeUploads reuses multipart upload checkpoints for asset changes.
	ResumeUploads bool
}

// ApplyOpts gates the apply orchestrator.
type ApplyOpts struct {
	Context ApplyContext

	// Confirm must be true for Apply to issue any write.
	Confirm bool
	// Resume validates the freshly fetched residual plan against the checkpoint.
	Resume bool
	// DryRun resolves the dispatch path without issuing any write.
	DryRun bool
	// Logger is called once per processed change. Nil disables.
	Logger func(c plan.Change, status string)
}

// ChangeError pairs a Change with the error its dispatch produced.
type ChangeError struct {
	Change  plan.Change `json:"change"`
	Message string      `json:"message"`
	Err     error       `json:"-"`
}

func newChangeError(change plan.Change, err error) ChangeError {
	message := ""
	if err != nil {
		message = asc.Redact(err.Error())
	}
	return ChangeError{Change: change, Message: message, Err: err}
}

// MessageText returns the stable, redacted user-facing error message.
func (e ChangeError) MessageText() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return asc.Redact(e.Err.Error())
	}
	return "change failed"
}

// Error implements error.
func (e *ChangeError) Error() string {
	return fmt.Sprintf("%s %s: %s", e.Change.Op, e.Change.Path, e.MessageText())
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
const applyCheckpointSchemaVersion = 2

// applyCheckpoint is the on-disk shape of an in-progress apply.
type applyCheckpoint struct {
	SchemaVersion int             `json:"schemaVersion"`
	BundleID      string          `json:"bundleId"`
	Version       string          `json:"version"`
	Platform      string          `json:"platform"`
	PlanDigest    string          `json:"planDigest"`
	Applied       []checkpointKey `json:"applied"`
}

// checkpointKey keys a Change by (Resource, Path, JSON(To)) so a
// freshly fetched residual plan can be bound to the original plan.
type checkpointKey struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	ToJSON   string `json:"to"`
}

// Apply dispatches each change in order, checkpointing after every success.
// Failures are collected while later independent changes are still attempted.
func Apply(ctx context.Context, c *asc.Client, changes []plan.Change, opts ApplyOpts) (*ApplyResult, error) {
	if c == nil {
		return nil, errors.New("state: Apply: client is nil")
	}
	if !opts.Confirm && !opts.DryRun {
		return nil, errors.New("state: Apply: --confirm is required for non-dry-run writes")
	}
	opts, cp, err := prepareApplyCheckpoint(opts, changes)
	if err != nil {
		return nil, err
	}
	res := &ApplyResult{}

	prog := opts.Logger
	if prog == nil {
		prog = func(plan.Change, string) {}
	}

	for _, ch := range changes {
		if opts.DryRun {
			res.Applied = append(res.Applied, ch)
			prog(ch, "dry-run")
			continue
		}

		// Continue past failures: one bad change must not strand the rest of the plan un-attempted.
		if err := dispatch(ctx, c, opts.Context, ch); err != nil {
			res.Errors = append(res.Errors, newChangeError(ch, err))
			prog(ch, "error")
			continue
		}
		res.Applied = append(res.Applied, ch)
		prog(ch, "applied")

		// Persist after every success so a kill mid-loop loses at most one change.
		cp.Applied = mergeCheckpointKeys(cp.Applied, checkpointKeys([]plan.Change{ch}))
		if err := persistApplyCheckpoint(opts.Context, cp); err != nil {
			return res, fmt.Errorf("state: change %s applied but checkpoint persistence failed: %w", ch.Path, err)
		}
	}

	if err := summarizeApplyErrors(res, len(changes)); err != nil {
		return res, err
	}
	if !opts.DryRun {
		if err := removeApplyCheckpoint(opts.Context); err != nil {
			return res, fmt.Errorf("state: apply succeeded but checkpoint cleanup failed: %w", err)
		}
	}
	return res, nil
}

func prepareApplyCheckpoint(opts ApplyOpts, changes []plan.Change) (ApplyOpts, applyCheckpoint, error) {
	actx, err := normalizeApplyContext(opts.Context)
	if err != nil {
		return opts, applyCheckpoint{}, err
	}
	opts.Context = actx
	opts.Context.ResumeUploads = opts.Resume
	digest, err := checkpointDigest(checkpointKeys(changes))
	if err != nil {
		return opts, applyCheckpoint{}, err
	}
	cp := applyCheckpoint{
		SchemaVersion: applyCheckpointSchemaVersion,
		BundleID:      actx.BundleID,
		Version:       actx.Version,
		Platform:      actx.Platform,
		PlanDigest:    digest,
	}
	loaded, err := resumeCheckpoint(opts, changes)
	if err != nil {
		return opts, applyCheckpoint{}, err
	}
	if loaded != nil {
		cp = *loaded
	}
	return opts, cp, nil
}

// resumeCheckpoint loads the prior checkpoint for --resume; a missing file is a clean first run.
func resumeCheckpoint(opts ApplyOpts, changes []plan.Change) (*applyCheckpoint, error) {
	if !opts.Resume || opts.DryRun {
		return nil, nil
	}
	cp, err := loadApplyCheckpoint(opts.Context)
	switch {
	case err == nil:
		current := checkpointKeys(changes)
		resumeDigest, digestErr := checkpointDigest(mergeCheckpointKeys(cp.Applied, current))
		if digestErr != nil {
			return nil, digestErr
		}
		if resumeDigest != cp.PlanDigest {
			return nil, errors.New("state: checkpoint plan does not match the current live diff; rerun without --resume")
		}
		return cp, nil
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	default:
		return nil, fmt.Errorf("state: load checkpoint: %w", err)
	}
}

func normalizeApplyContext(actx ApplyContext) (ApplyContext, error) {
	if actx.BundleID == "" {
		return actx, errors.New("state: Apply: opts.Context.BundleID is required")
	}
	if actx.Version == "" {
		return actx, errors.New("state: Apply: opts.Context.Version is required")
	}
	if actx.Platform == "" {
		actx.Platform = "IOS"
	}
	return actx, nil
}

func summarizeApplyErrors(res *ApplyResult, total int) error {
	if len(res.Errors) == 0 {
		return nil
	}
	errs := make([]error, 0, len(res.Errors))
	for i := range res.Errors {
		errs = append(errs, &res.Errors[i])
	}
	return fmt.Errorf("state: %d of %d changes failed; %d applied: %w",
		len(res.Errors), total, len(res.Applied), errors.Join(errs...))
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
	return fmt.Errorf("state: change at %s is not mapped to an L1 writer: %w",
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
	"gamblingSimulated":                         "gamblingSimulated",
	"gunsOrOtherWeapons":                        "gunsOrOtherWeapons",
	"advertising":                               "advertising",
	"ageAssurance":                              "ageAssurance",
	"healthOrWellnessTopics":                    "healthOrWellnessTopics",
	"lootBox":                                   "lootBox",
	"messagingAndChat":                          "messagingAndChat",
	"parentalControls":                          "parentalControls",
	"userGeneratedContent":                      "userGeneratedContent",
	"socialMedia":                               "socialMedia",
	"socialMediaAgeRestricted":                  "socialMediaAgeRestricted",
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

func applyCheckpointPath(actx ApplyContext) (string, error) {
	actx, err := normalizeApplyContext(actx)
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("state: resolve user cache dir: %w", err)
	}
	coordinate := actx.BundleID + "\x00" + actx.Version + "\x00" + actx.Platform
	hash := sha256.Sum256([]byte(coordinate))
	return filepath.Join(cache, "flightline", "apply", fmt.Sprintf("%x.json", hash)), nil
}

func checkpointKeys(changes []plan.Change) []checkpointKey {
	out := make([]checkpointKey, 0, len(changes))
	for _, ch := range changes {
		buf, _ := json.Marshal(ch.To)
		toJSON := string(buf)
		if identity := checkpointAssetIdentity(ch.To); identity != "" {
			toJSON += "\x00assets:" + identity
		}
		out = append(out, checkpointKey{
			Resource: ch.Resource,
			Path:     ch.Path,
			ToJSON:   toJSON,
		})
	}
	return out
}

func checkpointAssetIdentity(value any) string {
	var checksums []string
	switch target := value.(type) {
	case []config.ScreenshotFile:
		checksums = screenshotChecksums(target)
	case *config.IAPReviewScreenshot:
		if target != nil && target.SourceFileChecksum != "" {
			checksums = []string{target.SourceFileChecksum}
		}
	case config.IAPReviewScreenshot:
		if target.SourceFileChecksum != "" {
			checksums = []string{target.SourceFileChecksum}
		}
	case config.CustomProductPage:
		for _, locale := range target.Localizations {
			for _, files := range locale.Screenshots {
				checksums = append(checksums, screenshotChecksums(files)...)
			}
		}
	}
	if len(checksums) == 0 {
		return ""
	}
	sort.Strings(checksums)
	return strings.Join(checksums, ",")
}

func screenshotChecksums(files []config.ScreenshotFile) []string {
	checksums := make([]string, 0, len(files))
	for _, file := range files {
		if file.SourceFileChecksum == "" {
			return nil
		}
		checksums = append(checksums, file.SourceFileChecksum)
	}
	return checksums
}

func mergeCheckpointKeys(groups ...[]checkpointKey) []checkpointKey {
	seen := make(map[checkpointKey]struct{})
	out := make([]checkpointKey, 0)
	for _, group := range groups {
		for _, key := range group {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		return out[i].ToJSON < out[j].ToJSON
	})
	return out
}

func checkpointDigest(keys []checkpointKey) (string, error) {
	buf, err := json.Marshal(mergeCheckpointKeys(keys))
	if err != nil {
		return "", fmt.Errorf("state: marshal checkpoint plan: %w", err)
	}
	hash := sha256.Sum256(buf)
	return fmt.Sprintf("sha256:%x", hash), nil
}

// persistApplyCheckpoint writes the checkpoint atomically via rename-on-close.
func persistApplyCheckpoint(actx ApplyContext, cp applyCheckpoint) error {
	path, err := applyCheckpointPath(actx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("state: create apply cache dir: %w", err)
	}

	cp.Applied = mergeCheckpointKeys(cp.Applied)
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
func loadApplyCheckpoint(actx ApplyContext) (*applyCheckpoint, error) {
	path, err := applyCheckpointPath(actx)
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
	if cp.SchemaVersion != applyCheckpointSchemaVersion {
		return nil, fmt.Errorf("state: checkpoint at %s has unsupported schemaVersion %d", path, cp.SchemaVersion)
	}
	if cp.BundleID != actx.BundleID || cp.Version != actx.Version || cp.Platform != actx.Platform {
		return nil, errors.New("state: checkpoint coordinates do not match the requested apply")
	}
	if cp.PlanDigest == "" {
		return nil, fmt.Errorf("state: checkpoint at %s has no planDigest", path)
	}
	return &cp, nil
}

func removeApplyCheckpoint(actx ApplyContext) error {
	path, err := applyCheckpointPath(actx)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
