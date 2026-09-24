package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

type previewChangeTarget struct {
	page, locale, previewType string
	cpp                       bool
}

func ValidatePreviewChange(ch plan.Change) error {
	target, err := parsePreviewChangePath(ch.Path)
	if err != nil {
		return errUnmapped(ch)
	}
	if ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate {
		return fmt.Errorf("preview set does not support %s", ch.Op)
	}
	files, ok := ch.To.([]config.PreviewFile)
	if !ok {
		return errors.New("preview target must be a complete []PreviewFile")
	}
	if diagnostics := config.ValidatePreviewsIntent("", previewValidationState(target, files), nil); len(diagnostics) != 0 {
		return errors.New(diagnostics[0].Message)
	}
	if ch.From != nil {
		if _, ok := ch.From.([]config.PreviewFile); !ok {
			return errors.New("preview expected prior value has wrong type")
		}
	}
	return nil
}

func previewValidationState(target previewChangeTarget, files []config.PreviewFile) *config.State {
	state := &config.State{}
	if target.cpp {
		state.Spec.CustomProductPages = &config.CustomProductPagesSpec{target.page: {Localizations: map[string]config.CustomProductPageLocale{target.locale: {Previews: map[string][]config.PreviewFile{target.previewType: files}}}}}
	} else {
		state.Spec.Previews = &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{target.locale: {target.previewType: files}}}
	}
	return state
}

func parsePreviewChangePath(path string) (previewChangeTarget, error) {
	if strings.HasPrefix(path, "/spec/previews/locales/") {
		return parseMainPreviewPath(strings.TrimPrefix(path, "/spec/previews/locales/"))
	}
	if strings.HasPrefix(path, "/spec/customProductPages/") {
		return parseCPPPreviewPath(strings.TrimPrefix(path, "/spec/customProductPages/"))
	}
	return previewChangeTarget{}, errors.New("not a preview path")
}

func parseMainPreviewPath(rest string) (previewChangeTarget, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || !asc.ValidPreviewType(parts[1]) {
		return previewChangeTarget{}, errors.New("invalid main preview path")
	}
	locale, err := unescapeTestFlightPathSegment(parts[0])
	if err != nil || locale == "" {
		return previewChangeTarget{}, errors.New("invalid main preview locale")
	}
	return previewChangeTarget{locale: locale, previewType: parts[1]}, nil
}

func parseCPPPreviewPath(rest string) (previewChangeTarget, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 5 || parts[0] == "" || parts[1] != "localizations" || parts[2] == "" || parts[3] != "previews" || !asc.ValidPreviewType(parts[4]) {
		return previewChangeTarget{}, errors.New("invalid CPP preview path")
	}
	page, pageErr := unescapeTestFlightPathSegment(parts[0])
	locale, localeErr := unescapeTestFlightPathSegment(parts[2])
	if pageErr != nil || localeErr != nil || page == "" || locale == "" {
		return previewChangeTarget{}, errors.New("invalid CPP preview page or locale")
	}
	return previewChangeTarget{page: page, locale: locale, previewType: parts[4], cpp: true}, nil
}

type preparedPreview struct {
	file           config.PreviewFile
	path, checksum string
	existingID     string
}

func applyPreviewChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidatePreviewChange(ch); err != nil {
		return err
	}
	target, _ := parsePreviewChangePath(ch.Path)
	wanted, ok := ch.To.([]config.PreviewFile)
	if !ok {
		return errors.New("preview target has invalid type")
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	parent, cppPageID, err := resolvePreviewChangeParent(ctx, c, appID, actx, target)
	if err != nil {
		return err
	}
	setID, current, err := readCurrentPreviewSet(ctx, c, parent, target.previewType)
	if err != nil {
		return err
	}
	if err := verifyPreviewObserved(ch.From, current); err != nil {
		return err
	}
	prepared, stale, err := preparePreviewMutation(actx.StateDir, wanted, current)
	if err != nil {
		return err
	}
	if setID == "" && len(prepared) == 0 {
		return nil
	}
	setID, err = ensurePreviewWriteSet(ctx, c, parent, cppPageID, target, setID)
	if err != nil {
		return err
	}
	for index := range prepared {
		if err := applyPreparedPreview(ctx, c, actx.ResumeUploads, setID, &prepared[index]); err != nil {
			return err
		}
	}
	for _, id := range stale {
		if err := asc.DeleteAppPreview(ctx, c, setID, id); err != nil {
			return err
		}
	}
	return nil
}

