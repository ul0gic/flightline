package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type ReviewAttachment struct {
	ID                 string             `json:"id"`
	ReviewDetailID     string             `json:"reviewDetailId"`
	FileName           string             `json:"fileName"`
	FileSize           int64              `json:"fileSize"`
	SourceFileChecksum string             `json:"sourceFileChecksum"`
	AssetDeliveryState AppMediaAssetState `json:"assetDeliveryState"`
}

type reviewAttachmentAttributes struct {
	FileName           string             `json:"fileName"`
	FileSize           int64              `json:"fileSize"`
	SourceFileChecksum string             `json:"sourceFileChecksum"`
	AssetDeliveryState AppMediaAssetState `json:"assetDeliveryState"`
}

func ListReviewAttachments(ctx context.Context, c *Client, reviewDetailID string) ([]ReviewAttachment, error) {
	if reviewDetailID == "" {
		return nil, errors.New("review detail ID is required")
	}
	attachments := make([]ReviewAttachment, 0)
	path := "/v1/appStoreReviewDetails/" + url.PathEscape(reviewDetailID) + "/appStoreReviewAttachments"
	for page, err := range Pages[reviewAttachmentAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list review attachments: %w", err)
		}
		for _, row := range page.Data {
			attachment, err := projectReviewAttachment(row, reviewDetailID)
			if err != nil {
				return nil, err
			}
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func GetReviewAttachment(ctx context.Context, c *Client, attachmentID string) (ReviewAttachment, error) {
	if attachmentID == "" {
		return ReviewAttachment{}, errors.New("review attachment ID is required")
	}
	resp, err := Get[Single[reviewAttachmentAttributes]](ctx, c, "/v1/appStoreReviewAttachments/"+url.PathEscape(attachmentID), nil)
	if err != nil {
		return ReviewAttachment{}, fmt.Errorf("read review attachment: %w", err)
	}
	attachment, err := projectReviewAttachment(resp.Data, "")
	if err != nil {
		return ReviewAttachment{}, err
	}
	if attachment.ID != attachmentID {
		return ReviewAttachment{}, errors.New("review attachment response ID mismatch")
	}
	return attachment, nil
}

func projectReviewAttachment(row Resource[reviewAttachmentAttributes], expectedDetailID string) (ReviewAttachment, error) {
	if row.Type != "appStoreReviewAttachments" || row.ID == "" {
		return ReviewAttachment{}, errors.New("review attachment missing type or ID")
	}
	detailID := expectedDetailID
	if rel, ok := row.Relationships["appStoreReviewDetail"]; ok && len(rel.Data) > 0 {
		id, err := iapToOneID(rel, "appStoreReviewDetails")
		if err != nil {
			return ReviewAttachment{}, fmt.Errorf("review attachment %s parent: %w", row.ID, err)
		}
		if detailID != "" && detailID != id {
			return ReviewAttachment{}, fmt.Errorf("review attachment %s belongs to another review detail", row.ID)
		}
		detailID = id
	}
	if detailID == "" {
		return ReviewAttachment{}, fmt.Errorf("review attachment %s has no review-detail relationship", row.ID)
	}
	return ReviewAttachment{ID: row.ID, ReviewDetailID: detailID, FileName: row.Attributes.FileName,
		FileSize: row.Attributes.FileSize, SourceFileChecksum: row.Attributes.SourceFileChecksum,
		AssetDeliveryState: row.Attributes.AssetDeliveryState}, nil
}

func DeleteReviewAttachment(ctx context.Context, c *Client, reviewDetailID, attachmentID string) error {
	if reviewDetailID == "" || attachmentID == "" {
		return errors.New("review detail and attachment IDs are required")
	}
	attachments, err := ListReviewAttachments(ctx, c, reviewDetailID)
	if err != nil {
		return err
	}
	found := false
	for index := range attachments {
		if attachments[index].ID == attachmentID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("review attachment %s does not belong to review detail %s", attachmentID, reviewDetailID)
	}
	if err := c.Delete(ctx, "/v1/appStoreReviewAttachments/"+url.PathEscape(attachmentID), nil); err != nil {
		return fmt.Errorf("delete review attachment: %w", err)
	}
	return nil
}
