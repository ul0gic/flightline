package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type IAPPromotionImagesResult struct {
	ProductID string                    `json:"productId"`
	Images    []asc.IAPPromotionalImage `json:"images"`
}

func (r IAPPromotionImagesResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Images))
	for _, image := range r.Images {
		rows = append(rows, []string{image.ID, image.FileName, image.State, image.SourceFileChecksum})
	}
	return []string{"ID", "FILE_NAME", "STATE", "CHECKSUM"}, rows
}

type IAPPromotionImageResult struct {
	ProductID string                  `json:"productId"`
	IAPID     string                  `json:"iapId"`
	Image     asc.IAPPromotionalImage `json:"image"`
	NoOp      bool                    `json:"noOp"`
}

func (r IAPPromotionImageResult) TableRows() (headers []string, rows [][]string) {
	return []string{"PRODUCT_ID", "IAP_ID", "IMAGE_ID", "STATE", "NOOP"}, [][]string{{r.ProductID, r.IAPID, r.Image.ID, r.Image.State, strconv.FormatBool(r.NoOp)}}
}

type IAPPromotedListResult struct {
	Promotions []asc.PromotedPurchase `json:"promotions"`
}

func (r IAPPromotedListResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Promotions))
	for _, promo := range r.Promotions {
		rows = append(rows, []string{promo.ID, promo.IAPID, promo.SubscriptionID, boolPtrStr(promo.VisibleForAllUsers), boolPtrStr(promo.Enabled), promo.State})
	}
	return []string{"ID", "IAP_ID", "SUBSCRIPTION_ID", "VISIBLE_FOR_ALL_USERS", "ENABLED", "STATE"}, rows
}

type IAPPromotionActionResult struct {
	Action     string   `json:"action"`
	ID         string   `json:"id,omitempty"`
	NoOp       bool     `json:"noOp"`
	OrderedIDs []string `json:"orderedIds,omitempty"`
}

func (r IAPPromotionActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "NOOP", "ORDERED_IDS"}, [][]string{{r.Action, r.ID, strconv.FormatBool(r.NoOp), fmt.Sprint(r.OrderedIDs)}}
}

// newIAPPromotionCommand is attached under the existing iap command by the lead.
func newIAPPromotionCommand() *cobra.Command {
	group := &cobra.Command{Use: "promotion", Short: "Manage non-subscription IAP promotional images and promoted purchases"}
	group.AddCommand(newIAPPromotionImagesCommand(), newIAPPromotedCommand())
	return group
}

func newIAPPromotionImagesCommand() *cobra.Command {
	group := &cobra.Command{Use: "images", Short: "Manage IAP promotional images"}
	group.AddCommand(&cobra.Command{Use: "list <bundleId> <productId>", Short: "List IAP promotional images", Args: cobra.ExactArgs(2), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotionImagesList(cmd, c, args[0], args[1], outputMode())
	}})
	var confirmUpload, resume bool
	var attempts int
	var interval time.Duration
	upload := &cobra.Command{Use: "upload <bundleId> <productId> <file>", Short: "Upload and process a promotional image", Args: cobra.ExactArgs(3), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotionImageUpload(cmd, c, args[0], args[1], args[2], confirmUpload, resume, asc.AssetPollOptions{MaxAttempts: attempts, Interval: interval}, outputMode())
	}}
	upload.Flags().BoolVar(&confirmUpload, "confirm", false, "confirm promotional image upload")
	upload.Flags().BoolVar(&resume, "resume", false, "resume a matching upload checkpoint")
	upload.Flags().IntVar(&attempts, "poll-attempts", 3, "processing status reads after upload")
	upload.Flags().DurationVar(&interval, "poll-interval", time.Second, "time between processing reads")
	group.AddCommand(upload)
	var waitAttempts int
	var waitInterval time.Duration
	wait := &cobra.Command{Use: "wait <bundleId> <productId> <imageId>", Short: "Inspect promotional image processing", Args: cobra.ExactArgs(3), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotionImageWait(cmd, c, args[0], args[1], args[2], asc.AssetPollOptions{MaxAttempts: waitAttempts, Interval: waitInterval}, outputMode())
	}}
	wait.Flags().IntVar(&waitAttempts, "poll-attempts", 10, "processing status reads")
	wait.Flags().DurationVar(&waitInterval, "poll-interval", time.Second, "time between processing reads")
	group.AddCommand(wait)
	var confirmDelete bool
	del := &cobra.Command{Use: "delete <bundleId> <productId> <imageId>", Short: "Delete a confirmed promotional image", Args: cobra.ExactArgs(3), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotionImageDelete(cmd, c, args[0], args[1], args[2], confirmDelete, outputMode())
	}}
	del.Flags().BoolVar(&confirmDelete, "confirm", false, "confirm promotional image deletion")
	group.AddCommand(del)
	return group
}

