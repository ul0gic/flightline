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

type AppEventMediaResult struct {
	Media []asc.AppEventMediaView `json:"media"`
}

func (r AppEventMediaResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Media))
	for i := range r.Media {
		m := &r.Media[i]
		rows = append(rows, []string{m.ID, m.Kind, m.AssetType, m.FileName, m.DeliveryState.State})
	}
	return []string{"ID", "KIND", "SLOT", "FILE", "STATE"}, rows
}

type AppEventMediaActionResult struct {
	Action  string `json:"action"`
	ID      string `json:"id"`
	State   string `json:"state,omitempty"`
	Changed bool   `json:"changed"`
}

func (r AppEventMediaActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "STATE", "CHANGED"}, [][]string{{r.Action, r.ID, r.State, strconv.FormatBool(r.Changed)}}
}

func newEventMediaCommand() *cobra.Command {
	root := &cobra.Command{Use: "media", Short: "Manage event card and detail images or videos"}
	root.AddCommand(newEventMediaListCommand(), newEventMediaGetCommand(), newEventMediaUploadCommand(), newEventMediaWaitCommand(), newEventMediaDeleteCommand())
	return root
}
func mediaTargetFlags(cmd *cobra.Command) {
	localizationTargetFlags(cmd)
	cmd.Flags().String("media", "", "event media ID")
}
func mediaTarget(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("media")
	if id == "" {
		return "", errors.New("--media is required")
	}
	return id, nil
}
func mediaContext(cmd *cobra.Command, bundle string, mutate bool) (*asc.Client, string, error) {
	var c *asc.Client
	var eventID string
	var err error
	if mutate {
		c, eventID, err = draftEventContext(cmd, bundle)
	} else {
		c, eventID, err = eventLocalizationContext(cmd, bundle)
	}
	if err != nil {
		return nil, "", err
	}
	id, err := localizationTarget(cmd)
	if err != nil {
		return nil, "", err
	}
	if _, err := asc.FindAppEventLocalization(cmd.Context(), c, eventID, id); err != nil {
		return nil, "", err
	}
	return c, id, nil
}
func newEventMediaListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Short: "List all event images and videos in a localization", Args: cobra.ExactArgs(1)}
	localizationTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, locID, err := mediaContext(cmd, args[0], false)
		if err != nil {
			return err
		}
		items, err := asc.ListAppEventMedia(cmd.Context(), c, locID)
		if err != nil {
			return err
		}
		return Render(AppEventMediaResult{Media: items}, outputMode())
	}
	return cmd
}
func newEventMediaGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Short: "Read one owned event image or video", Args: cobra.ExactArgs(1)}
	mediaTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := mediaTarget(cmd)
		if err != nil {
			return err
		}
		c, locID, err := mediaContext(cmd, args[0], false)
		if err != nil {
			return err
		}
		m, err := asc.FindAppEventMedia(cmd.Context(), c, locID, id)
		if err != nil {
			return err
		}
		return Render(AppEventMediaResult{Media: []asc.AppEventMediaView{m}}, outputMode())
	}
	return cmd
}
func eventMediaPoll(cmd *cobra.Command) asc.AssetPollOptions { return previewPollOptions(cmd) }
func mediaUploadFlags(cmd *cobra.Command) {
	localizationTargetFlags(cmd)
	cmd.Flags().String("asset-type", "", "EVENT_CARD or EVENT_DETAILS_PAGE")
	cmd.Flags().Bool("video", false, "upload video instead of image")
	cmd.Flags().String("preview-frame-time-code", "", "video preview frame time code")
	cmd.Flags().Bool("resume", false, "resume a matching pending upload checkpoint")
	cmd.Flags().Bool("confirm", false, "confirm event media upload")
	previewPollFlags(cmd)
}
func validateEventMediaFile(path string, video bool) error {
	ext := strings.ToLower(filepath.Ext(path))
	if video {
		switch ext {
		case ".mov", ".m4v", ".mp4":
			return nil
		}
		return errors.New("event video must be .mov, .m4v, or .mp4")
	}
	switch ext {
	case ".jpg", ".jpeg", ".png":
		return nil
	}
	return errors.New("event image must be .jpg, .jpeg, or .png")
}
func newEventMediaUploadCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "upload <bundleId> <file>", Short: "Upload one event card or detail image or video and wait for processing", Args: cobra.ExactArgs(2)}
	mediaUploadFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		video, _ := cmd.Flags().GetBool("video")
		if err := validateEventMediaFile(args[1], video); err != nil {
			return err
		}
		assetType, _ := cmd.Flags().GetString("asset-type")
		if _, err := asc.EventMediaKind(video, assetType); err != nil {
			return err
		}
		frame, _ := cmd.Flags().GetString("preview-frame-time-code")
		if frame != "" && !video {
			return errors.New("preview frame requires --video")
		}
		poll := eventMediaPoll(cmd)
		if err := validatePreviewPoll(poll); err != nil {
			return err
		}
		c, locID, err := mediaContext(cmd, args[0], true)
		if err != nil {
			return err
		}
		resume, _ := cmd.Flags().GetBool("resume")
		result, err := uploadEventMediaWithClient(cmd.Context(), c, locID, args[1], assetType, video, frame, resume, poll)
		if err != nil {
			return err
		}
		return Render(result, outputMode())
	}
	return cmd
}
func uploadEventMediaWithClient(ctx context.Context, c *asc.Client, locID, path, assetType string, video bool, frame string, resume bool, poll asc.AssetPollOptions) (AppEventMediaActionResult, error) {
	if err := validatePreviewPoll(poll); err != nil {
		return AppEventMediaActionResult{}, err
	}
	if err := validateEventMediaFile(path, video); err != nil {
		return AppEventMediaActionResult{}, err
	}
	kind, err := asc.EventMediaKind(video, assetType)
	if err != nil {
		return AppEventMediaActionResult{}, err
	}
	checksum, err := md5HexOfFile(path)
	if err != nil {
		return AppEventMediaActionResult{}, err
	}
	items, err := asc.ListAppEventMedia(ctx, c, locID)
	if err != nil {
		return AppEventMediaActionResult{}, err
	}
	item, err := findOccupiedEventSlot(items, assetType)
	if err != nil {
		return AppEventMediaActionResult{}, err
	}
	if item != nil {
		if item.FileName != filepath.Base(path) || item.DeliveryState.State != "AWAITING_UPLOAD" {
			return AppEventMediaActionResult{}, fmt.Errorf("event slot %s already has %s (%s); inspect or delete explicitly before uploading", assetType, item.ID, item.DeliveryState.State)
		}
		if !resume {
			return AppEventMediaActionResult{}, fmt.Errorf("event media %s awaits upload; use --resume with its checkpoint", item.ID)
		}
		if item.Kind != eventMediaResourceKind(video) {
			return AppEventMediaActionResult{}, fmt.Errorf("event slot %s already contains another media kind", assetType)
		}
		if err := asc.VerifyPendingAssetResume(kind, path, locID, checksum, item.ID); err != nil {
			return AppEventMediaActionResult{}, err
		}
		return finishEventMediaUpload(ctx, c, locID, path, assetType, video, frame, true, poll, "resumed", item.ID)
	}
	if resume {
		return AppEventMediaActionResult{}, errors.New("no matching pending event media to resume; inspect before uploading")
	}
	return finishEventMediaUpload(ctx, c, locID, path, assetType, video, frame, false, poll, "uploaded", "")
}
func findOccupiedEventSlot(items []asc.AppEventMediaView, assetType string) (*asc.AppEventMediaView, error) {
	var found *asc.AppEventMediaView
	for i := range items {
		if items[i].AssetType != assetType {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("event slot %s has multiple media; inspect before upload", assetType)
		}
		found = &items[i]
	}
	return found, nil
}
func eventMediaResourceKind(video bool) string {
	if video {
		return "appEventVideoClips"
	}
	return "appEventScreenshots"
}
func finishEventMediaUpload(ctx context.Context, c *asc.Client, locID, path, assetType string, video bool, frame string, resume bool, poll asc.AssetPollOptions, action, expectedID string) (AppEventMediaActionResult, error) {
	result, media, err := asc.UploadAppEventMediaAndWait(ctx, c, locID, path, assetType, video, resume, poll)
	if err != nil {
		return AppEventMediaActionResult{}, err
	}
	if expectedID != "" && result.ID != expectedID {
		return AppEventMediaActionResult{}, fmt.Errorf("resumed media ID changed from %s to %s; inspect before retrying", expectedID, result.ID)
	}
	if frame != "" {
		_, _, err = asc.SetAppEventVideoFrame(ctx, c, result.ID, frame)
		if err != nil {
			return AppEventMediaActionResult{}, err
		}
		media, err = asc.WaitAppEventMedia(ctx, c, result.ID, true, poll)
		if err != nil {
			return AppEventMediaActionResult{}, err
		}
	}
	return AppEventMediaActionResult{Action: action, ID: result.ID, State: media.DeliveryState.State, Changed: true}, nil
}
func newEventMediaWaitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "wait <bundleId>", Short: "Wait for existing event media without uploading again", Args: cobra.ExactArgs(1)}
	mediaTargetFlags(cmd)
	previewPollFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := mediaTarget(cmd)
		if err != nil {
			return err
		}
		c, locID, err := mediaContext(cmd, args[0], false)
		if err != nil {
			return err
		}
		m, err := asc.FindAppEventMedia(cmd.Context(), c, locID, id)
		if err != nil {
			return err
		}
		done, err := asc.WaitAppEventMedia(cmd.Context(), c, id, m.Kind == "appEventVideoClips", eventMediaPoll(cmd))
		if err != nil {
			return err
		}
		return Render(AppEventMediaActionResult{Action: "wait", ID: id, State: done.DeliveryState.State}, outputMode())
	}
	return cmd
}
func newEventMediaDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Short: "Delete one owned draft event image or video", Args: cobra.ExactArgs(1)}
	mediaTargetFlags(cmd)
	cmd.Flags().Bool("confirm", false, "confirm event media deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		id, err := mediaTarget(cmd)
		if err != nil {
			return err
		}
		c, locID, err := mediaContext(cmd, args[0], true)
		if err != nil {
			return err
		}
		if err := asc.DeleteAppEventMedia(cmd.Context(), c, locID, id); err != nil {
			return err
		}
		return Render(AppEventMediaActionResult{Action: "delete", ID: id, Changed: true}, outputMode())
	}
	return cmd
}
