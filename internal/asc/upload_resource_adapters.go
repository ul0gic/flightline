package asc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type AssetPollOptions struct {
	Interval    time.Duration
	MaxAttempts int
}

type AssetProcessingPendingError struct {
	Kind  AssetKind
	ID    string
	Cause error
}

func (e *AssetProcessingPendingError) Error() string {
	return fmt.Sprintf("%s %s processing is not confirmed; inspect or wait by asset ID before any new upload: %v", e.Kind, e.ID, e.Cause)
}

func (e *AssetProcessingPendingError) Unwrap() error { return e.Cause }

// VerifyPendingAssetResume binds an explicit resume to the pending resource
// observed under the same parent. A missing or foreign checkpoint cannot cause
// Upload to reserve a second resource.
func VerifyPendingAssetResume(kind AssetKind, filePath, parentID, checksum, assetID string) error {
	if parentID == "" || assetID == "" || checksum == "" {
		return errors.New("pending asset resume requires parent, asset ID, and checksum")
	}
	endpoints, err := kind.endpoints()
	if err != nil {
		return err
	}
	asset, err := normalizeAsset(UploadAsset{Path: filePath})
	if err != nil {
		return err
	}
	cp, found, err := tryLoadCheckpointForAsset(asset.Path, kind, endpoints, parentID)
	if err != nil {
		return err
	}
	if !found || cp.AssetID != assetID {
		return fmt.Errorf("no matching upload checkpoint for pending %s %s; inspect or wait by ID before another upload", kind, assetID)
	}
	return validateCheckpointForReuse(cp, kind, endpoints, parentID, asset, checksum)
}

func UploadAppPreviewAndWait(ctx context.Context, c *Client, setID, filePath string, resume bool, poll AssetPollOptions) (UploadResult, AppPreview, error) {
	if err := validateAssetPoll(setID, poll); err != nil {
		return UploadResult{}, AppPreview{}, err
	}
	result, err := c.Upload(ctx, UploadOptions{Kind: AssetKindAppPreview, ParentID: setID, Asset: UploadAsset{Path: filePath}, ResumeFromCheckpoint: resume})
	if err != nil {
		return UploadResult{}, AppPreview{}, err
	}
	preview, err := WaitAppPreviewProcessing(ctx, c, result.ID, poll)
	return result, preview, err
}

func UploadReviewAttachmentAndWait(ctx context.Context, c *Client, reviewDetailID, filePath string, resume bool, poll AssetPollOptions) (UploadResult, ReviewAttachment, error) {
	if err := validateAssetPoll(reviewDetailID, poll); err != nil {
		return UploadResult{}, ReviewAttachment{}, err
	}
	result, err := c.Upload(ctx, UploadOptions{Kind: AssetKindReviewAttachment, ParentID: reviewDetailID, Asset: UploadAsset{Path: filePath}, ResumeFromCheckpoint: resume})
	if err != nil {
		return UploadResult{}, ReviewAttachment{}, err
	}
	attachment, err := WaitReviewAttachmentProcessing(ctx, c, result.ID, poll)
	return result, attachment, err
}

func WaitAppPreviewProcessing(ctx context.Context, c *Client, previewID string, opts AssetPollOptions) (AppPreview, error) {
	if err := validateAssetPoll(previewID, opts); err != nil {
		return AppPreview{}, err
	}
	var last AppPreview
	for attempt := range opts.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return last, pendingAsset(AssetKindAppPreview, previewID, err)
		}
		preview, err := GetAppPreview(ctx, c, previewID)
		if err != nil {
			return last, pendingAsset(AssetKindAppPreview, previewID, err)
		}
		last = preview
		done, err := inspectAssetState(AssetKindAppPreview, previewID, preview.VideoDeliveryState)
		if done || err != nil {
			return preview, err
		}
		if attempt+1 < opts.MaxAttempts {
			if err := waitAssetPoll(ctx, opts.Interval); err != nil {
				return preview, pendingAsset(AssetKindAppPreview, previewID, err)
			}
		}
	}
	return last, pendingAsset(AssetKindAppPreview, previewID, errors.New("poll attempt limit reached"))
}

func WaitReviewAttachmentProcessing(ctx context.Context, c *Client, attachmentID string, opts AssetPollOptions) (ReviewAttachment, error) {
	if err := validateAssetPoll(attachmentID, opts); err != nil {
		return ReviewAttachment{}, err
	}
	var last ReviewAttachment
	for attempt := range opts.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return last, pendingAsset(AssetKindReviewAttachment, attachmentID, err)
		}
		attachment, err := GetReviewAttachment(ctx, c, attachmentID)
		if err != nil {
			return last, pendingAsset(AssetKindReviewAttachment, attachmentID, err)
		}
		last = attachment
		done, err := inspectAssetState(AssetKindReviewAttachment, attachmentID, attachment.AssetDeliveryState)
		if done || err != nil {
			return attachment, err
		}
		if attempt+1 < opts.MaxAttempts {
			if err := waitAssetPoll(ctx, opts.Interval); err != nil {
				return attachment, pendingAsset(AssetKindReviewAttachment, attachmentID, err)
			}
		}
	}
	return last, pendingAsset(AssetKindReviewAttachment, attachmentID, errors.New("poll attempt limit reached"))
}

func validateAssetPoll(id string, opts AssetPollOptions) error {
	if id == "" || opts.MaxAttempts <= 0 || opts.Interval < 0 {
		return errors.New("asset polling requires an ID, positive attempt limit, and nonnegative interval")
	}
	return nil
}

func pendingAsset(kind AssetKind, id string, cause error) error {
	return &AssetProcessingPendingError{Kind: kind, ID: id, Cause: cause}
}

func inspectAssetState(kind AssetKind, id string, media AppMediaAssetState) (bool, error) {
	switch media.State {
	case "COMPLETE":
		return true, nil
	case "AWAITING_UPLOAD", "UPLOAD_COMPLETE", "PROCESSING":
		if media.State == "PROCESSING" && (kind == AssetKindReviewAttachment || kind == AssetKindAppEventCardScreenshot || kind == AssetKindAppEventDetailsScreenshot) {
			break
		}
		return false, nil
	case "FAILED":
		messages := make([]string, 0, len(media.Errors))
		for _, item := range media.Errors {
			messages = append(messages, item.Code+": "+item.Description)
		}
		return false, fmt.Errorf("%s %s processing failed: %s", kind, id, strings.Join(messages, "; "))
	}
	return false, fmt.Errorf("%s %s has unknown processing state %q; inspect live state before retrying upload", kind, id, media.State)
}

func waitAssetPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
