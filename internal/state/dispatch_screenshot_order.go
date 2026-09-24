package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// ValidateScreenshotOrderChange is pure validation for the opt-in complete
// screenshot relationship order written after its file collection reconcile.
func ValidateScreenshotOrderChange(ch plan.Change) error {
	target, err := screenshotOrderTargetForPath(ch.Path)
	if err != nil {
		return err
	}
	if target.locale == "" || target.deviceSet == "" || ch.Op != plan.OpUpdate {
		return errors.New("screenshot order requires an update for one locale and device set")
	}
	files, err := screenshotOrderFiles(ch.To)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("screenshot order requires a nonempty complete screenshot list")
	}
	_, err = screenshotOrderFileKeys(files)
	return err
}

// applyScreenshotOrderChange replaces only a set's appScreenshots relationship.
// It resolves target ownership from current ASC resources, maps each declared
// file to fresh membership, and refuses partial or ambiguous replacements.
func applyScreenshotOrderChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidateScreenshotOrderChange(ch); err != nil {
		return err
	}
	target, _ := screenshotOrderTargetForPath(ch.Path)
	files, err := screenshotOrderFiles(ch.To)
	if err != nil {
		return err
	}
	setID, err := resolveScreenshotOrderSet(ctx, c, actx, target)
	if err != nil {
		return err
	}
	members, err := asc.ListAppScreenshots(ctx, c, setID)
	if err != nil {
		return fmt.Errorf("apply screenshot order: read fresh membership: %w", err)
	}
	ids, err := screenshotOrderMemberIDs(files, members)
	if err != nil {
		return err
	}
	if sameAppScreenshotOrder(ids, members) {
		return nil
	}
	if err := asc.ReplaceAppScreenshotOrder(ctx, c, setID, ids); err != nil {
		return fmt.Errorf("apply screenshot order: %w", err)
	}
	return nil
}

type screenshotOrderTarget struct {
	locale, deviceSet, customProductPage string
}

func screenshotOrderTargetForPath(path string) (screenshotOrderTarget, error) {
	const mainPrefix = "/spec/screenshots/locales/"
	const cppPrefix = "/spec/customProductPages/"
	switch {
	case strings.HasPrefix(path, mainPrefix):
		return parseMainScreenshotOrderTarget(path, strings.TrimPrefix(path, mainPrefix))
	case strings.HasPrefix(path, cppPrefix):
		return parseCPPScreenshotOrderTarget(path, strings.TrimPrefix(path, cppPrefix))
	default:
		return screenshotOrderTarget{}, fmt.Errorf("screenshot order: unsupported path %q", path)
	}
}

func parseMainScreenshotOrderTarget(path, rest string) (screenshotOrderTarget, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] != "order" {
		return screenshotOrderTarget{}, fmt.Errorf("screenshot order: malformed main screenshot path %q", path)
	}
	return screenshotOrderTarget{locale: parts[0], deviceSet: parts[1]}, nil
}

func parseCPPScreenshotOrderTarget(path, rest string) (screenshotOrderTarget, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 6 || parts[0] == "" || parts[1] != "localizations" || parts[2] == "" || parts[3] != "screenshots" || parts[4] == "" || parts[5] != "order" {
		return screenshotOrderTarget{}, fmt.Errorf("screenshot order: malformed Custom Product Page path %q", path)
	}
	pageName, err := unescapeJSONPointerToken(parts[0])
	if err != nil || pageName == "" {
		return screenshotOrderTarget{}, fmt.Errorf("screenshot order: malformed Custom Product Page pointer %q", path)
	}
	return screenshotOrderTarget{customProductPage: pageName, locale: parts[2], deviceSet: parts[4]}, nil
}

func screenshotOrderFiles(value any) ([]config.ScreenshotFile, error) {
	files, ok := value.([]config.ScreenshotFile)
	if !ok {
		return nil, fmt.Errorf("screenshot order expects screenshot file list, got %T", value)
	}
	return files, nil
}

