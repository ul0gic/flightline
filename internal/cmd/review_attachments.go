package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ReviewAttachmentsResult struct {
	BundleID string                 `json:"bundleId"`
	Version  string                 `json:"version"`
	Items    []asc.ReviewAttachment `json:"attachments"`
}

func (r ReviewAttachmentsResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Items))
	for index := range r.Items {
		item := &r.Items[index]
		rows = append(rows, []string{item.ID, item.FileName, item.AssetDeliveryState.State, item.SourceFileChecksum})
	}
	return []string{"ID", "FILE", "STATE", "CHECKSUM"}, rows
}

type ReviewAttachmentActionResult struct {
	Action       string `json:"action"`
	BundleID     string `json:"bundleId"`
	AttachmentID string `json:"attachmentId"`
	State        string `json:"state,omitempty"`
	Changed      bool   `json:"changed"`
}

func (r ReviewAttachmentActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ATTACHMENT_ID", "STATE", "CHANGED"}, [][]string{{r.Action, r.AttachmentID, r.State, strconv.FormatBool(r.Changed)}}
}

func newReviewAttachmentsCommand() *cobra.Command {
	root := &cobra.Command{Use: "review-attachments", Short: "Manage App Store Review attachments"}
	root.AddCommand(newReviewAttachmentsListCommand(), newReviewAttachmentsUploadCommand(), newReviewAttachmentsWaitCommand(), newReviewAttachmentsDeleteCommand())
	return root
}

func reviewAttachmentTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("version", "", "App Store version string")
	cmd.Flags().String("platform", "IOS", "App Store platform")
}

func resolveReviewAttachmentParent(ctx context.Context, c *asc.Client, cmd *cobra.Command, bundleID string) (detailID, version string, err error) {
	version, _ = cmd.Flags().GetString("version")
	platform, _ := cmd.Flags().GetString("platform")
	if version == "" {
		return "", "", errors.New("review attachments require --version")
	}
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return "", "", err
	}
	versionID, err := resolveUniqueAppStoreVersionID(ctx, c, appID, version, platform)
	if err != nil {
		return "", "", err
	}
	detailID, _, err = fetchAppStoreReviewDetail(ctx, c, versionID)
	if err != nil {
		return "", "", err
	}
	return detailID, version, nil
}

func newReviewAttachmentsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Short: "List attachments for a version's review detail", Args: cobra.ExactArgs(1)}
	reviewAttachmentTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		detailID, version, err := resolveReviewAttachmentParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		items := make([]asc.ReviewAttachment, 0)
		if detailID != "" {
			items, err = asc.ListReviewAttachments(cmd.Context(), c, detailID)
			if err != nil {
				return err
			}
		}
		return Render(ReviewAttachmentsResult{BundleID: args[0], Version: version, Items: items}, outputMode())
	}
	return cmd
}

func newReviewAttachmentsUploadCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "upload <bundleId> <file>", Short: "Upload one review attachment and wait for processing", Args: cobra.ExactArgs(2)}
	reviewAttachmentTargetFlags(cmd)
	previewPollFlags(cmd)
	cmd.Flags().Bool("resume", false, "resume a matching upload checkpoint")
	cmd.Flags().Bool("confirm", false, "confirm attachment upload")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requirePreviewConfirm(cmd); err != nil {
			return err
		}
		if err := validateScreenshotFile(args[1]); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		detailID, _, err := resolveReviewAttachmentParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if detailID == "" {
			return errors.New("app store review detail is absent; create reviewer detail before uploading an attachment")
		}
		resume, _ := cmd.Flags().GetBool("resume")
		result, err := uploadReviewAttachmentWithClient(cmd.Context(), c, detailID, args[1], resume, previewPollOptions(cmd))
		if err != nil {
			return err
		}
		result.BundleID = args[0]
		return Render(result, outputMode())
	}
	return cmd
}