func newIAPPromotedCommand() *cobra.Command {
	group := &cobra.Command{Use: "promoted", Short: "Manage app promoted purchases"}
	group.AddCommand(&cobra.Command{Use: "list <bundleId>", Short: "List promoted purchases", Args: cobra.ExactArgs(1), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotedList(cmd, c, args[0], outputMode())
	}})
	var visible, enabled, confirmSet bool
	set := &cobra.Command{Use: "set <bundleId> <productId>", Short: "Create or update a confirmed IAP promotion", Args: cobra.ExactArgs(2), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		var enabledPtr *bool
		if cmd.Flags().Changed("enabled") {
			enabledPtr = &enabled
		}
		return runPromotedSet(cmd, c, args[0], args[1], visible, cmd.Flags().Changed("visible-for-all-users"), enabledPtr, confirmSet, outputMode())
	}}
	set.Flags().BoolVar(&visible, "visible-for-all-users", false, "show promotion to all users")
	set.Flags().BoolVar(&enabled, "enabled", false, "enable promotion")
	set.Flags().BoolVar(&confirmSet, "confirm", false, "confirm promoted purchase change")
	group.AddCommand(set)
	var confirmDelete bool
	del := &cobra.Command{Use: "delete <bundleId> <productId>", Short: "Delete a confirmed IAP promotion", Args: cobra.ExactArgs(2), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotedDelete(cmd, c, args[0], args[1], confirmDelete, outputMode())
	}}
	del.Flags().BoolVar(&confirmDelete, "confirm", false, "confirm promoted purchase deletion")
	group.AddCommand(del)
	var ids []string
	var confirmOrder bool
	order := &cobra.Command{Use: "order <bundleId>", Short: "Set the complete promoted purchase order", Args: cobra.ExactArgs(1), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runPromotedOrder(cmd, c, args[0], ids, confirmOrder, outputMode())
	}}
	order.Flags().StringArrayVar(&ids, "id", nil, "promoted purchase ID in desired order (repeatable; complete app set)")
	order.Flags().BoolVar(&confirmOrder, "confirm", false, "confirm full promoted purchase order")
	group.AddCommand(order)
	return group
}

func promotionApp(ctx context.Context, c *asc.Client, bundleID string) (string, error) {
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return "", err
	}
	app, err := asc.Get[asc.Single[AppAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(appID), nil)
	if err != nil {
		return "", err
	}
	if app.Data.Type != "apps" || app.Data.ID != appID || !isNumericAppID(bundleID) && app.Data.Attributes.BundleID != bundleID {
		return "", fmt.Errorf("promotion: app %q identity is inconsistent", bundleID)
	}
	return appID, nil
}

func promotionTarget(ctx context.Context, c *asc.Client, bundleID, productID string) (appID, iapID string, err error) {
	appID, err = promotionApp(ctx, c, bundleID)
	if err != nil {
		return "", "", err
	}
	query := url.Values{"filter[productId]": {productID}, "limit": {"200"}}
	path := "/v1/apps/" + url.PathEscape(appID) + "/inAppPurchasesV2"
	for page, err := range asc.Pages[asc.IAPAttributes](ctx, c, path, query) {
		if err != nil {
			return "", "", err
		}
		for _, row := range page.Data {
			if row.Type != "inAppPurchases" || row.ID == "" || row.Attributes.ProductID != productID || iapID != "" {
				return "", "", fmt.Errorf("promotion: product %q identity is missing, ambiguous, or inconsistent", productID)
			}
			iapID = row.ID
		}
	}
	if iapID == "" {
		return "", "", fmt.Errorf("promotion: product %q was not found in app %q", productID, bundleID)
	}
	return appID, iapID, nil
}

