package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type AppEventMediaView struct {
	ID                   string             `json:"id"`
	LocalizationID       string             `json:"localizationId"`
	Kind                 string             `json:"kind"`
	AssetType            string             `json:"appEventAssetType"`
	FileName             string             `json:"fileName"`
	FileSize             int64              `json:"fileSize"`
	PreviewFrameTimeCode string             `json:"previewFrameTimeCode,omitempty"`
	DeliveryState        AppMediaAssetState `json:"deliveryState"`
}

type appEventMediaAttributes struct {
	FileName             string             `json:"fileName"`
	FileSize             int64              `json:"fileSize"`
	AppEventAssetType    string             `json:"appEventAssetType"`
	PreviewFrameTimeCode string             `json:"previewFrameTimeCode"`
	AssetDeliveryState   AppMediaAssetState `json:"assetDeliveryState"`
	VideoDeliveryState   AppMediaAssetState `json:"videoDeliveryState"`
}

func EventMediaKind(video bool, assetType string) (AssetKind, error) {
	switch {
	case !video && assetType == "EVENT_CARD":
		return AssetKindAppEventCardScreenshot, nil
	case !video && assetType == "EVENT_DETAILS_PAGE":
		return AssetKindAppEventDetailsScreenshot, nil
	case video && assetType == "EVENT_CARD":
		return AssetKindAppEventCardVideo, nil
	case video && assetType == "EVENT_DETAILS_PAGE":
		return AssetKindAppEventDetailsVideo, nil
	default:
		return 0, fmt.Errorf("invalid app event asset type %q", assetType)
	}
}

func ListAppEventMedia(ctx context.Context, c *Client, localizationID string) ([]AppEventMediaView, error) {
	if localizationID == "" {
		return nil, errors.New("localization ID is required")
	}
	out := make([]AppEventMediaView, 0)
	for _, kind := range []string{"appEventScreenshots", "appEventVideoClips"} {
		path := "/v1/appEventLocalizations/" + url.PathEscape(localizationID) + "/" + kind
		for page, err := range Pages[appEventMediaAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
			if err != nil {
				return nil, fmt.Errorf("list %s: %w", kind, err)
			}
			for _, row := range page.Data {
				v, err := projectAppEventMedia(row, localizationID, kind)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
		}
	}
	return out, nil
}

func GetAppEventMedia(ctx context.Context, c *Client, mediaID string, video bool) (AppEventMediaView, error) {
	if mediaID == "" {
		return AppEventMediaView{}, errors.New("media ID is required")
	}
	kind := "appEventScreenshots"
	if video {
		kind = "appEventVideoClips"
	}
	resp, err := Get[Single[appEventMediaAttributes]](ctx, c, "/v1/"+kind+"/"+url.PathEscape(mediaID), nil)
	if err != nil {
		return AppEventMediaView{}, err
	}
	v, err := projectAppEventMedia(resp.Data, "", kind)
	if err != nil {
		return AppEventMediaView{}, err
	}
	if v.ID != mediaID {
		return AppEventMediaView{}, errors.New("event media response ID mismatch")
	}
	return v, nil
}

func projectAppEventMedia(row Resource[appEventMediaAttributes], localizationID, kind string) (AppEventMediaView, error) {
	if row.Type != kind || row.ID == "" || row.Attributes.FileName == "" {
		return AppEventMediaView{}, fmt.Errorf("%s missing identity or filename", kind)
	}
	if _, err := EventMediaKind(kind == "appEventVideoClips", row.Attributes.AppEventAssetType); err != nil {
		return AppEventMediaView{}, err
	}
	if rel, ok := row.Relationships["appEventLocalization"]; ok && len(rel.Data) > 0 {
		id, err := iapToOneID(rel, "appEventLocalizations")
		if err != nil {
			return AppEventMediaView{}, err
		}
		if localizationID != "" && localizationID != id {
			return AppEventMediaView{}, fmt.Errorf("event media %s belongs to another localization", row.ID)
		}
		localizationID = id
	}
	state := row.Attributes.AssetDeliveryState
	if kind == "appEventVideoClips" {
		state = row.Attributes.VideoDeliveryState
	}
	return AppEventMediaView{ID: row.ID, LocalizationID: localizationID, Kind: kind, AssetType: row.Attributes.AppEventAssetType, FileName: row.Attributes.FileName, FileSize: row.Attributes.FileSize, PreviewFrameTimeCode: row.Attributes.PreviewFrameTimeCode, DeliveryState: state}, nil
}

func FindAppEventMedia(ctx context.Context, c *Client, localizationID, mediaID string) (AppEventMediaView, error) {
	media, err := ListAppEventMedia(ctx, c, localizationID)
	if err != nil {
		return AppEventMediaView{}, err
	}
	for i := range media {
		if media[i].ID == mediaID {
			return media[i], nil
		}
	}
	return AppEventMediaView{}, fmt.Errorf("event media %s does not belong to localization %s", mediaID, localizationID)
}

func WaitAppEventMedia(ctx context.Context, c *Client, mediaID string, video bool, opts AssetPollOptions) (AppEventMediaView, error) {
	if err := validateAssetPoll(mediaID, opts); err != nil {
		return AppEventMediaView{}, err
	}
	var last AppEventMediaView
	for attempt := range opts.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return last, eventMediaPending(mediaID, err)
		}
		media, err := GetAppEventMedia(ctx, c, mediaID, video)
		if err != nil {
			return last, eventMediaPending(mediaID, err)
		}
		last = media
		kind, _ := EventMediaKind(video, media.AssetType)
		done, err := inspectAssetState(kind, mediaID, media.DeliveryState)
		if done || err != nil {
			return media, err
		}
		if attempt+1 < opts.MaxAttempts {
			if err := waitAssetPoll(ctx, opts.Interval); err != nil {
				return last, eventMediaPending(mediaID, err)
			}
		}
	}
	return last, eventMediaPending(mediaID, errors.New("poll attempt limit reached"))
}