func ensurePreviewWriteSet(ctx context.Context, c *asc.Client, parent asc.PreviewParent, cppPageID string, target previewChangeTarget, setID string) (string, error) {
	if setID != "" {
		return setID, nil
	}
	if parent.ID == "" {
		id, err := createCPPPreviewParent(ctx, c, cppPageID, target.locale)
		if err != nil {
			return "", err
		}
		parent.ID = id
	}
	set, _, err := asc.FindOrCreateAppPreviewSet(ctx, c, parent, target.previewType)
	if err != nil {
		return "", err
	}
	return set.ID, nil
}

func readCurrentPreviewSet(ctx context.Context, c *asc.Client, parent asc.PreviewParent, previewType string) (string, []asc.AppPreview, error) {
	if parent.ID == "" {
		return "", nil, nil
	}
	sets, err := asc.ListAppPreviewSets(ctx, c, parent)
	if err != nil {
		return "", nil, err
	}
	setID := ""
	for index := range sets {
		if sets[index].PreviewType != previewType {
			continue
		}
		if setID != "" {
			return "", nil, fmt.Errorf("duplicate preview sets for %s", previewType)
		}
		setID = sets[index].ID
	}
	if setID == "" {
		return "", nil, nil
	}
	current, err := asc.ListAppPreviews(ctx, c, setID)
	if err != nil {
		return "", nil, err
	}
	for index := range current {
		if current[index].VideoDeliveryState.State != "COMPLETE" {
			return "", nil, fmt.Errorf("preview %s is %q; inspect or wait by ID before apply", current[index].ID, current[index].VideoDeliveryState.State)
		}
	}
	return setID, current, nil
}

func createCPPPreviewParent(ctx context.Context, c *asc.Client, pageID, locale string) (string, error) {
	versionID, err := ensureEditableCPPVersion(ctx, c, pageID)
	if err != nil {
		return "", err
	}
	return ensureCPPLocalization(ctx, c, versionID, locale, nil)
}

func applyPreparedPreview(ctx context.Context, c *asc.Client, resume bool, setID string, item *preparedPreview) error {
	id := item.existingID
	options := asc.AssetPollOptions{Interval: 3 * time.Second, MaxAttempts: 20}
	if id == "" {
		result, _, err := asc.UploadAppPreviewAndWait(ctx, c, setID, item.path, resume, options)
		if err != nil {
			return fmt.Errorf("upload preview %s: %w", item.file.Path, err)
		}
		id = result.ID
	}
	if item.file.PreviewFrameTimeCode == nil {
		return nil
	}
	if _, err := asc.SetAppPreviewFrame(ctx, c, id, *item.file.PreviewFrameTimeCode); err != nil {
		return err
	}
	_, err := asc.WaitAppPreviewProcessing(ctx, c, id, options)
	return err
}

func resolvePreviewChangeParent(ctx context.Context, c *asc.Client, appID string, actx ApplyContext, target previewChangeTarget) (asc.PreviewParent, string, error) {
	if !target.cpp {
		_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, actx.Platform)
		if err != nil {
			return asc.PreviewParent{}, "", err
		}
		id, err := resolveVersionLocalizationID(ctx, c, versionID, target.locale)
		return asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: id}, "", err
	}
	pageID, err := resolveCustomProductPage(ctx, c, appID, target.page)
	if err != nil {
		return asc.PreviewParent{}, "", err
	}
	versionID, err := uniqueAssetResource(ctx, c, "/v1/appCustomProductPages/"+url.PathEscape(pageID)+"/appCustomProductPageVersions", url.Values{"limit": {"50"}}, func(row asc.Resource[asc.AppCustomProductPageVersionAttributes]) bool {
		return row.Attributes.State == "PREPARE_FOR_SUBMISSION" || row.Attributes.State == "REJECTED"
	})
	if err != nil {
		return asc.PreviewParent{}, "", err
	}
	parent := asc.PreviewParent{Type: "appCustomProductPageLocalizations"}
	if versionID == "" {
		return parent, pageID, nil
	}
	parent.ID, err = uniqueAssetResource(ctx, c, "/v1/appCustomProductPageVersions/"+url.PathEscape(versionID)+"/appCustomProductPageLocalizations", url.Values{"limit": {"50"}}, func(row asc.Resource[asc.AppCustomProductPageLocalizationAttributes]) bool {
		return row.Attributes.Locale == target.locale
	})
	if err != nil {
		return asc.PreviewParent{}, "", err
	}
	return parent, pageID, nil
}