func runPromotionImagesList(cmd *cobra.Command, c *asc.Client, bundleID, productID, output string) error {
	_, iapID, err := promotionTarget(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}
	images, err := asc.ListIAPPromotionalImages(cmd.Context(), c, iapID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), IAPPromotionImagesResult{ProductID: productID, Images: images}, output, true)
}

func runPromotionImageUpload(cmd *cobra.Command, c *asc.Client, bundleID, productID, file string, confirm, resume bool, poll asc.AssetPollOptions, output string) error {
	if !confirm {
		return errors.New("promotion image upload requires --confirm")
	}
	if poll.MaxAttempts <= 0 || poll.Interval < 0 {
		return errors.New("promotion image upload requires positive poll attempts and nonnegative interval")
	}
	_, iapID, err := promotionTarget(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}
	checksum, err := fileMD5Hex(file)
	if err != nil {
		return err
	}
	images, err := asc.ListIAPPromotionalImages(cmd.Context(), c, iapID)
	if err != nil {
		return err
	}
	existing, err := promotionImageUploadDecision(images, file, iapID, checksum, resume)
	if err != nil {
		return err
	}
	if existing != nil {
		return renderTo(cmd.OutOrStdout(), IAPPromotionImageResult{ProductID: productID, IAPID: iapID, Image: *existing, NoOp: true}, output, true)
	}
	res, err := c.Upload(cmd.Context(), asc.UploadOptions{Kind: asc.AssetKindIAPPromotionalImage, ParentID: iapID, Asset: asc.UploadAsset{Path: file}, ResumeFromCheckpoint: resume})
	if err != nil {
		return fmt.Errorf("promotion image upload did not complete; inspect image list and checkpoint before retrying (failure type %T)", err)
	}
	image, waitErr := asc.WaitIAPPromotionalImage(cmd.Context(), c, res.ID, poll)
	image.ID = res.ID
	image.SourceFileChecksum = res.Checksum
	if renderErr := renderTo(cmd.OutOrStdout(), IAPPromotionImageResult{ProductID: productID, IAPID: iapID, Image: image}, output, true); renderErr != nil {
		return renderErr
	}
	return waitErr
}

func promotionImageUploadDecision(images []asc.IAPPromotionalImage, file, iapID, checksum string, resume bool) (*asc.IAPPromotionalImage, error) {
	var existing *asc.IAPPromotionalImage
	for _, image := range images {
		if image.FileName != baseFileName(file) {
			continue
		}
		if existing != nil {
			return nil, errors.New("promotion image filename has multiple assets; inspect before retrying")
		}
		selected := image
		existing = &selected
	}
	if existing == nil {
		if resume {
			return nil, errors.New("no observed pending promotion image matches this file; --resume would risk a new reservation")
		}
		return nil, nil
	}
	return assessPromotionExistingImage(existing, file, iapID, checksum, resume)
}

func assessPromotionExistingImage(existing *asc.IAPPromotionalImage, file, iapID, checksum string, resume bool) (*asc.IAPPromotionalImage, error) {
	if existing.State == "AWAITING_UPLOAD" || existing.State == "UPLOAD_COMPLETE" {
		if !resume {
			return nil, fmt.Errorf("promotion image %s is pending; use images wait or --resume with a matching checkpoint", existing.ID)
		}
		return nil, asc.VerifyIAPPromotionalImageResume(file, iapID, checksum, existing.ID)
	}
	if existing.SourceFileChecksum == "" {
		return nil, fmt.Errorf("promotion image %s has unknown checksum; inspect before retrying upload", existing.ID)
	}
	if existing.SourceFileChecksum != checksum {
		return nil, fmt.Errorf("promotion image %s already uses this filename with different content; delete explicitly before replacement", existing.ID)
	}
	if existing.State == "FAILED" {
		return nil, fmt.Errorf("promotion image %s failed processing; inspect or delete explicitly", existing.ID)
	}
	if existing.State != "PREPARE_FOR_SUBMISSION" && existing.State != "WAITING_FOR_REVIEW" && existing.State != "APPROVED" && existing.State != "REJECTED" {
		return nil, fmt.Errorf("promotion image %s has unknown state %q; inspect before upload", existing.ID, existing.State)
	}
	return existing, nil
}

