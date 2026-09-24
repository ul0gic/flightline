package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type AppPreviewRow struct {
	PreviewType string         `json:"previewType"`
	SetID       string         `json:"setId"`
	Preview     asc.AppPreview `json:"preview"`
}

type AppPreviewsResult struct {
	BundleID string          `json:"bundleId"`
	Locale   string          `json:"locale"`
	Page     string          `json:"page,omitempty"`
	Previews []AppPreviewRow `json:"previews"`
}

func (r AppPreviewsResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"TYPE", "SET_ID", "PREVIEW_ID", "FILE", "STATE", "CHECKSUM"}
	rows = make([][]string, 0, len(r.Previews))
	for index := range r.Previews {
		item := &r.Previews[index]
		rows = append(rows, []string{item.PreviewType, item.SetID, item.Preview.ID, item.Preview.FileName, item.Preview.VideoDeliveryState.State, item.Preview.SourceFileChecksum})
	}
	return headers, rows
}

type AppPreviewActionResult struct {
	Action    string `json:"action"`
	BundleID  string `json:"bundleId"`
	SetID     string `json:"setId"`
	PreviewID string `json:"previewId"`
	Checksum  string `json:"checksum,omitempty"`
	State     string `json:"state,omitempty"`
	Changed   bool   `json:"changed"`
}

func (r AppPreviewActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "SET_ID", "PREVIEW_ID", "STATE", "CHANGED"}, [][]string{{r.Action, r.SetID, r.PreviewID, r.State, strconv.FormatBool(r.Changed)}}
}

func newAppPreviewsCommand() *cobra.Command {
	root := &cobra.Command{Use: "previews", Short: "Manage App Store preview videos"}
	root.AddCommand(newAppPreviewsListCommand(), newAppPreviewsUploadCommand(), newAppPreviewsWaitCommand(), newAppPreviewsDeleteCommand())
	return root
}

func previewTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("version", "", "App Store version string for the main listing")
	cmd.Flags().String("platform", "IOS", "App Store platform")
	cmd.Flags().String("locale", "", "listing locale")
	cmd.Flags().String("page", "", "custom product page name; omit for the main listing")
}

func previewPollFlags(cmd *cobra.Command) {
	cmd.Flags().Int("max-polls", 20, "maximum processing status reads")
	cmd.Flags().Duration("poll-interval", 3*time.Second, "time between processing status reads")
}

func previewPollOptions(cmd *cobra.Command) asc.AssetPollOptions {
	attempts, _ := cmd.Flags().GetInt("max-polls")
	interval, _ := cmd.Flags().GetDuration("poll-interval")
	return asc.AssetPollOptions{MaxAttempts: attempts, Interval: interval}
}

func newAppPreviewsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Short: "List preview sets and videos for a main or custom page locale", Args: cobra.ExactArgs(1)}
	previewTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		parent, locale, page, err := resolvePreviewParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		rows, err := listPreviewRows(cmd.Context(), c, parent)
		if err != nil {
			return err
		}
		return Render(AppPreviewsResult{BundleID: args[0], Locale: locale, Page: page, Previews: rows}, outputMode())
	}
	return cmd
}

func newAppPreviewsUploadCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "upload <bundleId> <file>", Short: "Upload one preview and wait for video processing", Args: cobra.ExactArgs(2)}
	previewTargetFlags(cmd)
	previewPollFlags(cmd)
	cmd.Flags().String("type", "", "PreviewType such as IPHONE_67 or IPAD_PRO_3GEN_129")
	cmd.Flags().String("frame-time-code", "", "optional preview frame time code, applied after upload")
	cmd.Flags().Bool("resume", false, "resume a matching upload checkpoint")
	cmd.Flags().Bool("confirm", false, "confirm preview upload")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requirePreviewConfirm(cmd); err != nil {
			return err
		}
		previewType, _ := cmd.Flags().GetString("type")
		if !asc.ValidPreviewType(previewType) {
			return fmt.Errorf("invalid preview type %q", previewType)
		}
		if err := validateScreenshotFile(args[1]); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		parent, err := resolvePreviewMutationParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		resume, _ := cmd.Flags().GetBool("resume")
		frame, _ := cmd.Flags().GetString("frame-time-code")
		out, err := uploadPreviewWithClient(cmd.Context(), c, parent, previewType, args[1], frame, resume, previewPollOptions(cmd))
		if err != nil {
			return err
		}
		out.BundleID = args[0]
		return Render(out, outputMode())
	}
	return cmd
}

func newAppPreviewsWaitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "wait <bundleId>", Short: "Wait for an existing preview by ID without uploading again", Args: cobra.ExactArgs(1)}
	previewTargetFlags(cmd)
	previewPollFlags(cmd)
	cmd.Flags().String("preview", "", "preview resource ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		previewID, _ := cmd.Flags().GetString("preview")
		if previewID == "" {
			return errors.New("wait requires --preview")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		parent, _, _, err := resolvePreviewParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		row, err := findPreviewRow(cmd.Context(), c, parent, previewID)
		if err != nil {
			return err
		}
		preview, err := asc.WaitAppPreviewProcessing(cmd.Context(), c, previewID, previewPollOptions(cmd))
		if err != nil {
			return err
		}
		return Render(AppPreviewActionResult{Action: "wait", BundleID: args[0], SetID: row.SetID, PreviewID: previewID, Checksum: preview.SourceFileChecksum, State: preview.VideoDeliveryState.State}, outputMode())
	}
	return cmd
}

func newAppPreviewsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Short: "Delete one preview from its verified listing", Args: cobra.ExactArgs(1)}
	previewTargetFlags(cmd)
	cmd.Flags().String("preview", "", "preview resource ID")
	cmd.Flags().Bool("confirm", false, "confirm preview deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requirePreviewConfirm(cmd); err != nil {
			return err
		}
		previewID, _ := cmd.Flags().GetString("preview")
		if previewID == "" {
			return errors.New("delete requires --preview")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		parent, err := resolvePreviewMutationParent(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		row, err := findPreviewRow(cmd.Context(), c, parent, previewID)
		if err != nil {
			return err
		}
		if err := asc.DeleteAppPreview(cmd.Context(), c, row.SetID, previewID); err != nil {
			return err
		}
		return Render(AppPreviewActionResult{Action: "delete", BundleID: args[0], SetID: row.SetID, PreviewID: previewID, Changed: true}, outputMode())
	}
	return cmd
}

func requirePreviewConfirm(cmd *cobra.Command) error {
	confirmed, _ := cmd.Flags().GetBool("confirm")
	if !confirmed {
		return errors.New("preview mutation requires --confirm")
	}
	return nil
}

func listPreviewRows(ctx context.Context, c *asc.Client, parent asc.PreviewParent) ([]AppPreviewRow, error) {
	sets, err := asc.ListAppPreviewSets(ctx, c, parent)
	if err != nil {
		return nil, err
	}
	rows := make([]AppPreviewRow, 0)
	for index := range sets {
		set := &sets[index]
		previews, err := asc.ListAppPreviews(ctx, c, set.ID)
		if err != nil {
			return nil, err
		}
		for previewIndex := range previews {
			rows = append(rows, AppPreviewRow{PreviewType: set.PreviewType, SetID: set.ID, Preview: previews[previewIndex]})
		}
	}
	return rows, nil
}

func findPreviewRow(ctx context.Context, c *asc.Client, parent asc.PreviewParent, previewID string) (AppPreviewRow, error) {
	rows, err := listPreviewRows(ctx, c, parent)
	if err != nil {
		return AppPreviewRow{}, err
	}
	for index := range rows {
		if rows[index].Preview.ID == previewID {
			return rows[index], nil
		}
	}
	return AppPreviewRow{}, fmt.Errorf("preview %s does not belong to requested listing", previewID)
}