func eventMediaPending(mediaID string, cause error) error {
	return fmt.Errorf("event media %s processing is unconfirmed; inspect or wait by ID before another upload: %w", mediaID, cause)
}

func UploadAppEventMediaAndWait(ctx context.Context, c *Client, localizationID, filePath, assetType string, video, resume bool, opts AssetPollOptions) (UploadResult, AppEventMediaView, error) {
	if err := validateAssetPoll(localizationID, opts); err != nil {
		return UploadResult{}, AppEventMediaView{}, err
	}
	kind, err := EventMediaKind(video, assetType)
	if err != nil {
		return UploadResult{}, AppEventMediaView{}, err
	}
	result, err := c.Upload(ctx, UploadOptions{Kind: kind, ParentID: localizationID, Asset: UploadAsset{Path: filePath}, ResumeFromCheckpoint: resume})
	if err != nil {
		return UploadResult{}, AppEventMediaView{}, err
	}
	media, err := WaitAppEventMedia(ctx, c, result.ID, video, opts)
	if err != nil {
		return result, media, err
	}
	if media.LocalizationID != "" && media.LocalizationID != localizationID {
		return result, media, errors.New("uploaded event media parent mismatch")
	}
	if media.AssetType != assetType {
		return result, media, errors.New("uploaded event media asset type mismatch")
	}
	return result, media, nil
}

func DeleteAppEventMedia(ctx context.Context, c *Client, localizationID, mediaID string) error {
	media, err := FindAppEventMedia(ctx, c, localizationID, mediaID)
	if err != nil {
		return err
	}
	return c.Delete(ctx, "/v1/"+media.Kind+"/"+url.PathEscape(mediaID), nil)
}

func SetAppEventVideoFrame(ctx context.Context, c *Client, mediaID, frame string) (AppEventMediaView, bool, error) {
	if frame == "" {
		return AppEventMediaView{}, false, errors.New("video frame time code is required")
	}
	current, err := GetAppEventMedia(ctx, c, mediaID, true)
	if err != nil {
		return AppEventMediaView{}, false, err
	}
	if current.PreviewFrameTimeCode == frame {
		return current, false, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appEventVideoClips", "id": mediaID, "attributes": map[string]any{"previewFrameTimeCode": frame}}}
	resp, err := Patch[Single[appEventMediaAttributes]](ctx, c, "/v1/appEventVideoClips/"+url.PathEscape(mediaID), nil, body)
	if err != nil {
		return AppEventMediaView{}, false, fmt.Errorf("set event video frame outcome may be uncertain; inspect before retrying: %w", err)
	}
	v, err := projectAppEventMedia(resp.Data, current.LocalizationID, "appEventVideoClips")
	if err != nil {
		return AppEventMediaView{}, false, err
	}
	if v.ID != mediaID || v.PreviewFrameTimeCode != frame {
		return AppEventMediaView{}, false, errors.New("event video frame update unconfirmed; inspect before retrying")
	}
	return v, true, nil
}
