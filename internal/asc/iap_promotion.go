package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

type IAPPromotionalImageAttributes struct {
	FileName           string     `json:"fileName,omitempty"`
	FileSize           int64      `json:"fileSize,omitempty"`
	SourceFileChecksum string     `json:"sourceFileChecksum,omitempty"`
	State              string     `json:"state,omitempty"`
	ImageAsset         ImageAsset `json:"imageAsset,omitempty"`
}

type IAPPromotionalImage struct {
	ID string `json:"id"`
	IAPPromotionalImageAttributes
}

func ListIAPPromotionalImages(ctx context.Context, c *Client, iapID string) ([]IAPPromotionalImage, error) {
	if iapID == "" {
		return nil, errors.New("asc: IAP id is required")
	}
	path := "/v2/inAppPurchases/" + url.PathEscape(iapID) + "/images"
	query := url.Values{"limit": {"200"}}
	out := make([]IAPPromotionalImage, 0)
	seen := make(map[string]bool)
	for page, err := range Pages[IAPPromotionalImageAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, fmt.Errorf("asc: list IAP promotional images: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "inAppPurchaseImages" || row.ID == "" || seen[row.ID] {
				return nil, fmt.Errorf("asc: IAP %q returned an incomplete or duplicate promotional image", iapID)
			}
			seen[row.ID] = true
			out = append(out, IAPPromotionalImage{ID: row.ID, IAPPromotionalImageAttributes: row.Attributes})
		}
	}
	return out, nil
}

func GetIAPPromotionalImage(ctx context.Context, c *Client, imageID string) (IAPPromotionalImage, error) {
	if imageID == "" {
		return IAPPromotionalImage{}, errors.New("asc: image id is required")
	}
	response, err := Get[Single[IAPPromotionalImageAttributes]](ctx, c, "/v1/inAppPurchaseImages/"+url.PathEscape(imageID), nil)
	if err != nil {
		return IAPPromotionalImage{}, fmt.Errorf("asc: get IAP promotional image %q: %w", imageID, err)
	}
	if response.Data.ID != imageID || response.Data.Type != "inAppPurchaseImages" {
		return IAPPromotionalImage{}, fmt.Errorf("asc: IAP promotional image %q response is incomplete or inconsistent", imageID)
	}
	return IAPPromotionalImage{ID: imageID, IAPPromotionalImageAttributes: response.Data.Attributes}, nil
}

func DeleteIAPPromotionalImage(ctx context.Context, c *Client, imageID string) error {
	if imageID == "" {
		return errors.New("asc: image id is required")
	}
	return c.Delete(ctx, "/v1/inAppPurchaseImages/"+url.PathEscape(imageID), nil)
}

// VerifyIAPPromotionalImageResume refuses a new reservation when a pending image
// exists but no checkpoint pins this exact file, parent, checksum, and image ID.
func VerifyIAPPromotionalImageResume(file, iapID, checksum, imageID string) error {
	endpoints, _ := AssetKindIAPPromotionalImage.endpoints()
	asset, err := normalizeAsset(UploadAsset{Path: file})
	if err != nil {
		return err
	}
	cp, found, err := tryLoadCheckpointForAsset(asset.Path, AssetKindIAPPromotionalImage, endpoints, iapID)
	if err != nil {
		return err
	}
	if !found || cp.AssetID != imageID {
		return fmt.Errorf("asc: no matching upload checkpoint for pending IAP promotional image %s; inspect or delete explicitly", imageID)
	}
	return validateCheckpointForReuse(cp, AssetKindIAPPromotionalImage, endpoints, iapID, asset, checksum)
}

func WaitIAPPromotionalImage(ctx context.Context, c *Client, imageID string, opts AssetPollOptions) (IAPPromotionalImage, error) {
	if err := validateAssetPoll(imageID, opts); err != nil {
		return IAPPromotionalImage{}, err
	}
	var last IAPPromotionalImage
	for attempt := range opts.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return last, pendingAsset(AssetKindIAPPromotionalImage, imageID, err)
		}
		image, err := GetIAPPromotionalImage(ctx, c, imageID)
		if err != nil {
			return last, pendingAsset(AssetKindIAPPromotionalImage, imageID, err)
		}
		last = image
		switch image.State {
		case "PREPARE_FOR_SUBMISSION", "WAITING_FOR_REVIEW", "APPROVED", "REJECTED":
			return image, nil
		case "FAILED":
			return image, fmt.Errorf("IAP promotional image %s processing failed", imageID)
		case "AWAITING_UPLOAD", "UPLOAD_COMPLETE":
			// Upload completion is not a processed image; retain its ID for a later wait.
		default:
			return image, fmt.Errorf("IAP promotional image %s has unknown state %q", imageID, image.State)
		}
		if attempt+1 < opts.MaxAttempts {
			if err := waitAssetPoll(ctx, opts.Interval); err != nil {
				return image, pendingAsset(AssetKindIAPPromotionalImage, imageID, err)
			}
		}
	}
	return last, pendingAsset(AssetKindIAPPromotionalImage, imageID, errors.New("poll attempt limit reached"))
}

type PromotedPurchaseAttributes struct {
	VisibleForAllUsers *bool  `json:"visibleForAllUsers,omitempty"`
	Enabled            *bool  `json:"enabled,omitempty"`
	State              string `json:"state,omitempty"`
}

type PromotedPurchase struct {
	ID             string `json:"id"`
	IAPID          string `json:"iapId,omitempty"`
	SubscriptionID string `json:"subscriptionId,omitempty"`
	PromotedPurchaseAttributes
}