func uploadPreviewWithClient(ctx context.Context, c *asc.Client, parent asc.PreviewParent, previewType, filePath, frame string, resume bool, poll asc.AssetPollOptions) (AppPreviewActionResult, error) {
	if err := validatePreviewPoll(poll); err != nil {
		return AppPreviewActionResult{}, err
	}
	checksum, err := md5HexOfFile(filePath)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	sets, err := asc.ListAppPreviewSets(ctx, c, parent)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	selected, err := selectPreviewSet(sets, previewType)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	if selected != nil {
		result, found, err := reusePreviewUpload(ctx, c, *selected, filePath, checksum, frame, resume, poll)
		if err != nil || found {
			return result, err
		}
	}
	if resume {
		return AppPreviewActionResult{}, errors.New("no matching pending preview to resume; inspect listing before another upload")
	}
	set, _, err := asc.FindOrCreateAppPreviewSet(ctx, c, parent, previewType)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	result, preview, err := asc.UploadAppPreviewAndWait(ctx, c, set.ID, filePath, resume, poll)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	preview, err = finalizePreviewFrame(ctx, c, result.ID, frame, preview, poll)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	return AppPreviewActionResult{Action: "uploaded", SetID: set.ID, PreviewID: result.ID, Checksum: result.Checksum, State: preview.VideoDeliveryState.State, Changed: true}, nil
}

func selectPreviewSet(sets []asc.AppPreviewSet, previewType string) (*asc.AppPreviewSet, error) {
	var selected *asc.AppPreviewSet
	for index := range sets {
		if sets[index].PreviewType != previewType {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("duplicate preview sets for %s", previewType)
		}
		selected = &sets[index]
	}
	return selected, nil
}

func reusePreviewUpload(ctx context.Context, c *asc.Client, set asc.AppPreviewSet, filePath, checksum, frame string, resume bool, poll asc.AssetPollOptions) (AppPreviewActionResult, bool, error) {
	previews, err := asc.ListAppPreviews(ctx, c, set.ID)
	if err != nil {
		return AppPreviewActionResult{}, false, err
	}
	for index := range previews {
		preview := &previews[index]
		if filepath.Base(filePath) == preview.FileName && preview.VideoDeliveryState.State == "AWAITING_UPLOAD" {
			result, err := resumePreviewUpload(ctx, c, set.ID, preview.ID, filePath, checksum, frame, resume, poll)
			return result, true, err
		}
		if strings.EqualFold(preview.SourceFileChecksum, checksum) {
			result, err := reuseCompletePreview(ctx, c, set.ID, preview.ID, checksum, frame, poll)
			return result, true, err
		}
		if filepath.Base(filePath) == preview.FileName {
			return AppPreviewActionResult{}, true, fmt.Errorf("preview %s with the same filename is %s; inspect or wait by ID before another upload", preview.ID, preview.VideoDeliveryState.State)
		}
	}
	return AppPreviewActionResult{}, false, nil
}

func resumePreviewUpload(ctx context.Context, c *asc.Client, setID, previewID, filePath, checksum, frame string, resume bool, poll asc.AssetPollOptions) (AppPreviewActionResult, error) {
	if !resume {
		return AppPreviewActionResult{}, fmt.Errorf("preview %s is awaiting upload; use --resume with its checkpoint or inspect by ID", previewID)
	}
	if err := asc.VerifyPendingAssetResume(asc.AssetKindAppPreview, filePath, setID, checksum, previewID); err != nil {
		return AppPreviewActionResult{}, err
	}
	result, complete, err := asc.UploadAppPreviewAndWait(ctx, c, setID, filePath, true, poll)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	if result.ID != previewID {
		return AppPreviewActionResult{}, fmt.Errorf("resumed preview ID changed from %s to %s; inspect before retrying", previewID, result.ID)
	}
	complete, err = finalizePreviewFrame(ctx, c, previewID, frame, complete, poll)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	return AppPreviewActionResult{Action: "resumed", SetID: setID, PreviewID: previewID, Checksum: checksum, State: complete.VideoDeliveryState.State, Changed: true}, nil
}

