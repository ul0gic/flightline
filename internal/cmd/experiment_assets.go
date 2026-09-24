package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ExperimentAssetRow struct {
	Kind        string `json:"kind"`
	DisplayType string `json:"displayType"`
	SetID       string `json:"setId"`
	AssetID     string `json:"assetId"`
	FileName    string `json:"fileName"`
	Checksum    string `json:"checksum"`
	State       string `json:"state"`
}
type ExperimentAssetListResult struct {
	Assets []ExperimentAssetRow `json:"assets"`
}

func (r ExperimentAssetListResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Assets))
	for i := range r.Assets {
		x := &r.Assets[i]
		rows = append(rows, []string{x.Kind, x.DisplayType, x.FileName, x.State, x.AssetID})
	}
	return []string{"KIND", "DISPLAY", "FILE", "STATE", "ID"}, rows
}

type ExperimentAssetAction struct {
	Action   string `json:"action"`
	Kind     string `json:"kind"`
	SetID    string `json:"setId"`
	AssetID  string `json:"assetId"`
	Checksum string `json:"checksum,omitempty"`
	State    string `json:"state,omitempty"`
	Changed  bool   `json:"changed"`
}

func (r ExperimentAssetAction) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "KIND", "SET", "ASSET", "STATE", "CHANGED"}, [][]string{{r.Action, r.Kind, r.SetID, r.AssetID, r.State, strconv.FormatBool(r.Changed)}}
}

func newExperimentAssetsCommand() *cobra.Command {
	root := &cobra.Command{Use: "assets", Short: "Manage treatment screenshots and previews"}
	root.AddCommand(newExperimentAssetListCommand(), newExperimentAssetUploadCommand(), newExperimentAssetWaitCommand(), newExperimentAssetDeleteCommand())
	return root
}
func experimentAssetFlags(cmd *cobra.Command) {
	experimentLocalizationFlags(cmd)
	cmd.Flags().String("kind", "", "screenshot or preview")
	cmd.Flags().String("display-type", "", "screenshot display type or preview type")
}
func experimentAssetKind(cmd *cobra.Command) (preview bool, display string, err error) {
	kind, _ := cmd.Flags().GetString("kind")
	display, _ = cmd.Flags().GetString("display-type")
	if kind != "screenshot" && kind != "preview" {
		return false, "", errors.New("--kind must be screenshot or preview")
	}
	if display == "" {
		return false, "", errors.New("--display-type is required")
	}
	if kind == "screenshot" && !isValidDeviceSet(display) {
		return false, "", errors.New("invalid screenshot display type")
	}
	if kind == "preview" && !asc.ValidPreviewType(display) {
		return false, "", errors.New("invalid preview type")
	}
	return kind == "preview", display, nil
}
func experimentAssetPollFlags(cmd *cobra.Command) {
	cmd.Flags().Int("max-polls", 20, "maximum processing status reads")
	cmd.Flags().Duration("poll-interval", 3*time.Second, "time between status reads")
}
func experimentAssetPoll(cmd *cobra.Command) (asc.AssetPollOptions, error) {
	n, _ := cmd.Flags().GetInt("max-polls")
	d, _ := cmd.Flags().GetDuration("poll-interval")
	if n <= 0 || d < 0 {
		return asc.AssetPollOptions{}, errors.New("positive max-polls and nonnegative poll-interval required")
	}
	return asc.AssetPollOptions{MaxAttempts: n, Interval: d}, nil
}

func newExperimentAssetListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Args: cobra.ExactArgs(1), Short: "List all sets and media under a selected treatment locale"}
	experimentLocalizationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		_, _, loc, err := selectedExperimentLocalization(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		rows, err := listExperimentAssetRows(cmd.Context(), c, loc.ID)
		if err != nil {
			return err
		}
		return Render(ExperimentAssetListResult{Assets: rows}, outputMode())
	}
	return cmd
}
func listExperimentAssetRows(ctx context.Context, c *asc.Client, locID string) ([]ExperimentAssetRow, error) {
	out := make([]ExperimentAssetRow, 0)
	for _, preview := range []bool{false, true} {
		sets, err := asc.ListExperimentAssetSets(ctx, c, locID, preview)
		if err != nil {
			return nil, err
		}
		for _, set := range sets {
			rows, err := experimentAssetSetRows(ctx, c, set, preview)
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		}
	}
	return out, nil
}