func resolveScreenshotOrderSet(ctx context.Context, c *asc.Client, actx ApplyContext, target screenshotOrderTarget) (string, error) {
	appID, err := resolveExactScreenshotOrderAppID(ctx, c, actx.BundleID)
	if err != nil {
		return "", err
	}
	if target.customProductPage != "" {
		return resolveCPPScreenshotOrderSet(ctx, c, appID, target)
	}
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	versionID, err := resolveExactScreenshotOrderVersion(ctx, c, appID, actx.Version, platform)
	if err != nil {
		return "", err
	}
	localizationID, err := resolveVersionLocalizationID(ctx, c, versionID, target.locale)
	if err != nil {
		return "", err
	}
	return resolveExistingScreenshotOrderSet(ctx, c, "appStoreVersionLocalizations", localizationID, target.deviceSet)
}

func resolveExactScreenshotOrderAppID(ctx context.Context, c *asc.Client, bundleID string) (string, error) {
	if isNumericAppID(bundleID) {
		app, err := asc.Get[asc.Single[appAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(bundleID), nil)
		if err != nil {
			return "", fmt.Errorf("apply screenshot order: read app %s: %w", bundleID, err)
		}
		if app.Data.ID != bundleID || app.Data.Type != "apps" {
			return "", fmt.Errorf("apply screenshot order: app %s returned mismatched identity", bundleID)
		}
		return bundleID, nil
	}
	id, err := uniqueAssetResource(ctx, c, "/v1/apps", url.Values{"filter[bundleId]": {bundleID}, "limit": {"50"}}, func(row asc.Resource[appAttributes]) bool {
		return row.Type == "apps" && row.Attributes.BundleID == bundleID
	})
	if err != nil {
		return "", fmt.Errorf("apply screenshot order: resolve app %s: %w", bundleID, err)
	}
	if id == "" {
		return "", fmt.Errorf("apply screenshot order: app with bundleId %q not found", bundleID)
	}
	return id, nil
}

func resolveExactScreenshotOrderVersion(ctx context.Context, c *asc.Client, appID, version, platform string) (string, error) {
	if version == "" {
		return "", errors.New("apply screenshot order: version is required for main screenshots")
	}
	path := "/v1/apps/" + url.PathEscape(appID) + "/appStoreVersions"
	query := url.Values{"filter[versionString]": {version}, "filter[platform]": {platform}, "limit": {"50"}}
	id, err := uniqueAssetResource(ctx, c, path, query, func(row asc.Resource[asc.VersionAttributes]) bool {
		return row.Type == "appStoreVersions" && row.Attributes.VersionString == version && row.Attributes.Platform == platform
	})
	if err != nil {
		return "", fmt.Errorf("apply screenshot order: resolve version: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("apply screenshot order: version %q not found on %s", version, platform)
	}
	return id, nil
}

func resolveCPPScreenshotOrderSet(ctx context.Context, c *asc.Client, appID string, target screenshotOrderTarget) (string, error) {
	pageID, err := resolveCustomProductPage(ctx, c, appID, target.customProductPage)
	if err != nil {
		return "", err
	}
	versionID, err := resolveExistingEditableCPPVersion(ctx, c, pageID)
	if err != nil {
		return "", err
	}
	localizationID, err := resolveExistingCPPLocalization(ctx, c, versionID, target.locale)
	if err != nil {
		return "", err
	}
	return resolveExistingScreenshotOrderSet(ctx, c, "appCustomProductPageLocalizations", localizationID, target.deviceSet)
}

func resolveExistingEditableCPPVersion(ctx context.Context, c *asc.Client, pageID string) (string, error) {
	path := "/v1/appCustomProductPages/" + url.PathEscape(pageID) + "/appCustomProductPageVersions"
	id, err := uniqueAssetResource(ctx, c, path, url.Values{"limit": {"50"}}, func(row asc.Resource[asc.AppCustomProductPageVersionAttributes]) bool {
		return row.Type == "appCustomProductPageVersions" && (row.Attributes.State == "PREPARE_FOR_SUBMISSION" || row.Attributes.State == "REJECTED")
	})
	if err != nil {
		return "", fmt.Errorf("apply screenshot order: resolve editable Custom Product Page version: %w", err)
	}
	if id == "" {
		return "", errors.New("apply screenshot order: Custom Product Page has no existing editable version")
	}
	return id, nil
}

func resolveExistingCPPLocalization(ctx context.Context, c *asc.Client, versionID, locale string) (string, error) {
	path := "/v1/appCustomProductPageVersions/" + url.PathEscape(versionID) + "/appCustomProductPageLocalizations"
	id, err := uniqueAssetResource(ctx, c, path, url.Values{"limit": {"50"}}, func(row asc.Resource[asc.AppCustomProductPageLocalizationAttributes]) bool {
		return row.Type == "appCustomProductPageLocalizations" && row.Attributes.Locale == locale
	})
	if err != nil {
		return "", fmt.Errorf("apply screenshot order: resolve Custom Product Page locale: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("apply screenshot order: Custom Product Page locale %q does not exist", locale)
	}
	return id, nil
}

func resolveExistingScreenshotOrderSet(ctx context.Context, c *asc.Client, parentType, parentID, deviceSet string) (string, error) {
	path := "/v1/" + parentType + "/" + url.PathEscape(parentID) + "/appScreenshotSets"
	id, err := uniqueAssetResource(ctx, c, path, url.Values{"filter[screenshotDisplayType]": {deviceSet}, "limit": {"50"}}, func(row asc.Resource[screenshotSetAttrs]) bool {
		return row.Type == "appScreenshotSets" && row.Attributes.ScreenshotDisplayType == deviceSet
	})
	if err != nil {
		return "", fmt.Errorf("apply screenshot order: resolve screenshot set: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("apply screenshot order: screenshot set %q does not exist", deviceSet)
	}
	return id, nil
}

func screenshotOrderMemberIDs(files []config.ScreenshotFile, members []asc.AppScreenshot) ([]string, error) {
	keys, err := screenshotOrderFileKeys(files)
	if err != nil {
		return nil, err
	}
	if len(keys) != len(members) {
		return nil, fmt.Errorf("apply screenshot order: declared %d files but fresh membership has %d screenshots", len(keys), len(members))
	}

	ids := make([]string, 0, len(keys))
	used := make(map[string]struct{}, len(members))
	for _, key := range keys {
		id, err := uniqueScreenshotOrderMember(key, members)
		if err != nil {
			return nil, err
		}
		if _, duplicate := used[id]; duplicate {
			return nil, fmt.Errorf("apply screenshot order: declared files resolve to duplicate screenshot %q", id)
		}
		used[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(used) != len(members) {
		return nil, errors.New("apply screenshot order: declared files are not a complete fresh membership")
	}
	return ids, nil
}

type screenshotOrderFileKey struct {
	checksum string
	filename string
}

func screenshotOrderFileKeys(files []config.ScreenshotFile) ([]screenshotOrderFileKey, error) {
	keys := make([]screenshotOrderFileKey, 0, len(files))
	for _, file := range files {
		checksum, err := screenshotOrderChecksum(file)
		if err != nil {
			return nil, err
		}
		filename := screenshotOrderFilename(file.Path)
		if checksum == "" && filename == "" {
			return nil, errors.New("screenshot order: file path is required when checksum is unknown")
		}
		keys = append(keys, screenshotOrderFileKey{checksum: checksum, filename: filename})
	}
	return keys, nil
}

func screenshotOrderChecksum(file config.ScreenshotFile) (string, error) {
	if file.SourceFileChecksum != "" {
		return file.SourceFileChecksum, nil
	}
	if file.Alt == nil || !strings.HasPrefix(*file.Alt, "checksum:") {
		return "", nil
	}
	checksum := strings.TrimPrefix(*file.Alt, "checksum:")
	if checksum == "" {
		return "", errors.New("screenshot order: checksum alt value cannot be empty")
	}
	return checksum, nil
}

func screenshotOrderFilename(path string) string {
	path = strings.TrimSpace(path)
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func uniqueScreenshotOrderMember(key screenshotOrderFileKey, members []asc.AppScreenshot) (string, error) {
	id := ""
	for _, member := range members {
		matches := member.FileName == key.filename
		if key.checksum != "" {
			matches = member.SourceFileChecksum == key.checksum
		}
		if !matches {
			continue
		}
		if id != "" {
			return "", errors.New("apply screenshot order: declared file matches multiple fresh screenshots")
		}
		id = member.ID
	}
	if id == "" {
		return "", errors.New("apply screenshot order: declared file is absent from fresh membership")
	}
	return id, nil
}

func sameAppScreenshotOrder(ids []string, members []asc.AppScreenshot) bool {
	if len(ids) != len(members) {
		return false
	}
	for i := range ids {
		if ids[i] != members[i].ID {
			return false
		}
	}
	return true
}
