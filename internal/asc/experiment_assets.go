package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type ExperimentAssetSet struct {
	ID             string `json:"id"`
	LocalizationID string `json:"localizationId"`
	Kind           string `json:"kind"`
	DisplayType    string `json:"displayType"`
}

type experimentAssetSetAttrs struct {
	ScreenshotDisplayType string `json:"screenshotDisplayType"`
	PreviewType           string `json:"previewType"`
}

func experimentSetKind(preview bool) (resource, collection, attribute string) {
	if preview {
		return "appPreviewSets", "appPreviewSets", "previewType"
	}
	return "appScreenshotSets", "appScreenshotSets", "screenshotDisplayType"
}

func ListExperimentAssetSets(ctx context.Context, c *Client, localizationID string, preview bool) ([]ExperimentAssetSet, error) {
	if localizationID == "" {
		return nil, errors.New("localization ID is required")
	}
	resource, collection, _ := experimentSetKind(preview)
	path := "/v1/appStoreVersionExperimentTreatmentLocalizations/" + url.PathEscape(localizationID) + "/" + collection
	out := make([]ExperimentAssetSet, 0)
	for page, err := range Pages[experimentAssetSetAttrs](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list experiment asset sets: %w", err)
		}
		for _, row := range page.Data {
			set, err := projectExperimentAssetSet(row, localizationID, resource, preview)
			if err != nil {
				return nil, err
			}
			out = append(out, set)
		}
	}
	return out, nil
}

func projectExperimentAssetSet(row Resource[experimentAssetSetAttrs], localizationID, resource string, preview bool) (ExperimentAssetSet, error) {
	if row.Type != resource || row.ID == "" {
		return ExperimentAssetSet{}, errors.New("experiment asset set has invalid identity")
	}
	if rel, ok := row.Relationships["appStoreVersionExperimentTreatmentLocalization"]; ok && len(rel.Data) > 0 {
		id, err := experimentRelationshipID(row.Relationships, "appStoreVersionExperimentTreatmentLocalization", "appStoreVersionExperimentTreatmentLocalizations")
		if err != nil || id != localizationID {
			return ExperimentAssetSet{}, errors.New("experiment asset set belongs to another localization")
		}
	}
	display := row.Attributes.ScreenshotDisplayType
	if preview {
		display = row.Attributes.PreviewType
	}
	if display == "" {
		return ExperimentAssetSet{}, errors.New("experiment asset set missing display type")
	}
	return ExperimentAssetSet{ID: row.ID, LocalizationID: localizationID, Kind: resource, DisplayType: display}, nil
}

func FindOrCreateExperimentAssetSet(ctx context.Context, c *Client, localizationID, displayType string, preview bool) (ExperimentAssetSet, bool, error) {
	if displayType == "" {
		return ExperimentAssetSet{}, false, errors.New("display type is required")
	}
	sets, err := ListExperimentAssetSets(ctx, c, localizationID, preview)
	if err != nil {
		return ExperimentAssetSet{}, false, err
	}
	var selected *ExperimentAssetSet
	for i := range sets {
		if sets[i].DisplayType == displayType {
			if selected != nil {
				return ExperimentAssetSet{}, false, errors.New("duplicate experiment asset sets")
			}
			selected = &sets[i]
		}
	}
	if selected != nil {
		return *selected, false, nil
	}
	resource, _, attribute := experimentSetKind(preview)
	body := map[string]any{"data": map[string]any{"type": resource, "attributes": map[string]any{attribute: displayType}, "relationships": map[string]any{"appStoreVersionExperimentTreatmentLocalization": map[string]any{"data": map[string]any{"type": "appStoreVersionExperimentTreatmentLocalizations", "id": localizationID}}}}}
	resp, err := Post[Single[experimentAssetSetAttrs]](ctx, c, "/v1/"+resource, nil, body)
	if err != nil {
		return ExperimentAssetSet{}, false, fmt.Errorf("create experiment asset set (inspect live before retry): %w", err)
	}
	row := resp.Data
	if row.ID == "" || row.Type != resource {
		return ExperimentAssetSet{}, false, errors.New("created experiment asset set has invalid identity; inspect live")
	}
	got := row.Attributes.ScreenshotDisplayType
	if preview {
		got = row.Attributes.PreviewType
	}
	if got != displayType {
		return ExperimentAssetSet{}, false, errors.New("created experiment asset set type differs; inspect live")
	}
	return ExperimentAssetSet{ID: row.ID, LocalizationID: localizationID, Kind: resource, DisplayType: got}, true, nil
}