func verifyPreviewObserved(before any, current []asc.AppPreview) error {
	if before == nil {
		if len(current) != 0 {
			return errors.New("preview set changed since planning; refetch before apply")
		}
		return nil
	}
	observed, ok := before.([]config.PreviewFile)
	if !ok {
		return errors.New("preview expected prior value has wrong type")
	}
	if len(observed) != len(current) {
		return errors.New("preview set changed since planning; refetch before apply")
	}
	used := make([]bool, len(current))
	for index := range observed {
		found := false
		for old := range current {
			if used[old] || !previewFileMatchesAsset(observed[index], current[old]) || !samePreviewFrame(observed[index].PreviewFrameTimeCode, current[old].PreviewFrameTimeCode) {
				continue
			}
			used[old], found = true, true
			break
		}
		if !found {
			return errors.New("preview set changed since planning; refetch before apply")
		}
	}
	return nil
}

func preparePreviewMutation(stateDir string, wanted []config.PreviewFile, current []asc.AppPreview) ([]preparedPreview, []string, error) {
	used := make([]bool, len(current))
	prepared := make([]preparedPreview, 0, len(wanted))
	for index := range wanted {
		file := wanted[index]
		match, err := matchPreviewAsset(file, current, used)
		if err != nil {
			return nil, nil, err
		}
		item := preparedPreview{file: file}
		if match >= 0 {
			used[match] = true
			item.existingID = current[match].ID
		} else {
			path, checksum, err := preparePreviewSource(stateDir, file)
			if err != nil {
				return nil, nil, err
			}
			item.path, item.checksum = path, checksum
		}
		prepared = append(prepared, item)
	}
	stale := make([]string, 0)
	for index := range current {
		if !used[index] {
			stale = append(stale, current[index].ID)
		}
	}
	return prepared, stale, nil
}

func matchPreviewAsset(file config.PreviewFile, current []asc.AppPreview, used []bool) (int, error) {
	match := -1
	for old := range current {
		if used[old] || !previewFileMatchesAsset(file, current[old]) {
			continue
		}
		if match >= 0 {
			return -1, fmt.Errorf("ambiguous existing preview identity %s", file.Path)
		}
		match = old
	}
	return match, nil
}

func preparePreviewSource(stateDir string, file config.PreviewFile) (path, checksum string, err error) {
	path, err = resolveAssetPath(stateDir, file.Path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("stat preview source %s: %w", file.Path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", "", fmt.Errorf("preview source %s must be a nonempty regular file", file.Path)
	}
	checksum, err = fileMD5(path)
	if err != nil {
		return "", "", err
	}
	if file.SourceFileChecksum != "" && !strings.EqualFold(file.SourceFileChecksum, checksum) {
		return "", "", fmt.Errorf("preview source %s changed since planning", file.Path)
	}
	return path, checksum, nil
}

func previewFileMatchesAsset(file config.PreviewFile, asset asc.AppPreview) bool {
	if file.SourceFileChecksum != "" || asset.SourceFileChecksum != "" {
		return strings.EqualFold(file.SourceFileChecksum, asset.SourceFileChecksum)
	}
	return file.Path != "" && file.Path == asset.FileName
}

func samePreviewFrame(observed *string, current string) bool {
	if observed == nil {
		return current == ""
	}
	return *observed == current
}