func ListAppPromotedPurchases(ctx context.Context, c *Client, appID string) ([]PromotedPurchase, error) {
	if appID == "" {
		return nil, errors.New("asc: app id is required")
	}
	path := "/v1/apps/" + url.PathEscape(appID) + "/promotedPurchases"
	query := url.Values{"limit": {"200"}, "fields[promotedPurchases]": {"visibleForAllUsers,enabled,state,inAppPurchaseV2,subscription"}}
	out := make([]PromotedPurchase, 0)
	seen := map[string]bool{}
	for page, err := range Pages[PromotedPurchaseAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, fmt.Errorf("asc: list app promoted purchases: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "promotedPurchases" || seen[row.ID] {
				return nil, fmt.Errorf("asc: app %q returned incomplete or duplicate promoted purchase", appID)
			}
			iapID, err := promotedRelationshipID(row.Relationships["inAppPurchaseV2"], "inAppPurchases")
			if err != nil {
				return nil, err
			}
			subID, err := promotedRelationshipID(row.Relationships["subscription"], "subscriptions")
			if err != nil || iapID == "" && subID == "" || iapID != "" && subID != "" {
				return nil, fmt.Errorf("asc: promoted purchase %q has missing or ambiguous target", row.ID)
			}
			seen[row.ID] = true
			out = append(out, PromotedPurchase{ID: row.ID, IAPID: iapID, SubscriptionID: subID, PromotedPurchaseAttributes: row.Attributes})
		}
	}
	return orderAppPromotedPurchases(ctx, c, appID, out)
}

func orderAppPromotedPurchases(ctx context.Context, c *Client, appID string, rows []PromotedPurchase) ([]PromotedPurchase, error) {
	byID := make(map[string]PromotedPurchase, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	path := "/v1/apps/" + url.PathEscape(appID) + "/relationships/promotedPurchases"
	ordered := make([]PromotedPurchase, 0, len(rows))
	seen := map[string]bool{}
	for page, err := range Pages[EmptyAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("asc: read promoted purchase order: %w", err)
		}
		for _, ref := range page.Data {
			row, ok := byID[ref.ID]
			if ref.Type != "promotedPurchases" || ref.ID == "" || !ok || seen[ref.ID] {
				return nil, fmt.Errorf("asc: app %q returned incomplete or inconsistent promoted purchase order", appID)
			}
			seen[ref.ID] = true
			ordered = append(ordered, row)
		}
	}
	if len(ordered) != len(rows) {
		return nil, fmt.Errorf("asc: app %q promoted purchase order omits entries", appID)
	}
	return ordered, nil
}

func promotedRelationshipID(rel Relationship, expectedType string) (string, error) {
	if len(rel.Data) == 0 || string(rel.Data) == "null" {
		return "", nil
	}
	var ref struct{ Type, ID string }
	if err := json.Unmarshal(rel.Data, &ref); err != nil || ref.Type != expectedType || ref.ID == "" {
		return "", fmt.Errorf("asc: promoted purchase has invalid %s relationship", expectedType)
	}
	return ref.ID, nil
}

func CreateIAPPromotedPurchase(ctx context.Context, c *Client, appID, iapID string, visible bool, enabled *bool) (string, error) {
	if appID == "" || iapID == "" {
		return "", errors.New("asc: app and IAP ids are required")
	}
	attrs := map[string]any{"visibleForAllUsers": visible}
	if enabled != nil {
		attrs["enabled"] = *enabled
	}
	body := map[string]any{"data": map[string]any{
		"type": "promotedPurchases", "attributes": attrs,
		"relationships": map[string]any{
			"app":             map[string]any{"data": map[string]string{"type": "apps", "id": appID}},
			"inAppPurchaseV2": map[string]any{"data": map[string]string{"type": "inAppPurchases", "id": iapID}},
		},
	}}
	response, err := Post[Single[PromotedPurchaseAttributes]](ctx, c, "/v1/promotedPurchases", nil, body)
	if err != nil {
		return "", err
	}
	if response.Data.ID == "" || response.Data.Type != "promotedPurchases" {
		return "", errors.New("asc: promoted purchase create returned incomplete resource")
	}
	return response.Data.ID, nil
}

func PatchPromotedPurchase(ctx context.Context, c *Client, id string, visible, enabled *bool) error {
	if id == "" || visible == nil && enabled == nil {
		return errors.New("asc: promoted purchase update requires id and explicit attributes")
	}
	attrs := map[string]any{}
	if visible != nil {
		attrs["visibleForAllUsers"] = *visible
	}
	if enabled != nil {
		attrs["enabled"] = *enabled
	}
	body := map[string]any{"data": map[string]any{"type": "promotedPurchases", "id": id, "attributes": attrs}}
	response, err := Patch[Single[PromotedPurchaseAttributes]](ctx, c, "/v1/promotedPurchases/"+url.PathEscape(id), nil, body)
	if err != nil {
		return err
	}
	if response.Data.ID != id || response.Data.Type != "promotedPurchases" {
		return fmt.Errorf("asc: promoted purchase update %q returned inconsistent resource", id)
	}
	return nil
}

func DeletePromotedPurchase(ctx context.Context, c *Client, id string) error {
	if id == "" {
		return errors.New("asc: promoted purchase id is required")
	}
	return c.Delete(ctx, "/v1/promotedPurchases/"+url.PathEscape(id), nil)
}

func OrderAppPromotedPurchases(ctx context.Context, c *Client, appID string, orderedIDs []string) error {
	if appID == "" {
		return errors.New("asc: app id is required")
	}
	refs := make([]map[string]string, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if id == "" {
			return errors.New("asc: promoted purchase id is required")
		}
		refs = append(refs, map[string]string{"type": "promotedPurchases", "id": id})
	}
	_, err := Patch[json.RawMessage](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/relationships/promotedPurchases", nil, map[string]any{"data": refs})
	return err
}