func reuseCompletePreview(ctx context.Context, c *asc.Client, setID, previewID, checksum, frame string, poll asc.AssetPollOptions) (AppPreviewActionResult, error) {
	complete, err := asc.WaitAppPreviewProcessing(ctx, c, previewID, poll)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	frameChanged := frame != "" && complete.PreviewFrameTimeCode != frame
	complete, err = finalizePreviewFrame(ctx, c, previewID, frame, complete, poll)
	if err != nil {
		return AppPreviewActionResult{}, err
	}
	action := "skipped"
	if frameChanged {
		action = "updated"
	}
	return AppPreviewActionResult{Action: action, SetID: setID, PreviewID: previewID, Checksum: checksum, State: complete.VideoDeliveryState.State, Changed: frameChanged}, nil
}

func validatePreviewPoll(poll asc.AssetPollOptions) error {
	if poll.MaxAttempts <= 0 || poll.Interval < 0 {
		return errors.New("preview polling requires positive max-polls and nonnegative poll-interval")
	}
	return nil
}

func finalizePreviewFrame(ctx context.Context, c *asc.Client, previewID, frame string, preview asc.AppPreview, poll asc.AssetPollOptions) (asc.AppPreview, error) {
	if frame == "" || preview.PreviewFrameTimeCode == frame {
		return preview, nil
	}
	if _, err := asc.SetAppPreviewFrame(ctx, c, previewID, frame); err != nil {
		return asc.AppPreview{}, err
	}
	return asc.WaitAppPreviewProcessing(ctx, c, previewID, poll)
}

func resolvePreviewParent(ctx context.Context, c *asc.Client, cmd *cobra.Command, bundleID string) (parent asc.PreviewParent, locale, page string, err error) {
	locale, _ = cmd.Flags().GetString("locale")
	pageName, _ := cmd.Flags().GetString("page")
	if locale == "" {
		return asc.PreviewParent{}, "", "", errors.New("previews require --locale")
	}
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return asc.PreviewParent{}, "", "", err
	}
	if pageName != "" {
		parent, err := resolveCPPPreviewParent(ctx, c, appID, pageName, locale)
		return parent, locale, pageName, err
	}
	version, _ := cmd.Flags().GetString("version")
	platform, _ := cmd.Flags().GetString("platform")
	if version == "" {
		return asc.PreviewParent{}, "", "", errors.New("main listing previews require --version")
	}
	parent, err = resolveMainPreviewParent(ctx, c, appID, version, platform, locale)
	return parent, locale, "", err
}

func resolvePreviewMutationParent(ctx context.Context, c *asc.Client, cmd *cobra.Command, bundleID string) (asc.PreviewParent, error) {
	parent, locale, page, err := resolvePreviewParent(ctx, c, cmd, bundleID)
	if err != nil || page == "" {
		return parent, err
	}
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	return resolveEditableCPPPreviewParent(ctx, c, appID, page, locale)
}

func resolveEditableCPPPreviewParent(ctx context.Context, c *asc.Client, appID, pageName, locale string) (asc.PreviewParent, error) {
	pageID, err := resolveUniqueCPPPageID(ctx, c, appID, pageName)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	versions, err := collectCustomProductPageVersions(ctx, c, pageID)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	versionID := ""
	for index := range versions {
		version := &versions[index]
		if version.Attributes.State != "PREPARE_FOR_SUBMISSION" && version.Attributes.State != "REJECTED" {
			continue
		}
		if versionID != "" {
			return asc.PreviewParent{}, errors.New("multiple editable custom product page versions")
		}
		versionID = version.ID
	}
	if versionID == "" {
		return asc.PreviewParent{}, errors.New("custom product page has no editable version; preview mutation is unavailable")
	}
	localizations, err := collectCustomProductPageLocalizations(ctx, c, versionID)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	selected := ""
	for index := range localizations {
		if localizations[index].Attributes.Locale != locale {
			continue
		}
		if selected != "" {
			return asc.PreviewParent{}, fmt.Errorf("duplicate CPP localization %s", locale)
		}
		selected = localizations[index].ID
	}
	if selected == "" {
		return asc.PreviewParent{}, fmt.Errorf("editable CPP localization %s not found", locale)
	}
	return asc.PreviewParent{Type: "appCustomProductPageLocalizations", ID: selected}, nil
}

