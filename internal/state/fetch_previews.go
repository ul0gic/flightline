package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// FetchPreviews observes all preview sets for every localization on one version.
func FetchPreviews(ctx context.Context, c *asc.Client, versionID string) (*config.PreviewsSpec, error) {
	if versionID == "" {
		return nil, errors.New("preview fetch requires version ID")
	}
	out := &config.PreviewsSpec{Locales: make(map[string]map[string][]config.PreviewFile)}
	seen := make(map[string]bool)
	for page, err := range asc.Pages[versionLocAttrs](ctx, c, "/v1/appStoreVersions/"+url.PathEscape(versionID)+"/appStoreVersionLocalizations", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list preview localizations: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Attributes.Locale == "" {
				return nil, errors.New("preview localization missing ID or locale")
			}
			if seen[row.Attributes.Locale] {
				return nil, fmt.Errorf("duplicate preview localization %s", row.Attributes.Locale)
			}
			seen[row.Attributes.Locale] = true
			types, err := fetchPreviewTypes(ctx, c, asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: row.ID})
			if err != nil {
				return nil, fmt.Errorf("preview localization %s: %w", row.Attributes.Locale, err)
			}
			if len(types) != 0 {
				out.Locales[row.Attributes.Locale] = types
			}
		}
	}
	if len(out.Locales) == 0 {
		return nil, nil
	}
	return out, nil
}

// FetchCPPPreviews observes the preview sets attached to one CPP localization.
func FetchCPPPreviews(ctx context.Context, c *asc.Client, localizationID string) (map[string][]config.PreviewFile, error) {
	if localizationID == "" {
		return nil, errors.New("CPP preview fetch requires localization ID")
	}
	return fetchPreviewTypes(ctx, c, asc.PreviewParent{Type: "appCustomProductPageLocalizations", ID: localizationID})
}

func fetchPreviewTypes(ctx context.Context, c *asc.Client, parent asc.PreviewParent) (map[string][]config.PreviewFile, error) {
	sets, err := asc.ListAppPreviewSets(ctx, c, parent)
	if err != nil {
		return nil, err
	}
	types := make(map[string][]config.PreviewFile)
	for index := range sets {
		set := &sets[index]
		if _, exists := types[set.PreviewType]; exists {
			return nil, fmt.Errorf("duplicate preview set %s for %s", set.PreviewType, parent.ID)
		}
		assets, err := asc.ListAppPreviews(ctx, c, set.ID)
		if err != nil {
			return nil, fmt.Errorf("preview set %s: %w", set.ID, err)
		}
		files, err := projectPreviewFiles(set.ID, assets)
		if err != nil {
			return nil, err
		}
		types[set.PreviewType] = files
	}
	if len(types) == 0 {
		return nil, nil
	}
	return types, nil
}

func projectPreviewFiles(setID string, assets []asc.AppPreview) ([]config.PreviewFile, error) {
	files := make([]config.PreviewFile, 0, len(assets))
	seenNames := make(map[string]bool)
	seenChecksums := make(map[string]bool)
	for index := range assets {
		asset := &assets[index]
		if asset.VideoDeliveryState.State != "COMPLETE" {
			return nil, fmt.Errorf("preview %s is %q; inspect or wait by ID before planning", asset.ID, asset.VideoDeliveryState.State)
		}
		if asset.FileName == "" {
			return nil, fmt.Errorf("preview %s missing filename", asset.ID)
		}
		if seenNames[asset.FileName] {
			return nil, fmt.Errorf("preview set %s contains duplicate filename %s", setID, asset.FileName)
		}
		seenNames[asset.FileName] = true
		if asset.SourceFileChecksum != "" {
			checksum := strings.ToLower(asset.SourceFileChecksum)
			if seenChecksums[checksum] {
				return nil, fmt.Errorf("preview set %s contains duplicate checksum", setID)
			}
			seenChecksums[checksum] = true
		}
		file := config.PreviewFile{Path: asset.FileName, SourceFileChecksum: asset.SourceFileChecksum}
		if asset.PreviewFrameTimeCode != "" {
			frame := asset.PreviewFrameTimeCode
			file.PreviewFrameTimeCode = &frame
		}
		files = append(files, file)
	}
	return files, nil
}
