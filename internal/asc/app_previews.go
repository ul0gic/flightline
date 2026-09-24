package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type PreviewParent struct {
	Type string
	ID   string
}

type AppPreviewSet struct {
	ID          string        `json:"id"`
	PreviewType string        `json:"previewType"`
	Parent      PreviewParent `json:"parent"`
}

type AppPreview struct {
	ID                   string             `json:"id"`
	SetID                string             `json:"setId"`
	FileName             string             `json:"fileName"`
	FileSize             int64              `json:"fileSize"`
	SourceFileChecksum   string             `json:"sourceFileChecksum"`
	PreviewFrameTimeCode string             `json:"previewFrameTimeCode,omitempty"`
	MimeType             string             `json:"mimeType,omitempty"`
	VideoDeliveryState   AppMediaAssetState `json:"videoDeliveryState"`
}

type appPreviewSetAttributes struct {
	PreviewType string `json:"previewType"`
}

type appPreviewAttributes struct {
	FileName             string             `json:"fileName"`
	FileSize             int64              `json:"fileSize"`
	SourceFileChecksum   string             `json:"sourceFileChecksum"`
	PreviewFrameTimeCode string             `json:"previewFrameTimeCode"`
	MimeType             string             `json:"mimeType"`
	VideoDeliveryState   AppMediaAssetState `json:"videoDeliveryState"`
}

var previewTypes = map[string]bool{
	"IPHONE_67": true, "IPHONE_61": true, "IPHONE_65": true, "IPHONE_58": true,
	"IPHONE_55": true, "IPHONE_47": true, "IPHONE_40": true, "IPHONE_35": true,
	"IPAD_PRO_3GEN_129": true, "IPAD_PRO_3GEN_11": true, "IPAD_PRO_129": true,
	"IPAD_105": true, "IPAD_97": true, "DESKTOP": true, "APPLE_TV": true, "APPLE_VISION_PRO": true,
}

func ValidPreviewType(value string) bool { return previewTypes[value] }

func previewParentPath(parent PreviewParent) (string, error) {
	if parent.ID == "" {
		return "", errors.New("preview parent ID is required")
	}
	switch parent.Type {
	case "appStoreVersionLocalizations":
		return "/v1/appStoreVersionLocalizations/" + url.PathEscape(parent.ID) + "/appPreviewSets", nil
	case "appCustomProductPageLocalizations":
		return "/v1/appCustomProductPageLocalizations/" + url.PathEscape(parent.ID) + "/appPreviewSets", nil
	default:
		return "", fmt.Errorf("unsupported preview parent type %q", parent.Type)
	}
}

func ListAppPreviewSets(ctx context.Context, c *Client, parent PreviewParent) ([]AppPreviewSet, error) {
	path, err := previewParentPath(parent)
	if err != nil {
		return nil, err
	}
	sets := make([]AppPreviewSet, 0)
	for page, err := range Pages[appPreviewSetAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list app preview sets: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "appPreviewSets" || row.ID == "" || !ValidPreviewType(row.Attributes.PreviewType) {
				return nil, fmt.Errorf("app preview set %q has invalid type, ID, or previewType", row.ID)
			}
			if rel, ok := row.Relationships[parent.Type[:len(parent.Type)-1]]; ok && len(rel.Data) > 0 {
				id, err := iapToOneID(rel, parent.Type)
				if err != nil || id != parent.ID {
					return nil, fmt.Errorf("app preview set %s belongs to a different parent", row.ID)
				}
			}
			sets = append(sets, AppPreviewSet{ID: row.ID, PreviewType: row.Attributes.PreviewType, Parent: parent})
		}
	}
	return sets, nil
}

func ListAppPreviews(ctx context.Context, c *Client, setID string) ([]AppPreview, error) {
	if setID == "" {
		return nil, errors.New("app preview set ID is required")
	}
	previews := make([]AppPreview, 0)
	for page, err := range Pages[appPreviewAttributes](ctx, c, "/v1/appPreviewSets/"+url.PathEscape(setID)+"/appPreviews", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list app previews: %w", err)
		}
		for _, row := range page.Data {
			preview, err := projectAppPreview(row, setID)
			if err != nil {
				return nil, err
			}
			previews = append(previews, preview)
		}
	}
	return previews, nil
}

func GetAppPreview(ctx context.Context, c *Client, previewID string) (AppPreview, error) {
	if previewID == "" {
		return AppPreview{}, errors.New("app preview ID is required")
	}
	resp, err := Get[Single[appPreviewAttributes]](ctx, c, "/v1/appPreviews/"+url.PathEscape(previewID), nil)
	if err != nil {
		return AppPreview{}, fmt.Errorf("read app preview: %w", err)
	}
	preview, err := projectAppPreview(resp.Data, "")
	if err != nil {
		return AppPreview{}, err
	}
	if preview.ID != previewID {
		return AppPreview{}, errors.New("app preview response ID mismatch")
	}
	return preview, nil
}