func resolveMainPreviewParent(ctx context.Context, c *asc.Client, appID, version, platform, locale string) (asc.PreviewParent, error) {
	versionID, err := resolveUniqueAppStoreVersionID(ctx, c, appID, version, platform)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	locID, err := resolveUniqueVersionLocale(ctx, c, versionID, locale)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	return asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: locID}, nil
}

func resolveUniqueAppStoreVersionID(ctx context.Context, c *asc.Client, appID, version, platform string) (string, error) {
	q := url.Values{"filter[versionString]": {version}, "filter[platform]": {platform}, "limit": {"200"}}
	versionID := ""
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appStoreVersions", q) {
		if err != nil {
			return "", err
		}
		for _, row := range page.Data {
			if row.Attributes.VersionString != version || row.Attributes.Platform != platform {
				continue
			}
			if versionID != "" {
				return "", errors.New("multiple matching App Store versions")
			}
			versionID = row.ID
		}
	}
	if versionID == "" {
		return "", fmt.Errorf("app store version %s/%s not found", version, platform)
	}
	return versionID, nil
}

func resolveUniqueVersionLocale(ctx context.Context, c *asc.Client, versionID, locale string) (string, error) {
	q := url.Values{"filter[locale]": {locale}, "limit": {"200"}}
	selected := ""
	for page, err := range asc.Pages[metadataASCVersionLocalizationAttrs](ctx, c, "/v1/appStoreVersions/"+url.PathEscape(versionID)+"/appStoreVersionLocalizations", q) {
		if err != nil {
			return "", err
		}
		for _, row := range page.Data {
			if row.Attributes.Locale != locale {
				continue
			}
			if selected != "" {
				return "", fmt.Errorf("duplicate version localization for %s", locale)
			}
			selected = row.ID
		}
	}
	if selected == "" {
		return "", fmt.Errorf("version localization %s not found", locale)
	}
	return selected, nil
}

func resolveCPPPreviewParent(ctx context.Context, c *asc.Client, appID, pageName, locale string) (asc.PreviewParent, error) {
	pageID, err := resolveUniqueCPPPageID(ctx, c, appID, pageName)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	versions, err := collectCustomProductPageVersions(ctx, c, pageID)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	versionID, err := highestCPPVersionID(versions)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	localizations, err := collectCustomProductPageLocalizations(ctx, c, versionID)
	if err != nil {
		return asc.PreviewParent{}, err
	}
	selected := ""
	for index := range localizations {
		if localizations[index].Attributes.Locale != locale {
			continue
		}
		if selected != "" {
			return asc.PreviewParent{}, fmt.Errorf("duplicate CPP localization %s", locale)
		}
		selected = localizations[index].ID
	}
	if selected == "" {
		return asc.PreviewParent{}, fmt.Errorf("CPP localization %s not found", locale)
	}
	return asc.PreviewParent{Type: "appCustomProductPageLocalizations", ID: selected}, nil
}

func resolveUniqueCPPPageID(ctx context.Context, c *asc.Client, appID, pageName string) (string, error) {
	pages, err := collectCustomProductPages(ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appCustomProductPages", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		return "", err
	}
	pageID := ""
	for _, page := range pages {
		if page.Attributes.Name != pageName {
			continue
		}
		if pageID != "" {
			return "", fmt.Errorf("duplicate custom product page name %s", pageName)
		}
		pageID = page.ID
	}
	if pageID == "" {
		return "", fmt.Errorf("custom product page %s not found", pageName)
	}
	return pageID, nil
}