func experimentAssetSetRows(ctx context.Context, c *asc.Client, set asc.ExperimentAssetSet, preview bool) ([]ExperimentAssetRow, error) {
	rows := make([]ExperimentAssetRow, 0)
	if preview {
		items, err := asc.ListAppPreviews(ctx, c, set.ID)
		if err != nil {
			return nil, err
		}
		for i := range items {
			p := &items[i]
			rows = append(rows, ExperimentAssetRow{Kind: "preview", DisplayType: set.DisplayType, SetID: set.ID, AssetID: p.ID, FileName: p.FileName, Checksum: p.SourceFileChecksum, State: p.VideoDeliveryState.State})
		}
		return rows, nil
	}
	items, err := asc.ListExperimentScreenshots(ctx, c, set.ID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		s := &items[i]
		rows = append(rows, ExperimentAssetRow{Kind: "screenshot", DisplayType: set.DisplayType, SetID: set.ID, AssetID: s.ID, FileName: s.FileName, Checksum: s.Checksum, State: s.State.State})
	}
	return rows, nil
}

func newExperimentAssetUploadCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "upload <bundleId> <file>", Args: cobra.ExactArgs(2), Short: "Upload a treatment screenshot or preview"}
	experimentAssetFlags(cmd)
	experimentConfirmFlag(cmd)
	experimentAssetPollFlags(cmd)
	cmd.Flags().Bool("resume", false, "resume exact matching pending upload checkpoint")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		preview, display, err := experimentAssetKind(cmd)
		if err != nil {
			return err
		}
		poll, err := experimentAssetPoll(cmd)
		if err != nil {
			return err
		}
		if err := validateScreenshotFile(args[1]); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		exp, _, loc, err := selectedExperimentLocalization(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		resume, _ := cmd.Flags().GetBool("resume")
		out, err := uploadExperimentAsset(cmd.Context(), c, loc.ID, preview, display, args[1], resume, poll)
		if err != nil {
			return err
		}
		return Render(out, outputMode())
	}
	return cmd
}

func uploadExperimentAsset(ctx context.Context, c *asc.Client, locID string, preview bool, display, file string, resume bool, poll asc.AssetPollOptions) (ExperimentAssetAction, error) {
	if poll.MaxAttempts <= 0 || poll.Interval < 0 {
		return ExperimentAssetAction{}, errors.New("invalid processing poll options")
	}
	if err := validateScreenshotFile(file); err != nil {
		return ExperimentAssetAction{}, err
	}
	checksum, err := md5HexOfFile(file)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	var set asc.ExperimentAssetSet
	if resume {
		set, err = selectedExperimentAssetSet(ctx, c, locID, preview, display)
	} else {
		set, _, err = asc.FindOrCreateExperimentAssetSet(ctx, c, locID, display, preview)
	}
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	if preview {
		return uploadExperimentPreview(ctx, c, set.ID, file, checksum, resume, poll)
	}
	return uploadExperimentScreenshot(ctx, c, set.ID, file, checksum, resume, poll)
}

func uploadExperimentScreenshot(ctx context.Context, c *asc.Client, setID, file, checksum string, resume bool, poll asc.AssetPollOptions) (ExperimentAssetAction, error) {
	items, err := asc.ListExperimentScreenshots(ctx, c, setID)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	match, err := matchingExperimentScreenshot(items, filepath.Base(file), checksum)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	if match != nil {
		return reuseExperimentScreenshot(ctx, c, setID, file, checksum, resume, poll, *match)
	}
	if resume {
		return ExperimentAssetAction{}, errors.New("no pending screenshot in selected set for --resume")
	}
	result, err := c.Upload(ctx, asc.UploadOptions{Kind: asc.AssetKindAppScreenshot, ParentID: setID, Asset: asc.UploadAsset{Path: file}, ResumeFromCheckpoint: resume})
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	done, err := asc.WaitExperimentScreenshotProcessing(ctx, c, setID, result.ID, poll)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	return ExperimentAssetAction{Action: "uploaded", Kind: "screenshot", SetID: setID, AssetID: result.ID, Checksum: checksum, State: done.State.State, Changed: true}, nil
}

func matchingExperimentScreenshot(items []asc.ExperimentScreenshot, base, checksum string) (*asc.ExperimentScreenshot, error) {
	var match *asc.ExperimentScreenshot
	for i := range items {
		if items[i].FileName == base || strings.EqualFold(items[i].Checksum, checksum) {
			if match != nil {
				return nil, errors.New("ambiguous screenshots match filename or checksum; inspect selected set")
			}
			match = &items[i]
		}
	}
	return match, nil
}

