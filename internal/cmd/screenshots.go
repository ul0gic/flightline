package cmd

import (
	"context"
	"crypto/md5" //nolint:gosec // Apple's API requires MD5 for upload integrity (sourceFileChecksum)
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// screenshots upload is the cmd-layer wrapper around (*asc.Client).Upload.
//
// Wire dance:
//   1. resolve bundle -> app -> version -> versionLocalization (locale)
//   2. find or create the appScreenshotSet for (versionLocalization,
//      screenshotDisplayType)
//   3. enumerate the set's existing appScreenshots; index by
//      sourceFileChecksum
//   4. for each local file, compute md5; if a slot already carries that
//      checksum, skip (idempotent contract); else hand the file to
//      Client.Upload which performs reserve -> PUT chunks -> commit.
//
// Re-running with the same arguments is a no-op: every file is already in
// place at its checksum, so step 4 short-circuits for all files.
//
// Files supplied without an explicit display type are assumed to belong to
// --device-set; the spec says one device set per invocation, which keeps
// the contract small and predictable. Multi-set uploads are a v2 problem.

// validScreenshotDeviceSets is the canonical list of ScreenshotDisplayType
// enum values pulled from openapi.oas.json. Surfaced as the --device-set
// validator. Renaming or removing entries is a breaking change to the
// flag contract.
var validScreenshotDeviceSets = []string{
	"APP_IPHONE_67", "APP_IPHONE_61", "APP_IPHONE_65", "APP_IPHONE_58",
	"APP_IPHONE_55", "APP_IPHONE_47", "APP_IPHONE_40", "APP_IPHONE_35",
	"APP_IPAD_PRO_3GEN_129", "APP_IPAD_PRO_3GEN_11", "APP_IPAD_PRO_129",
	"APP_IPAD_105", "APP_IPAD_97",
	"APP_DESKTOP",
	"APP_WATCH_ULTRA", "APP_WATCH_SERIES_10", "APP_WATCH_SERIES_7",
	"APP_WATCH_SERIES_4", "APP_WATCH_SERIES_3",
	"APP_APPLE_TV", "APP_APPLE_VISION_PRO",
	"IMESSAGE_APP_IPHONE_67", "IMESSAGE_APP_IPHONE_61",
	"IMESSAGE_APP_IPHONE_65", "IMESSAGE_APP_IPHONE_58",
	"IMESSAGE_APP_IPHONE_55", "IMESSAGE_APP_IPHONE_47",
	"IMESSAGE_APP_IPHONE_40", "IMESSAGE_APP_IPAD_PRO_3GEN_129",
	"IMESSAGE_APP_IPAD_PRO_3GEN_11", "IMESSAGE_APP_IPAD_PRO_129",
	"IMESSAGE_APP_IPAD_105", "IMESSAGE_APP_IPAD_97",
}

// isValidDeviceSet reports whether the given string is one of Apple's
// recognised ScreenshotDisplayType values.
func isValidDeviceSet(s string) bool {
	for _, v := range validScreenshotDeviceSets {
		if v == s {
			return true
		}
	}
	return false
}

// screenshotSetAttrs mirrors AppScreenshotSet.attributes — only the field
// Flightline reads. Type is the Apple-side display type enum.
type screenshotSetAttrs struct {
	ScreenshotDisplayType string `json:"screenshotDisplayType,omitempty"`
}

// existingScreenshotAttrs mirrors AppScreenshot.attributes — only the
// fields Flightline reads to decide whether a file is already uploaded.
type existingScreenshotAttrs struct {
	FileName           string `json:"fileName,omitempty"`
	SourceFileChecksum string `json:"sourceFileChecksum,omitempty"`
	FileSize           int64  `json:"fileSize,omitempty"`
}

// ScreenshotUploadResultEntry describes the per-file outcome inside a
// `screenshots upload` invocation. Stable JSON contract.
type ScreenshotUploadResultEntry struct {
	Path     string `json:"path"`
	FileName string `json:"fileName"`
	MD5      string `json:"md5"`
	Action   string `json:"action"` // "uploaded" | "skipped"
	AssetID  string `json:"assetId,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// ScreenshotUploadResult is the JSON-stable envelope for `screenshots upload`.
type ScreenshotUploadResult struct {
	Action        string                        `json:"action"` // "noop" | "uploaded"
	Changed       bool                          `json:"changed"`
	BundleID      string                        `json:"bundleId"`
	Version       string                        `json:"version"`
	Locale        string                        `json:"locale"`
	DeviceSet     string                        `json:"deviceSet"`
	SetID         string                        `json:"setId"`
	UploadedCount int                           `json:"uploadedCount"`
	SkippedCount  int                           `json:"skippedCount"`
	Files         []ScreenshotUploadResultEntry `json:"files"`
}

// TableRows for ScreenshotUploadResult — header summary + per-file rows.
func (r *ScreenshotUploadResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FILE", "ACTION", "MD5", "ASSET_ID"}
	rows = make([][]string, 0, len(r.Files))
	for _, e := range r.Files {
		rows = append(rows, []string{e.FileName, e.Action, e.MD5, e.AssetID})
	}
	return headers, rows
}

var screenshotsCmd = &cobra.Command{
	Use:   "screenshots",
	Short: "Manage App Store screenshots",
	Long: `screenshots wraps Apple's appScreenshotSets / appScreenshots resources.
Uploads use the 3-step reserve -> PUT chunks -> commit dance via the
shared internal/asc/upload.go helper. All operations are idempotent — a
file whose MD5 already matches a slot in the target set is skipped.`,
}

var screenshotsUploadCmd = &cobra.Command{
	Use:          "upload <bundleId> [files...]",
	Short:        "Upload screenshots to a version localization (idempotent: skips files already at target by MD5)",
	SilenceUsage: true,
	Args:         cobra.MinimumNArgs(2),
	RunE:         runScreenshotsUpload,
	Example: `  fline screenshots upload com.example.myapp --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 ./shots/iphone-67/*.png
  fline screenshots upload com.example.myapp --version 1.0.1 --locale en-US --device-set APP_IPAD_PRO_3GEN_129 ./shots/ipad/01.png ./shots/ipad/02.png
  fline screenshots upload com.example.myapp --version 1.0.1 --locale en-US --device-set APP_IPHONE_67 --resume ./shots/01.png`,
}

var (
	screenshotsUploadVersion   string
	screenshotsUploadPlatform  string
	screenshotsUploadLocale    string
	screenshotsUploadDeviceSet string
	screenshotsUploadResume    bool
)

func init() {
	screenshotsUploadCmd.Flags().StringVar(&screenshotsUploadVersion, "version", "", "App Store version string (e.g. 1.0.1)")
	screenshotsUploadCmd.Flags().StringVar(&screenshotsUploadPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	screenshotsUploadCmd.Flags().StringVar(&screenshotsUploadLocale, "locale", "", "BCP-47 locale code (e.g. en-US)")
	screenshotsUploadCmd.Flags().StringVar(&screenshotsUploadDeviceSet, "device-set", "", "ScreenshotDisplayType (e.g. APP_IPHONE_67)")
	screenshotsUploadCmd.Flags().BoolVar(&screenshotsUploadResume, "resume", false, "resume from on-disk upload checkpoint if present")
	_ = screenshotsUploadCmd.MarkFlagRequired("version")
	_ = screenshotsUploadCmd.MarkFlagRequired("locale")
	_ = screenshotsUploadCmd.MarkFlagRequired("device-set")

	screenshotsCmd.AddCommand(screenshotsUploadCmd)
	rootCmd.AddCommand(screenshotsCmd)
}

// screenshotsUploadInput is the validated, normalized form of the flag
// state plus positional args. Pulled out so the run loop reads top-down.
type screenshotsUploadInput struct {
	bundleID, versionStr, platform, locale, deviceSet string
	files                                             []string
}

// validateScreenshotsUploadFlags collapses every input-shape check into
// one place. Returns a typed error on the first failure.
func validateScreenshotsUploadFlags(args []string) (screenshotsUploadInput, error) {
	in := screenshotsUploadInput{
		bundleID:   args[0],
		files:      args[1:],
		versionStr: strings.TrimSpace(screenshotsUploadVersion),
		platform:   strings.TrimSpace(screenshotsUploadPlatform),
		locale:     strings.TrimSpace(screenshotsUploadLocale),
		deviceSet:  strings.TrimSpace(screenshotsUploadDeviceSet),
	}
	if in.versionStr == "" {
		return in, fmt.Errorf("screenshots: --version is required")
	}
	if in.locale == "" {
		return in, fmt.Errorf("screenshots: --locale is required")
	}
	if !isValidDeviceSet(in.deviceSet) {
		return in, fmt.Errorf("screenshots: --device-set %q is not a recognised ScreenshotDisplayType (see `fline screenshots upload --help`)", in.deviceSet)
	}
	if len(in.files) == 0 {
		return in, fmt.Errorf("screenshots: at least one file path is required")
	}
	for _, p := range in.files {
		if err := validateScreenshotFile(p); err != nil {
			return in, err
		}
	}
	return in, nil
}

// resolveScreenshotTarget walks bundleId -> appId -> versionId ->
// versionLocId -> setId for the upload destination. Centralized so the
// runner stays linear.
func resolveScreenshotTarget(ctx context.Context, c *asc.Client, in screenshotsUploadInput) (string, error) {
	appID, err := resolveAppID(ctx, c, in.bundleID)
	if err != nil {
		return "", err
	}
	versionView, err := lookupVersion(ctx, c, appID, in.versionStr, in.platform)
	if err != nil {
		return "", err
	}
	if versionView == nil {
		return "", fmt.Errorf("screenshots: no version %q found for %q (platform=%s)", in.versionStr, in.bundleID, in.platform)
	}
	versionLocID, _, err := getVersionLocalization(ctx, c, versionView.ID, in.locale)
	if err != nil {
		return "", err
	}
	if versionLocID == "" {
		return "", fmt.Errorf("screenshots: no appStoreVersionLocalization for locale %q under version %s; create it via the locale picker first", in.locale, in.versionStr)
	}
	return findOrCreateScreenshotSet(ctx, c, versionLocID, in.deviceSet)
}

// uploadOrSkipFiles iterates the local files and runs each one through
// processOneScreenshot.
func uploadOrSkipFiles(
	ctx context.Context,
	c *asc.Client,
	setID string,
	files []string,
	existing map[string]existingScreenshotEntry,
) (entries []ScreenshotUploadResultEntry, uploaded, skipped int, err error) {
	entries = make([]ScreenshotUploadResultEntry, 0, len(files))
	for _, p := range files {
		var entry ScreenshotUploadResultEntry
		entry, err = processOneScreenshot(ctx, c, setID, p, existing)
		if err != nil {
			return nil, 0, 0, err
		}
		switch entry.Action {
		case "uploaded":
			uploaded++
		case "skipped":
			skipped++
		}
		entries = append(entries, entry)
	}
	return entries, uploaded, skipped, nil
}

func runScreenshotsUpload(cmd *cobra.Command, args []string) error {
	in, err := validateScreenshotsUploadFlags(args)
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	setID, err := resolveScreenshotTarget(cmd.Context(), c, in)
	if err != nil {
		return err
	}

	existing, err := listScreenshotsByChecksum(cmd.Context(), c, setID)
	if err != nil {
		return err
	}
	entries, uploadedCount, skippedCount, err := uploadOrSkipFiles(cmd.Context(), c, setID, in.files, existing)
	if err != nil {
		return err
	}

	result := &ScreenshotUploadResult{
		Action:        actionForUpload(uploadedCount),
		Changed:       uploadedCount > 0,
		BundleID:      in.bundleID,
		Version:       in.versionStr,
		Locale:        in.locale,
		DeviceSet:     in.deviceSet,
		SetID:         setID,
		UploadedCount: uploadedCount,
		SkippedCount:  skippedCount,
		Files:         entries,
	}
	return Render(result, outputMode())
}

// actionForUpload picks the result-envelope action based on whether any
// file was uploaded.
func actionForUpload(uploadedCount int) string {
	if uploadedCount == 0 {
		return "noop"
	}
	return "uploaded"
}

// validateScreenshotFile rejects files that don't exist, aren't regular,
// or are zero-length. Catches typos and path-traversal-shaped inputs
// before we hash them.
func validateScreenshotFile(path string) error {
	if path == "" {
		return fmt.Errorf("screenshots: empty file path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("screenshots: stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("screenshots: %s: not a regular file", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("screenshots: %s: zero-length file", path)
	}
	return nil
}

// processOneScreenshot hashes the file, looks for an existing slot at the
// same checksum, and either skips (idempotent path) or hands off to
// Client.Upload (the 3-step dance from internal/asc/upload.go).
func processOneScreenshot(
	ctx context.Context,
	c *asc.Client,
	setID string,
	path string,
	existing map[string]existingScreenshotEntry,
) (ScreenshotUploadResultEntry, error) {
	md5Hex, err := md5HexOfFile(path)
	if err != nil {
		return ScreenshotUploadResultEntry{}, err
	}
	fileName := basename(path)
	entry := ScreenshotUploadResultEntry{Path: path, FileName: fileName, MD5: md5Hex}

	if got, ok := existing[md5Hex]; ok {
		entry.Action = "skipped"
		entry.AssetID = got.assetID
		entry.Reason = "checksum already present in target set"
		return entry, nil
	}

	out, err := c.Upload(ctx, asc.UploadOptions{
		Kind:     asc.AssetKindAppScreenshot,
		ParentID: setID,
		Asset: asc.UploadAsset{
			Path:     path,
			FileName: fileName,
		},
		ResumeFromCheckpoint: screenshotsUploadResume,
	})
	if err != nil {
		return ScreenshotUploadResultEntry{}, fmt.Errorf("screenshots: upload %s: %w", path, err)
	}
	entry.Action = "uploaded"
	entry.AssetID = out.ID
	return entry, nil
}

// existingScreenshotEntry holds the resource identifiers that pair with a
// known sourceFileChecksum.
type existingScreenshotEntry struct {
	assetID  string
	fileName string
}

// listScreenshotsByChecksum returns a checksum -> entry map for every
// appScreenshot under setID. Used as the idempotency lookup table.
func listScreenshotsByChecksum(ctx context.Context, c *asc.Client, setID string) (map[string]existingScreenshotEntry, error) {
	out := make(map[string]existingScreenshotEntry)
	q := url.Values{"limit": {"200"}}
	for page, err := range asc.Pages[existingScreenshotAttrs](
		ctx, c, "/v1/appScreenshotSets/"+url.PathEscape(setID)+"/appScreenshots", q,
	) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			if r.Attributes.SourceFileChecksum == "" {
				// Skip in-flight slots that haven't committed yet — those
				// shouldn't match a local checksum, and treating them as
				// matches would silently drop a re-upload of the right file.
				continue
			}
			out[r.Attributes.SourceFileChecksum] = existingScreenshotEntry{
				assetID:  r.ID,
				fileName: r.Attributes.FileName,
			}
		}
	}
	return out, nil
}

// findOrCreateScreenshotSet GETs the set with screenshotDisplayType=
// deviceSet under versionLocID. If absent, POSTs a new one and returns
// its ID. Idempotent by Apple's contract: there is at most one set per
// (localization, displayType).
func findOrCreateScreenshotSet(ctx context.Context, c *asc.Client, versionLocID, deviceSet string) (string, error) {
	q := url.Values{
		"filter[screenshotDisplayType]": {deviceSet},
		"limit":                         {"1"},
	}
	page, err := asc.Get[asc.Collection[screenshotSetAttrs]](
		ctx, c, "/v1/appStoreVersionLocalizations/"+url.PathEscape(versionLocID)+"/appScreenshotSets", q,
	)
	if err != nil {
		return "", err
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	body := screenshotSetCreateRequest{
		Data: screenshotSetCreateData{
			Type: "appScreenshotSets",
			Attributes: screenshotSetCreateAttributes{
				ScreenshotDisplayType: deviceSet,
			},
			Relationships: screenshotSetCreateRels{
				AppStoreVersionLocalization: screenshotSetCreateRel{
					Data: screenshotSetCreateRelRef{
						Type: "appStoreVersionLocalizations",
						ID:   versionLocID,
					},
				},
			},
		},
	}
	resp, err := asc.Post[asc.Single[screenshotSetAttrs]](
		ctx, c, "/v1/appScreenshotSets", nil, body,
	)
	if err != nil {
		return "", err
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("screenshots: create appScreenshotSet returned empty id")
	}
	return resp.Data.ID, nil
}

// screenshotSetCreateRequest mirrors Apple's
// AppScreenshotSetCreateRequest. Built by hand because the surface is
// small and lives only here.
type screenshotSetCreateRequest struct {
	Data screenshotSetCreateData `json:"data"`
}

type screenshotSetCreateData struct {
	Type          string                        `json:"type"`
	Attributes    screenshotSetCreateAttributes `json:"attributes"`
	Relationships screenshotSetCreateRels       `json:"relationships"`
}

type screenshotSetCreateAttributes struct {
	ScreenshotDisplayType string `json:"screenshotDisplayType"`
}

type screenshotSetCreateRels struct {
	AppStoreVersionLocalization screenshotSetCreateRel `json:"appStoreVersionLocalization"`
}

type screenshotSetCreateRel struct {
	Data screenshotSetCreateRelRef `json:"data"`
}

type screenshotSetCreateRelRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// md5HexOfFile streams the file through md5 and returns a lowercase hex
// digest. Apple requires MD5 for sourceFileChecksum — we re-implement it
// here rather than reaching into asc.computeFileMD5 (unexported) because
// this layer also computes MD5 for the idempotency probe before any
// Upload call lands.
func md5HexOfFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path validated by validateScreenshotFile
	if err != nil {
		return "", fmt.Errorf("screenshots: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := md5.New() //nolint:gosec // Apple's API contract requires MD5 for upload integrity
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("screenshots: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// basename trims the directory portion of a path. Apple's fileName is
// display-only; we don't preserve directory structure.
func basename(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// validDeviceSetsSorted returns the canonical enum slice in deterministic
// order for tests / completion functions.
func validDeviceSetsSorted() []string {
	out := make([]string, len(validScreenshotDeviceSets))
	copy(out, validScreenshotDeviceSets)
	sort.Strings(out)
	return out
}