func runPromotionImageWait(cmd *cobra.Command, c *asc.Client, bundleID, productID, imageID string, poll asc.AssetPollOptions, output string) error {
	_, iapID, err := promotionTarget(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}
	images, err := asc.ListIAPPromotionalImages(cmd.Context(), c, iapID)
	if err != nil {
		return err
	}
	if !containsPromotionImage(images, imageID) {
		return fmt.Errorf("promotion image %s is not owned by product %s", imageID, productID)
	}
	image, waitErr := asc.WaitIAPPromotionalImage(cmd.Context(), c, imageID, poll)
	image.ID = imageID
	if renderErr := renderTo(cmd.OutOrStdout(), IAPPromotionImageResult{ProductID: productID, IAPID: iapID, Image: image}, output, true); renderErr != nil {
		return renderErr
	}
	return waitErr
}

func runPromotionImageDelete(cmd *cobra.Command, c *asc.Client, bundleID, productID, imageID string, confirm bool, output string) error {
	if !confirm {
		return errors.New("promotion image deletion requires --confirm")
	}
	_, iapID, err := promotionTarget(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}
	images, err := asc.ListIAPPromotionalImages(cmd.Context(), c, iapID)
	if err != nil {
		return err
	}
	if !containsPromotionImage(images, imageID) {
		return fmt.Errorf("promotion image %s is not owned by product %s", imageID, productID)
	}
	writeErr := asc.DeleteIAPPromotionalImage(cmd.Context(), c, imageID)
	after, readErr := asc.ListIAPPromotionalImages(cmd.Context(), c, iapID)
	if readErr == nil && !containsPromotionImage(after, imageID) {
		return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "delete-image", ID: imageID}, output, true)
	}
	if writeErr != nil {
		return fmt.Errorf("promotion image deletion unconfirmed; inspect before retrying: %w", writeErr)
	}
	if readErr != nil {
		return fmt.Errorf("promotion image deletion unconfirmed; inspect before retrying: %w", readErr)
	}
	return errors.New("promotion image deletion did not converge")
}

func containsPromotionImage(images []asc.IAPPromotionalImage, id string) bool {
	for _, image := range images {
		if image.ID == id {
			return true
		}
	}
	return false
}

func runPromotedList(cmd *cobra.Command, c *asc.Client, bundleID, output string) error {
	appID, err := promotionApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	rows, err := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), IAPPromotedListResult{Promotions: rows}, output, true)
}

func findIAPPromotion(rows []asc.PromotedPurchase, iapID string) (*asc.PromotedPurchase, error) {
	var found *asc.PromotedPurchase
	for i := range rows {
		if rows[i].IAPID != iapID {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("IAP %s has multiple promoted purchases", iapID)
		}
		found = &rows[i]
	}
	return found, nil
}

func runPromotedSet(cmd *cobra.Command, c *asc.Client, bundleID, productID string, visible, visibleSet bool, enabled *bool, confirm bool, output string) error {
	if !confirm || !visibleSet {
		return errors.New("promoted set requires --visible-for-all-users and --confirm")
	}
	appID, iapID, err := promotionTarget(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}
	rows, err := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if err != nil {
		return err
	}
	current, err := findIAPPromotion(rows, iapID)
	if err != nil {
		return err
	}
	if promotedMatches(current, visible, enabled) {
		return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "set", ID: current.ID, NoOp: true}, output, true)
	}
	writeErr := writePromotedSet(cmd.Context(), c, appID, iapID, current, visible, enabled)
	after, readErr := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if readErr != nil {
		return fmt.Errorf("promoted purchase write outcome unconfirmed; inspect before retrying: %w", readErr)
	}
	actual, err := findIAPPromotion(after, iapID)
	if err == nil && promotedMatches(actual, visible, enabled) {
		return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "set", ID: actual.ID}, output, true)
	}
	if writeErr != nil {
		return fmt.Errorf("promoted purchase write outcome unconfirmed; inspect before retrying: %w", writeErr)
	}
	if err != nil {
		return err
	}
	return errors.New("promoted purchase write did not converge")
}