func reuseExperimentScreenshot(ctx context.Context, c *asc.Client, setID, file, checksum string, resume bool, poll asc.AssetPollOptions, item asc.ExperimentScreenshot) (ExperimentAssetAction, error) {
	if strings.EqualFold(item.Checksum, checksum) && item.State.State != "AWAITING_UPLOAD" {
		done, err := asc.WaitExperimentScreenshotProcessing(ctx, c, setID, item.ID, poll)
		if err != nil {
			return ExperimentAssetAction{}, err
		}
		return ExperimentAssetAction{Action: "existing", Kind: "screenshot", SetID: setID, AssetID: item.ID, Checksum: checksum, State: done.State.State}, nil
	}
	if item.FileName != filepath.Base(file) || item.State.State != "AWAITING_UPLOAD" {
		return ExperimentAssetAction{}, fmt.Errorf("screenshot %s conflicts with selected filename or checksum; inspect before retry", item.ID)
	}
	if !resume {
		return ExperimentAssetAction{}, fmt.Errorf("screenshot %s awaits upload; use --resume with exact checkpoint", item.ID)
	}
	if err := asc.VerifyPendingAssetResume(asc.AssetKindAppScreenshot, file, setID, checksum, item.ID); err != nil {
		return ExperimentAssetAction{}, err
	}
	result, err := c.Upload(ctx, asc.UploadOptions{Kind: asc.AssetKindAppScreenshot, ParentID: setID, Asset: asc.UploadAsset{Path: file}, ResumeFromCheckpoint: true})
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	if result.ID != item.ID {
		return ExperimentAssetAction{}, errors.New("resumed screenshot ID changed; inspect live")
	}
	done, err := asc.WaitExperimentScreenshotProcessing(ctx, c, setID, result.ID, poll)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	return ExperimentAssetAction{Action: "resumed", Kind: "screenshot", SetID: setID, AssetID: result.ID, Checksum: checksum, State: done.State.State, Changed: true}, nil
}

func uploadExperimentPreview(ctx context.Context, c *asc.Client, setID, file, checksum string, resume bool, poll asc.AssetPollOptions) (ExperimentAssetAction, error) {
	items, err := asc.ListAppPreviews(ctx, c, setID)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	match, err := matchingExperimentPreview(items, filepath.Base(file), checksum)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	if match != nil {
		return reuseExperimentPreview(ctx, c, setID, file, checksum, resume, poll, *match)
	}
	if resume {
		return ExperimentAssetAction{}, errors.New("no pending preview in selected set for --resume")
	}
	result, done, err := asc.UploadAppPreviewAndWait(ctx, c, setID, file, resume, poll)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	return ExperimentAssetAction{Action: "uploaded", Kind: "preview", SetID: setID, AssetID: result.ID, Checksum: checksum, State: done.VideoDeliveryState.State, Changed: true}, nil
}

func matchingExperimentPreview(items []asc.AppPreview, base, checksum string) (*asc.AppPreview, error) {
	var match *asc.AppPreview
	for i := range items {
		if items[i].FileName == base || strings.EqualFold(items[i].SourceFileChecksum, checksum) {
			if match != nil {
				return nil, errors.New("ambiguous previews match filename or checksum; inspect selected set")
			}
			match = &items[i]
		}
	}
	return match, nil
}

func reuseExperimentPreview(ctx context.Context, c *asc.Client, setID, file, checksum string, resume bool, poll asc.AssetPollOptions, item asc.AppPreview) (ExperimentAssetAction, error) {
	if strings.EqualFold(item.SourceFileChecksum, checksum) && item.VideoDeliveryState.State != "AWAITING_UPLOAD" {
		done, err := asc.WaitAppPreviewProcessing(ctx, c, item.ID, poll)
		if err != nil {
			return ExperimentAssetAction{}, err
		}
		return ExperimentAssetAction{Action: "existing", Kind: "preview", SetID: setID, AssetID: item.ID, Checksum: checksum, State: done.VideoDeliveryState.State}, nil
	}
	if item.FileName != filepath.Base(file) || item.VideoDeliveryState.State != "AWAITING_UPLOAD" {
		return ExperimentAssetAction{}, fmt.Errorf("preview %s conflicts with selected filename or checksum; inspect before retry", item.ID)
	}
	if !resume {
		return ExperimentAssetAction{}, fmt.Errorf("preview %s awaits upload; use --resume with exact checkpoint", item.ID)
	}
	if err := asc.VerifyPendingAssetResume(asc.AssetKindAppPreview, file, setID, checksum, item.ID); err != nil {
		return ExperimentAssetAction{}, err
	}
	result, done, err := asc.UploadAppPreviewAndWait(ctx, c, setID, file, true, poll)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	if result.ID != item.ID {
		return ExperimentAssetAction{}, errors.New("resumed preview ID changed; inspect live")
	}
	return ExperimentAssetAction{Action: "resumed", Kind: "preview", SetID: setID, AssetID: result.ID, Checksum: checksum, State: done.VideoDeliveryState.State, Changed: true}, nil
}

func newExperimentAssetWaitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "wait <bundleId>", Args: cobra.ExactArgs(1), Short: "Wait for selected treatment media processing"}
	experimentAssetFlags(cmd)
	experimentAssetPollFlags(cmd)
	cmd.Flags().String("asset", "", "asset ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		preview, display, err := experimentAssetKind(cmd)
		if err != nil {
			return err
		}
		poll, err := experimentAssetPoll(cmd)
		if err != nil {
			return err
		}
		assetID, _ := cmd.Flags().GetString("asset")
		if assetID == "" {
			return errors.New("--asset is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		_, _, loc, err := selectedExperimentLocalization(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		set, err := selectedExperimentAssetSet(cmd.Context(), c, loc.ID, preview, display)
		if err != nil {
			return err
		}
		out, err := waitExperimentAsset(cmd.Context(), c, set.ID, assetID, preview, poll)
		if err != nil {
			return err
		}
		return Render(out, outputMode())
	}
	return cmd
}

func waitExperimentAsset(ctx context.Context, c *asc.Client, setID, assetID string, preview bool, poll asc.AssetPollOptions) (ExperimentAssetAction, error) {
	if preview {
		items, err := asc.ListAppPreviews(ctx, c, setID)
		if err != nil {
			return ExperimentAssetAction{}, err
		}
		found := false
		for i := range items {
			if items[i].ID == assetID {
				found = true
			}
		}
		if !found {
			return ExperimentAssetAction{}, errors.New("preview not in selected set")
		}
		done, err := asc.WaitAppPreviewProcessing(ctx, c, assetID, poll)
		if err != nil {
			return ExperimentAssetAction{}, err
		}
		return ExperimentAssetAction{Action: "waited", Kind: "preview", SetID: setID, AssetID: assetID, State: done.VideoDeliveryState.State}, nil
	}
	items, err := asc.ListExperimentScreenshots(ctx, c, setID)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	found := false
	for i := range items {
		if items[i].ID == assetID {
			found = true
		}
	}
	if !found {
		return ExperimentAssetAction{}, errors.New("screenshot not in selected set")
	}
	done, err := asc.WaitExperimentScreenshotProcessing(ctx, c, setID, assetID, poll)
	if err != nil {
		return ExperimentAssetAction{}, err
	}
	return ExperimentAssetAction{Action: "waited", Kind: "screenshot", SetID: setID, AssetID: assetID, State: done.State.State}, nil
}

func selectedExperimentAssetSet(ctx context.Context, c *asc.Client, locID string, preview bool, display string) (asc.ExperimentAssetSet, error) {
	sets, err := asc.ListExperimentAssetSets(ctx, c, locID, preview)
	if err != nil {
		return asc.ExperimentAssetSet{}, err
	}
	var selected *asc.ExperimentAssetSet
	for i := range sets {
		if sets[i].DisplayType == display {
			if selected != nil {
				return asc.ExperimentAssetSet{}, errors.New("duplicate experiment asset sets")
			}
			selected = &sets[i]
		}
	}
	if selected == nil {
		return asc.ExperimentAssetSet{}, errors.New("experiment asset set not found")
	}
	return *selected, nil
}

func newExperimentAssetDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Args: cobra.ExactArgs(1), Short: "Delete selected treatment media"}
	experimentAssetFlags(cmd)
	experimentConfirmFlag(cmd)
	cmd.Flags().String("asset", "", "asset ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		preview, display, err := experimentAssetKind(cmd)
		if err != nil {
			return err
		}
		assetID, _ := cmd.Flags().GetString("asset")
		if assetID == "" {
			return errors.New("--asset is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		exp, _, loc, err := selectedExperimentLocalization(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		set, err := selectedExperimentAssetSet(cmd.Context(), c, loc.ID, preview, display)
		if err != nil {
			return err
		}
		kind := "screenshot"
		if preview {
			kind = "preview"
			err = asc.DeleteAppPreview(cmd.Context(), c, set.ID, assetID)
		} else {
			err = asc.DeleteExperimentScreenshot(cmd.Context(), c, set.ID, assetID)
		}
		if err != nil {
			return err
		}
		return Render(ExperimentAssetAction{Action: "deleted", Kind: kind, SetID: set.ID, AssetID: assetID, Changed: true}, outputMode())
	}
	return cmd
}