func projectAppPreview(row Resource[appPreviewAttributes], expectedSetID string) (AppPreview, error) {
	if row.Type != "appPreviews" || row.ID == "" {
		return AppPreview{}, errors.New("app preview missing type or ID")
	}
	setID := expectedSetID
	if rel, ok := row.Relationships["appPreviewSet"]; ok && len(rel.Data) > 0 {
		id, err := iapToOneID(rel, "appPreviewSets")
		if err != nil {
			return AppPreview{}, fmt.Errorf("app preview %s set: %w", row.ID, err)
		}
		if setID != "" && setID != id {
			return AppPreview{}, fmt.Errorf("app preview %s belongs to another set", row.ID)
		}
		setID = id
	}
	if setID == "" {
		return AppPreview{}, fmt.Errorf("app preview %s has no set relationship", row.ID)
	}
	return AppPreview{ID: row.ID, SetID: setID, FileName: row.Attributes.FileName, FileSize: row.Attributes.FileSize,
		SourceFileChecksum: row.Attributes.SourceFileChecksum, PreviewFrameTimeCode: row.Attributes.PreviewFrameTimeCode,
		MimeType: row.Attributes.MimeType, VideoDeliveryState: row.Attributes.VideoDeliveryState}, nil
}

func FindOrCreateAppPreviewSet(ctx context.Context, c *Client, parent PreviewParent, previewType string) (AppPreviewSet, bool, error) {
	if !ValidPreviewType(previewType) {
		return AppPreviewSet{}, false, fmt.Errorf("invalid previewType %q", previewType)
	}
	sets, err := ListAppPreviewSets(ctx, c, parent)
	if err != nil {
		return AppPreviewSet{}, false, err
	}
	var selected *AppPreviewSet
	for _, set := range sets {
		if set.PreviewType != previewType {
			continue
		}
		if selected != nil {
			return AppPreviewSet{}, false, fmt.Errorf("duplicate %s preview sets for parent %s", previewType, parent.ID)
		}
		copySet := set
		selected = &copySet
	}
	if selected != nil {
		return *selected, false, nil
	}
	relName := "appStoreVersionLocalization"
	if parent.Type == "appCustomProductPageLocalizations" {
		relName = "appCustomProductPageLocalization"
	}
	body := map[string]any{"data": map[string]any{"type": "appPreviewSets", "attributes": map[string]any{"previewType": previewType},
		"relationships": map[string]any{relName: map[string]any{"data": map[string]any{"type": parent.Type, "id": parent.ID}}},
	}}
	resp, err := Post[Single[appPreviewSetAttributes]](ctx, c, "/v1/appPreviewSets", nil, body)
	if err != nil {
		return AppPreviewSet{}, false, fmt.Errorf("create app preview set: %w", err)
	}
	if resp.Data.Type != "appPreviewSets" || resp.Data.ID == "" || resp.Data.Attributes.PreviewType != previewType {
		return AppPreviewSet{}, false, errors.New("created app preview set response has unexpected identity or previewType")
	}
	return AppPreviewSet{ID: resp.Data.ID, PreviewType: previewType, Parent: parent}, true, nil
}

func SetAppPreviewFrame(ctx context.Context, c *Client, previewID, frameTimeCode string) (AppPreview, error) {
	if previewID == "" || frameTimeCode == "" {
		return AppPreview{}, errors.New("preview ID and frame time code are required")
	}
	current, err := GetAppPreview(ctx, c, previewID)
	if err != nil {
		return AppPreview{}, err
	}
	if current.PreviewFrameTimeCode == frameTimeCode {
		return current, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appPreviews", "id": previewID, "attributes": map[string]any{"previewFrameTimeCode": frameTimeCode}}}
	resp, err := Patch[Single[appPreviewAttributes]](ctx, c, "/v1/appPreviews/"+url.PathEscape(previewID), nil, body)
	if err != nil {
		return AppPreview{}, fmt.Errorf("set app preview frame: %w", err)
	}
	updated, err := projectAppPreview(resp.Data, current.SetID)
	if err != nil {
		return AppPreview{}, err
	}
	if updated.ID != previewID || updated.PreviewFrameTimeCode != frameTimeCode {
		return AppPreview{}, errors.New("app preview frame response did not confirm update; inspect live before retrying")
	}
	return updated, nil
}

func DeleteAppPreview(ctx context.Context, c *Client, setID, previewID string) error {
	if setID == "" || previewID == "" {
		return errors.New("preview set and preview IDs are required")
	}
	previews, err := ListAppPreviews(ctx, c, setID)
	if err != nil {
		return err
	}
	found := false
	for index := range previews {
		if previews[index].ID == previewID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("app preview %s does not belong to set %s", previewID, setID)
	}
	if err := c.Delete(ctx, "/v1/appPreviews/"+url.PathEscape(previewID), nil); err != nil {
		return fmt.Errorf("delete app preview: %w", err)
	}
	return nil
}