type ExperimentScreenshot struct {
	ID       string             `json:"id"`
	SetID    string             `json:"setId"`
	FileName string             `json:"fileName"`
	Checksum string             `json:"checksum"`
	State    AppMediaAssetState `json:"assetDeliveryState"`
}
type experimentScreenshotAttrs struct {
	FileName           string             `json:"fileName"`
	SourceFileChecksum string             `json:"sourceFileChecksum"`
	AssetDeliveryState AppMediaAssetState `json:"assetDeliveryState"`
}

func ListExperimentScreenshots(ctx context.Context, c *Client, setID string) ([]ExperimentScreenshot, error) {
	if setID == "" {
		return nil, errors.New("set ID is required")
	}
	out := make([]ExperimentScreenshot, 0)
	for page, err := range Pages[experimentScreenshotAttrs](ctx, c, "/v1/appScreenshotSets/"+url.PathEscape(setID)+"/appScreenshots", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, err
		}
		for _, row := range page.Data {
			if row.Type != "appScreenshots" || row.ID == "" {
				return nil, errors.New("screenshot has invalid identity")
			}
			out = append(out, ExperimentScreenshot{ID: row.ID, SetID: setID, FileName: row.Attributes.FileName, Checksum: row.Attributes.SourceFileChecksum, State: row.Attributes.AssetDeliveryState})
		}
	}
	return out, nil
}

func GetExperimentScreenshot(ctx context.Context, c *Client, setID, screenshotID string) (ExperimentScreenshot, error) {
	if setID == "" || screenshotID == "" {
		return ExperimentScreenshot{}, errors.New("set and screenshot IDs required")
	}
	resp, err := Get[Single[experimentScreenshotAttrs]](ctx, c, "/v1/appScreenshots/"+url.PathEscape(screenshotID), nil)
	if err != nil {
		return ExperimentScreenshot{}, err
	}
	row := resp.Data
	if row.Type != "appScreenshots" || row.ID != screenshotID {
		return ExperimentScreenshot{}, errors.New("screenshot response identity mismatch")
	}
	if rel, ok := row.Relationships["appScreenshotSet"]; ok && len(rel.Data) > 0 {
		id, err := experimentRelationshipID(row.Relationships, "appScreenshotSet", "appScreenshotSets")
		if err != nil || id != setID {
			return ExperimentScreenshot{}, errors.New("screenshot belongs to another set")
		}
	}
	return ExperimentScreenshot{ID: row.ID, SetID: setID, FileName: row.Attributes.FileName, Checksum: row.Attributes.SourceFileChecksum, State: row.Attributes.AssetDeliveryState}, nil
}

func WaitExperimentScreenshotProcessing(ctx context.Context, c *Client, setID, screenshotID string, poll AssetPollOptions) (ExperimentScreenshot, error) {
	if err := validateAssetPoll(screenshotID, poll); err != nil {
		return ExperimentScreenshot{}, err
	}
	var last ExperimentScreenshot
	for attempt := range poll.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return last, pendingAsset(AssetKindAppScreenshot, screenshotID, err)
		}
		current, err := GetExperimentScreenshot(ctx, c, setID, screenshotID)
		if err != nil {
			return last, pendingAsset(AssetKindAppScreenshot, screenshotID, err)
		}
		last = current
		done, err := inspectAssetState(AssetKindAppScreenshot, screenshotID, current.State)
		if done || err != nil {
			return current, err
		}
		if attempt+1 < poll.MaxAttempts {
			if err := waitAssetPoll(ctx, poll.Interval); err != nil {
				return last, pendingAsset(AssetKindAppScreenshot, screenshotID, err)
			}
		}
	}
	return last, pendingAsset(AssetKindAppScreenshot, screenshotID, errors.New("poll attempt limit reached"))
}

func DeleteExperimentScreenshot(ctx context.Context, c *Client, setID, screenshotID string) error {
	items, err := ListExperimentScreenshots(ctx, c, setID)
	if err != nil {
		return err
	}
	found := false
	for i := range items {
		if items[i].ID == screenshotID {
			found = true
		}
	}
	if !found {
		return errors.New("screenshot does not belong to selected set")
	}
	if err := c.Delete(ctx, "/v1/appScreenshots/"+url.PathEscape(screenshotID), nil); err != nil {
		return fmt.Errorf("delete experiment screenshot (inspect live before retry): %w", err)
	}
	return nil
}