func promotedMatches(current *asc.PromotedPurchase, visible bool, enabled *bool) bool {
	return current != nil && current.VisibleForAllUsers != nil && *current.VisibleForAllUsers == visible &&
		(enabled == nil || current.Enabled != nil && *current.Enabled == *enabled)
}

func writePromotedSet(ctx context.Context, c *asc.Client, appID, iapID string, current *asc.PromotedPurchase, visible bool, enabled *bool) error {
	if current == nil {
		_, err := asc.CreateIAPPromotedPurchase(ctx, c, appID, iapID, visible, enabled)
		return err
	}
	var visiblePatch *bool
	if current.VisibleForAllUsers == nil || *current.VisibleForAllUsers != visible {
		visiblePatch = &visible
	}
	var enabledPatch *bool
	if enabled != nil && (current.Enabled == nil || *current.Enabled != *enabled) {
		enabledPatch = enabled
	}
	return asc.PatchPromotedPurchase(ctx, c, current.ID, visiblePatch, enabledPatch)
}

func runPromotedDelete(cmd *cobra.Command, c *asc.Client, bundleID, productID string, confirm bool, output string) error {
	if !confirm {
		return errors.New("promoted purchase deletion requires --confirm")
	}
	appID, iapID, err := promotionTarget(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}
	rows, err := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if err != nil {
		return err
	}
	current, err := findIAPPromotion(rows, iapID)
	if err != nil {
		return err
	}
	if current == nil {
		return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "delete", NoOp: true}, output, true)
	}
	writeErr := asc.DeletePromotedPurchase(cmd.Context(), c, current.ID)
	after, readErr := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if readErr == nil {
		actual, err := findIAPPromotion(after, iapID)
		if err == nil && actual == nil {
			return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "delete", ID: current.ID}, output, true)
		}
	}
	if writeErr != nil {
		return fmt.Errorf("promoted purchase deletion outcome unconfirmed; inspect before retrying: %w", writeErr)
	}
	if readErr != nil {
		return fmt.Errorf("promoted purchase deletion outcome unconfirmed; inspect before retrying: %w", readErr)
	}
	return errors.New("promoted purchase deletion did not converge")
}

func runPromotedOrder(cmd *cobra.Command, c *asc.Client, bundleID string, ids []string, confirm bool, output string) error {
	if !confirm {
		return errors.New("promoted purchase order requires --confirm")
	}
	appID, err := promotionApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	rows, err := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if err != nil {
		return err
	}
	if err := validatePromotionOrder(rows, ids); err != nil {
		return err
	}
	current := promotionIDs(rows)
	if slices.Equal(current, ids) {
		return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "order", NoOp: true, OrderedIDs: ids}, output, true)
	}
	writeErr := asc.OrderAppPromotedPurchases(cmd.Context(), c, appID, ids)
	after, readErr := asc.ListAppPromotedPurchases(cmd.Context(), c, appID)
	if readErr == nil && slices.Equal(promotionIDs(after), ids) {
		return renderTo(cmd.OutOrStdout(), IAPPromotionActionResult{Action: "order", OrderedIDs: ids}, output, true)
	}
	if writeErr != nil {
		return fmt.Errorf("promoted purchase order outcome unconfirmed; inspect before retrying: %w", writeErr)
	}
	if readErr != nil {
		return fmt.Errorf("promoted purchase order outcome unconfirmed; inspect before retrying: %w", readErr)
	}
	return errors.New("promoted purchase order did not converge")
}

func promotionIDs(rows []asc.PromotedPurchase) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func validatePromotionOrder(rows []asc.PromotedPurchase, ids []string) error {
	if len(rows) != len(ids) {
		return errors.New("promotion order must include every app promoted purchase, including subscriptions")
	}
	have := map[string]bool{}
	for _, row := range rows {
		have[row.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !have[id] || seen[id] {
			return fmt.Errorf("promotion order includes unknown or duplicate ID %q", id)
		}
		seen[id] = true
	}
	return nil
}