func uploadReviewAttachmentWithClient(ctx context.Context, c *asc.Client, detailID, filePath string, resume bool, poll asc.AssetPollOptions) (ReviewAttachmentActionResult, error) {
	if err := validatePreviewPoll(poll); err != nil {
		return ReviewAttachmentActionResult{}, err
	}
	checksum, err := md5HexOfFile(filePath)
	if err != nil {
		return ReviewAttachmentActionResult{}, err
	}
	items, err := asc.ListReviewAttachments(ctx, c, detailID)
	if err != nil {
		return ReviewAttachmentActionResult{}, err
	}
	for index := range items {
		item := &items[index]
		if filepath.Base(filePath) == item.FileName && item.AssetDeliveryState.State == "AWAITING_UPLOAD" {
			return resumeReviewAttachmentUpload(ctx, c, detailID, item.ID, filePath, checksum, resume, poll)
		}
		if strings.EqualFold(item.SourceFileChecksum, checksum) {
			complete, err := asc.WaitReviewAttachmentProcessing(ctx, c, item.ID, poll)
			if err != nil {
				return ReviewAttachmentActionResult{}, err
			}
			return ReviewAttachmentActionResult{Action: "skipped", AttachmentID: item.ID, State: complete.AssetDeliveryState.State}, nil
		}
		if filepath.Base(filePath) == item.FileName {
			return ReviewAttachmentActionResult{}, fmt.Errorf("review attachment %s with the same filename is %s; inspect or wait by ID before another upload", item.ID, item.AssetDeliveryState.State)
		}
	}
	if resume {
		return ReviewAttachmentActionResult{}, errors.New("no matching pending review attachment to resume; inspect listing before another upload")
	}
	result, attachment, err := asc.UploadReviewAttachmentAndWait(ctx, c, detailID, filePath, resume, poll)
	if err != nil {
		return ReviewAttachmentActionResult{}, err
	}
	return ReviewAttachmentActionResult{Action: "uploaded", AttachmentID: result.ID, State: attachment.AssetDeliveryState.State, Changed: true}, nil
}

func resumeReviewAttachmentUpload(ctx context.Context, c *asc.Client, detailID, attachmentID, filePath, checksum string, resume bool, poll asc.AssetPollOptions) (ReviewAttachmentActionResult, error) {
	if !resume {
		return ReviewAttachmentActionResult{}, fmt.Errorf("review attachment %s is awaiting upload; use --resume with its checkpoint or inspect by ID", attachmentID)
	}
	if err := asc.VerifyPendingAssetResume(asc.AssetKindReviewAttachment, filePath, detailID, checksum, attachmentID); err != nil {
		return ReviewAttachmentActionResult{}, err
	}
	result, complete, err := asc.UploadReviewAttachmentAndWait(ctx, c, detailID, filePath, true, poll)
	if err != nil {
		return ReviewAttachmentActionResult{}, err
	}
	if result.ID != attachmentID {
		return ReviewAttachmentActionResult{}, fmt.Errorf("resumed review attachment ID changed from %s to %s; inspect before retrying", attachmentID, result.ID)
	}
	return ReviewAttachmentActionResult{Action: "resumed", AttachmentID: attachmentID, State: complete.AssetDeliveryState.State, Changed: true}, nil
}

func newReviewAttachmentsWaitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "wait <bundleId>", Short: "Wait for an existing review attachment without uploading again", Args: cobra.ExactArgs(1)}
	reviewAttachmentTargetFlags(cmd)
	previewPollFlags(cmd)
	cmd.Flags().String("attachment", "", "review attachment ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("attachment")
		if id == "" {
			return errors.New("wait requires --attachment")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		detailID, _, err := resolveReviewAttachmentParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if err := verifyReviewAttachmentMember(cmd.Context(), c, detailID, id); err != nil {
			return err
		}
		attachment, err := asc.WaitReviewAttachmentProcessing(cmd.Context(), c, id, previewPollOptions(cmd))
		if err != nil {
			return err
		}
		return Render(ReviewAttachmentActionResult{Action: "wait", BundleID: args[0], AttachmentID: id, State: attachment.AssetDeliveryState.State}, outputMode())
	}
	return cmd
}

func newReviewAttachmentsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Short: "Delete one verified review attachment", Args: cobra.ExactArgs(1)}
	reviewAttachmentTargetFlags(cmd)
	cmd.Flags().String("attachment", "", "review attachment ID")
	cmd.Flags().Bool("confirm", false, "confirm attachment deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requirePreviewConfirm(cmd); err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("attachment")
		if id == "" {
			return errors.New("delete requires --attachment")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		detailID, _, err := resolveReviewAttachmentParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if err := asc.DeleteReviewAttachment(cmd.Context(), c, detailID, id); err != nil {
			return err
		}
		return Render(ReviewAttachmentActionResult{Action: "delete", BundleID: args[0], AttachmentID: id, Changed: true}, outputMode())
	}
	return cmd
}

func verifyReviewAttachmentMember(ctx context.Context, c *asc.Client, detailID, id string) error {
	if detailID == "" {
		return errors.New("app store review detail is absent")
	}
	items, err := asc.ListReviewAttachments(ctx, c, detailID)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index].ID == id {
			return nil
		}
	}
	return fmt.Errorf("review attachment %s does not belong to requested version", id)
}
