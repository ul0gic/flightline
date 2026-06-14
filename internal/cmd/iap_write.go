package cmd

import (
	"context"
	"crypto/md5" //nolint:gosec // Apple's API contract requires MD5 for upload integrity (sourceFileChecksum)
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// iapCreateRequest is the wire body for POST /v2/inAppPurchases. Apple requires
// productId, name, inAppPurchaseType, and the app relationship.
type iapCreateRequest struct {
	Data iapCreateData `json:"data"`
}

type iapCreateData struct {
	Type          string                 `json:"type"`
	Attributes    iapCreateAttrs         `json:"attributes"`
	Relationships map[string]relRefBlock `json:"relationships"`
}

type iapCreateAttrs struct {
	Name              string `json:"name"`
	ProductID         string `json:"productId"`
	InAppPurchaseType string `json:"inAppPurchaseType"`
	ReviewNote        string `json:"reviewNote,omitempty"`
	FamilySharable    *bool  `json:"familySharable,omitempty"`
}

// iapUpdateRequest is the PATCH body for /v2/inAppPurchases/{id}; productId and
// inAppPurchaseType are immutable post-create.
type iapUpdateRequest struct {
	Data iapUpdateData `json:"data"`
}

type iapUpdateData struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Attributes iapUpdateAttrs `json:"attributes"`
}

type iapUpdateAttrs struct {
	Name           *string `json:"name,omitempty"`
	ReviewNote     *string `json:"reviewNote,omitempty"`
	FamilySharable *bool   `json:"familySharable,omitempty"`
}

// iapLocalizationCreateRequest is the POST body for /v1/inAppPurchaseLocalizations.
type iapLocalizationCreateRequest struct {
	Data iapLocalizationCreateData `json:"data"`
}

type iapLocalizationCreateData struct {
	Type          string                     `json:"type"`
	Attributes    iapLocalizationCreateAttrs `json:"attributes"`
	Relationships map[string]relRefBlock     `json:"relationships"`
}

type iapLocalizationCreateAttrs struct {
	Name        string `json:"name"`
	Locale      string `json:"locale"`
	Description string `json:"description,omitempty"`
}

// iapLocalizationUpdateRequest is the PATCH body for
// /v1/inAppPurchaseLocalizations/{id}; locale is immutable.
type iapLocalizationUpdateRequest struct {
	Data iapLocalizationUpdateData `json:"data"`
}

type iapLocalizationUpdateData struct {
	Type       string                     `json:"type"`
	ID         string                     `json:"id"`
	Attributes iapLocalizationUpdateAttrs `json:"attributes"`
}

type iapLocalizationUpdateAttrs struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type relRefBlock struct {
	Data relRef `json:"data"`
}

type relRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// IAPWriteResult is the JSON-stable view for iap create/update/delete;
// noop=true means current state already matched.
type IAPWriteResult struct {
	Action     string            `json:"action"`
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	ProductID  string            `json:"productId,omitempty"`
	NoOp       bool              `json:"noop"`
	Attributes asc.IAPAttributes `json:"attributes,omitempty"`
}

func (r *IAPWriteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"ID", r.ID},
		{"TYPE", r.Type},
		{"PRODUCT_ID", r.ProductID},
		{"NOOP", strconv.FormatBool(r.NoOp)},
		{"NAME", r.Attributes.Name},
		{"IAP_TYPE", r.Attributes.InAppPurchaseType},
		{"STATE", r.Attributes.State},
		{"REVIEW_NOTE", r.Attributes.ReviewNote},
		{"FAMILY_SHARABLE", boolPtrStr(r.Attributes.FamilySharable)},
	}
	return headers, rows
}

type IAPLocalizationWriteResult struct {
	Action     string                        `json:"action"`
	ID         string                        `json:"id"`
	Type       string                        `json:"type"`
	NoOp       bool                          `json:"noop"`
	Attributes asc.IAPLocalizationAttributes `json:"attributes,omitempty"`
}

func (r *IAPLocalizationWriteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"ID", r.ID},
		{"TYPE", r.Type},
		{"NOOP", strconv.FormatBool(r.NoOp)},
		{"LOCALE", r.Attributes.Locale},
		{"NAME", r.Attributes.Name},
		{"DESCRIPTION", r.Attributes.Description},
		{"STATE", r.Attributes.State},
	}
	return headers, rows
}

// IAPScreenshotUploadResult is the JSON-stable view for `iap review-screenshot
// upload`; Checksum is the MD5 hex sent as sourceFileChecksum.
type IAPScreenshotUploadResult struct {
	Action      string `json:"action"`
	ID          string `json:"id"`
	Type        string `json:"type"`
	IAPID       string `json:"iapId"`
	ProductID   string `json:"productId,omitempty"`
	FileName    string `json:"fileName"`
	Checksum    string `json:"checksum"`
	NoOp        bool   `json:"noop"`
	TemplateURL string `json:"templateUrl,omitempty"`
}

func (r *IAPScreenshotUploadResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"ID", r.ID},
		{"TYPE", r.Type},
		{"IAP_ID", r.IAPID},
		{"PRODUCT_ID", r.ProductID},
		{"FILE_NAME", r.FileName},
		{"CHECKSUM", r.Checksum},
		{"NOOP", strconv.FormatBool(r.NoOp)},
		{"TEMPLATE_URL", r.TemplateURL},
	}
	return headers, rows
}

var iapCreateCmd = &cobra.Command{
	Use:          "create <bundleId>",
	Short:        "Create an in-app purchase",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPCreate,
	Long: `create reserves a new In-App Purchase under the given app.

Idempotent: if an IAP with --product-id already exists for this app, returns
the existing record with noop=true rather than failing or creating a duplicate.`,
	Example: `  flightline iap create com.example.myapp --product-id com.example.myapp.lifetime --type NON_CONSUMABLE --name "Lifetime Pro"
  flightline iap create com.example.myapp --product-id com.example.myapp.coins --type CONSUMABLE --name Coins --review-note "Currency for the in-app store"`,
}

var iapUpdateCmd = &cobra.Command{
	Use:          "update <bundleId>",
	Short:        "Update an in-app purchase's mutable attributes",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPUpdate,
	Long: `update PATCHes the mutable attributes (name, reviewNote, familySharable) on
an existing In-App Purchase. Idempotent: if every flag matches the current
value, returns noop=true without issuing a PATCH.

productId and inAppPurchaseType are immutable post-create: to change either,
delete and recreate.`,
	Example: `  flightline iap update com.example.myapp --product com.example.myapp.lifetime --name "Lifetime Pro v2"
  flightline iap update com.example.myapp --product com.example.myapp.lifetime --review-note "updated reviewer steps"`,
}

var iapDeleteCmd = &cobra.Command{
	Use:          "delete <bundleId>",
	Short:        "Delete an in-app purchase",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPDelete,
	Long: `delete removes an In-App Purchase. Destructive: requires --yes to confirm.

Idempotent: if the IAP doesn't exist, returns noop=true without issuing a
DELETE. Apple may refuse deletion of an IAP that has been APPROVED and is
visible on the store; that case surfaces as a typed APIError.`,
	Example: `  flightline iap delete com.example.myapp --product com.example.myapp.lifetime --yes`,
}

var iapLocalizationsSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Create or update an IAP localization for one locale",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPLocalizationsSet,
	Long: `set creates the localization for --locale if it does not exist, or PATCHes
the mutable fields (name, description) if it does. Idempotent: when the
existing localization already matches the supplied flags, returns noop=true.`,
	Example: `  flightline iap localizations set com.example.myapp --product com.example.myapp.lifetime --locale en-US --name "Lifetime Pro" --description "Unlock everything, forever."
  flightline iap localizations set com.example.myapp --product com.example.myapp.lifetime --locale fr-FR --name "Pro à vie" --description "Tout débloquer, pour toujours."`,
}

var iapReviewScreenshotCmd = &cobra.Command{
	Use:   "review-screenshot",
	Short: "Manage IAP App Store review screenshots",
}

var iapReviewScreenshotUploadCmd = &cobra.Command{
	Use:          "upload <bundleId>",
	Short:        "Upload an App Store review screenshot for an IAP",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPReviewScreenshotUpload,
	Long: `upload reserves a new IAP review screenshot, PUTs the file in chunks to
Apple's CDN, and commits the upload with the local MD5.

Idempotent: if a screenshot with the same sourceFileChecksum is already
attached to this IAP, returns noop=true without re-uploading. Use
--resume to pick up a partial upload from the on-disk checkpoint.`,
	Example: `  flightline iap review-screenshot upload com.example.myapp --product com.example.myapp.lifetime --file ./review/lifetime.png
  flightline iap review-screenshot upload com.example.myapp --product com.example.myapp.lifetime --file ./review/lifetime.png --resume`,
}

// Bool flags use string indirection so "unset" stays observable for
// idempotent diffing: see resolveTriBool.
var (
	iapCreateProductID      string
	iapCreateType           string
	iapCreateName           string
	iapCreateReviewNote     string
	iapCreateFamilySharable string

	iapUpdateProduct        string
	iapUpdateName           string
	iapUpdateReviewNote     string
	iapUpdateFamilySharable string

	iapDeleteProduct string
	iapDeleteYes     bool

	iapLocSetProduct     string
	iapLocSetLocale      string
	iapLocSetName        string
	iapLocSetDescription string

	iapShotUploadProduct string
	iapShotUploadFile    string
	iapShotUploadResume  bool
)

func init() {
	iapCreateCmd.Flags().StringVar(&iapCreateProductID, "product-id", "", "developer-chosen StoreKit identifier (e.g. com.example.myapp.lifetime)")
	iapCreateCmd.Flags().StringVar(&iapCreateType, "type", "", "IAP type (CONSUMABLE | NON_CONSUMABLE | NON_RENEWING_SUBSCRIPTION)")
	iapCreateCmd.Flags().StringVar(&iapCreateName, "name", "", "internal reference name (visible in App Store Connect, not to users)")
	iapCreateCmd.Flags().StringVar(&iapCreateReviewNote, "review-note", "", "note to App Review explaining how to test")
	iapCreateCmd.Flags().StringVar(&iapCreateFamilySharable, "family-sharable", "", "true | false; omit to leave unset")
	_ = iapCreateCmd.MarkFlagRequired("product-id")
	_ = iapCreateCmd.MarkFlagRequired("type")
	_ = iapCreateCmd.MarkFlagRequired("name")

	iapUpdateCmd.Flags().StringVar(&iapUpdateProduct, "product", "", "productId of the IAP to update")
	iapUpdateCmd.Flags().StringVar(&iapUpdateName, "name", "", "new internal reference name")
	iapUpdateCmd.Flags().StringVar(&iapUpdateReviewNote, "review-note", "", "new review note")
	iapUpdateCmd.Flags().StringVar(&iapUpdateFamilySharable, "family-sharable", "", "true | false; omit to leave unchanged")
	_ = iapUpdateCmd.MarkFlagRequired("product")

	iapDeleteCmd.Flags().StringVar(&iapDeleteProduct, "product", "", "productId of the IAP to delete")
	iapDeleteCmd.Flags().BoolVar(&iapDeleteYes, "yes", false, "skip confirmation prompt (required for non-interactive runs)")
	_ = iapDeleteCmd.MarkFlagRequired("product")

	iapLocalizationsSetCmd.Flags().StringVar(&iapLocSetProduct, "product", "", "productId of the parent IAP")
	iapLocalizationsSetCmd.Flags().StringVar(&iapLocSetLocale, "locale", "", "BCP-47 locale code (e.g. en-US, fr-FR)")
	iapLocalizationsSetCmd.Flags().StringVar(&iapLocSetName, "name", "", "user-visible IAP name in this locale")
	iapLocalizationsSetCmd.Flags().StringVar(&iapLocSetDescription, "description", "", "user-visible description in this locale")
	_ = iapLocalizationsSetCmd.MarkFlagRequired("product")
	_ = iapLocalizationsSetCmd.MarkFlagRequired("locale")
	_ = iapLocalizationsSetCmd.MarkFlagRequired("name")

	iapReviewScreenshotUploadCmd.Flags().StringVar(&iapShotUploadProduct, "product", "", "productId of the parent IAP")
	iapReviewScreenshotUploadCmd.Flags().StringVar(&iapShotUploadFile, "file", "", "path to the screenshot file (PNG/JPEG)")
	iapReviewScreenshotUploadCmd.Flags().BoolVar(&iapShotUploadResume, "resume", false, "resume from on-disk upload checkpoint if present")
	_ = iapReviewScreenshotUploadCmd.MarkFlagRequired("product")
	_ = iapReviewScreenshotUploadCmd.MarkFlagRequired("file")

	iapLocalizationsCmd.AddCommand(iapLocalizationsSetCmd)
	iapReviewScreenshotCmd.AddCommand(iapReviewScreenshotUploadCmd)

	iapCmd.AddCommand(iapCreateCmd)
	iapCmd.AddCommand(iapUpdateCmd)
	iapCmd.AddCommand(iapDeleteCmd)
	iapCmd.AddCommand(iapReviewScreenshotCmd)
}

func runIAPCreate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapCreateProductID)
	iapType := strings.TrimSpace(iapCreateType)
	name := strings.TrimSpace(iapCreateName)
	if productID == "" {
		return errors.New("iap create: --product-id is required")
	}
	if !isValidIAPType(iapType) {
		return fmt.Errorf("iap create: --type %q is not one of CONSUMABLE | NON_CONSUMABLE | NON_RENEWING_SUBSCRIPTION", iapType)
	}
	if name == "" {
		return errors.New("iap create: --name is required")
	}
	familySharable, err := resolveTriBool("family-sharable", iapCreateFamilySharable)
	if err != nil {
		return fmt.Errorf("iap create: %w", err)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Existing productId returns a noop; Apple would otherwise 409.
	if existingID, existingAttrs, err := lookupIAP(cmd.Context(), c, appID, productID); err == nil {
		return Render(&IAPWriteResult{
			Action:     "create",
			ID:         existingID,
			Type:       "inAppPurchases",
			ProductID:  productID,
			NoOp:       true,
			Attributes: existingAttrs,
		}, outputMode())
	}

	body := iapCreateRequest{
		Data: iapCreateData{
			Type: "inAppPurchases",
			Attributes: iapCreateAttrs{
				Name:              name,
				ProductID:         productID,
				InAppPurchaseType: iapType,
				ReviewNote:        strings.TrimSpace(iapCreateReviewNote),
				FamilySharable:    familySharable,
			},
			Relationships: map[string]relRefBlock{
				"app": {Data: relRef{Type: "apps", ID: appID}},
			},
		},
	}
	resp, err := asc.Post[asc.Single[asc.IAPAttributes]](cmd.Context(), c, "/v2/inAppPurchases", nil, body)
	if err != nil {
		return err
	}
	return Render(&IAPWriteResult{
		Action:     "create",
		ID:         resp.Data.ID,
		Type:       resp.Data.Type,
		ProductID:  productID,
		NoOp:       false,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

func runIAPUpdate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapUpdateProduct)

	familySharable, err := resolveTriBool("family-sharable", iapUpdateFamilySharable)
	if err != nil {
		return fmt.Errorf("iap update: %w", err)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	id, current, err := findIAPByProductID(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}

	body := iapUpdateRequest{
		Data: iapUpdateData{
			Type: "inAppPurchases",
			ID:   id,
		},
	}
	changed := false

	if v := strings.TrimSpace(iapUpdateName); v != "" && v != current.Name {
		body.Data.Attributes.Name = &v
		changed = true
	}
	if cmd.Flags().Changed("review-note") {
		v := iapUpdateReviewNote
		if v != current.ReviewNote {
			body.Data.Attributes.ReviewNote = &v
			changed = true
		}
	}
	if cmd.Flags().Changed("family-sharable") && !boolPtrEq(familySharable, current.FamilySharable) {
		body.Data.Attributes.FamilySharable = familySharable
		changed = true
	}

	if !changed {
		return Render(&IAPWriteResult{
			Action:     "update",
			ID:         id,
			Type:       "inAppPurchases",
			ProductID:  productID,
			NoOp:       true,
			Attributes: current,
		}, outputMode())
	}

	resp, err := asc.Patch[asc.Single[asc.IAPAttributes]](cmd.Context(), c, "/v2/inAppPurchases/"+id, nil, body)
	if err != nil {
		return err
	}
	return Render(&IAPWriteResult{
		Action:     "update",
		ID:         resp.Data.ID,
		Type:       resp.Data.Type,
		ProductID:  productID,
		NoOp:       false,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

func runIAPDelete(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapDeleteProduct)
	if !iapDeleteYes {
		return errors.New("iap delete: refusing to delete without --yes (this is destructive)")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	existingID, _, err := lookupIAP(cmd.Context(), c, appID, productID)
	if err != nil {
		// No record → idempotent noop.
		return Render(&IAPWriteResult{
			Action:    "delete",
			ID:        "",
			Type:      "inAppPurchases",
			ProductID: productID,
			NoOp:      true,
		}, outputMode())
	}

	if err := c.Delete(cmd.Context(), "/v2/inAppPurchases/"+existingID, nil); err != nil {
		return err
	}
	return Render(&IAPWriteResult{
		Action:    "delete",
		ID:        existingID,
		Type:      "inAppPurchases",
		ProductID: productID,
		NoOp:      false,
	}, outputMode())
}

func runIAPLocalizationsSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapLocSetProduct)
	locale := strings.TrimSpace(iapLocSetLocale)
	name := strings.TrimSpace(iapLocSetName)
	desc := iapLocSetDescription

	c, err := newClient()
	if err != nil {
		return err
	}

	iapID, _, err := findIAPByProductID(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}

	existing, err := findLocalization(cmd.Context(), c, iapID, locale)
	if err != nil {
		return err
	}

	if existing == nil {
		body := iapLocalizationCreateRequest{
			Data: iapLocalizationCreateData{
				Type: "inAppPurchaseLocalizations",
				Attributes: iapLocalizationCreateAttrs{
					Name:        name,
					Locale:      locale,
					Description: desc,
				},
				Relationships: map[string]relRefBlock{
					"inAppPurchaseV2": {Data: relRef{Type: "inAppPurchases", ID: iapID}},
				},
			},
		}
		resp, err := asc.Post[asc.Single[asc.IAPLocalizationAttributes]](
			cmd.Context(), c, "/v1/inAppPurchaseLocalizations", nil, body,
		)
		if err != nil {
			return err
		}
		return Render(&IAPLocalizationWriteResult{
			Action:     "create",
			ID:         resp.Data.ID,
			Type:       resp.Data.Type,
			NoOp:       false,
			Attributes: resp.Data.Attributes,
		}, outputMode())
	}

	body := iapLocalizationUpdateRequest{
		Data: iapLocalizationUpdateData{
			Type: "inAppPurchaseLocalizations",
			ID:   existing.ID,
		},
	}
	changed := false
	if name != "" && name != existing.Attributes.Name {
		body.Data.Attributes.Name = &name
		changed = true
	}
	if cmd.Flags().Changed("description") && desc != existing.Attributes.Description {
		body.Data.Attributes.Description = &desc
		changed = true
	}

	if !changed {
		return Render(&IAPLocalizationWriteResult{
			Action:     "update",
			ID:         existing.ID,
			Type:       existing.Type,
			NoOp:       true,
			Attributes: existing.Attributes,
		}, outputMode())
	}

	resp, err := asc.Patch[asc.Single[asc.IAPLocalizationAttributes]](
		cmd.Context(), c, "/v1/inAppPurchaseLocalizations/"+existing.ID, nil, body,
	)
	if err != nil {
		return err
	}
	return Render(&IAPLocalizationWriteResult{
		Action:     "update",
		ID:         resp.Data.ID,
		Type:       resp.Data.Type,
		NoOp:       false,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

// runIAPReviewScreenshotUpload reserves, PUTs chunks, then commits; idempotent
// when the local MD5 matches the currently-attached screenshot.
func runIAPReviewScreenshotUpload(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapShotUploadProduct)
	file := strings.TrimSpace(iapShotUploadFile)

	c, err := newClient()
	if err != nil {
		return err
	}

	iapID, _, err := findIAPByProductID(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}

	// Skip the upload when the local hash matches the current sourceFileChecksum.
	localMD5, err := fileMD5Hex(file)
	if err != nil {
		return fmt.Errorf("iap review-screenshot upload: %w", err)
	}
	if existingChecksum, existingURL, ok := currentIAPScreenshot(cmd.Context(), c, iapID); ok && existingChecksum == localMD5 {
		return Render(&IAPScreenshotUploadResult{
			Action:      "upload",
			IAPID:       iapID,
			ProductID:   productID,
			FileName:    baseFileName(file),
			Checksum:    localMD5,
			NoOp:        true,
			TemplateURL: existingURL,
		}, outputMode())
	}

	res, err := c.Upload(cmd.Context(), asc.UploadOptions{
		Kind:                 asc.AssetKindIAPReviewScreenshot,
		ParentID:             iapID,
		Asset:                asc.UploadAsset{Path: file},
		ResumeFromCheckpoint: iapShotUploadResume,
	})
	if err != nil {
		return err
	}

	templateURL := ""
	if u, err := fetchIAPReviewScreenshotURL(cmd.Context(), c, iapID); err == nil {
		templateURL = u
	}

	return Render(&IAPScreenshotUploadResult{
		Action:      "upload",
		ID:          res.ID,
		Type:        res.Type,
		IAPID:       iapID,
		ProductID:   productID,
		FileName:    baseFileName(file),
		Checksum:    res.Checksum,
		NoOp:        false,
		TemplateURL: templateURL,
	}, outputMode())
}

// lookupIAP returns the IAP record for (appID, productID) or a typed error
// when no record exists.
func lookupIAP(ctx context.Context, c *asc.Client, appID, productID string) (string, asc.IAPAttributes, error) {
	q := url.Values{
		"filter[productId]": {productID},
		"limit":             {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.IAPAttributes]](ctx, c, "/v1/apps/"+appID+"/inAppPurchasesV2", q)
	if err != nil {
		return "", asc.IAPAttributes{}, err
	}
	if len(page.Data) == 0 {
		return "", asc.IAPAttributes{}, fmt.Errorf("iap: no in-app purchase with productId %q exists", productID)
	}
	return page.Data[0].ID, page.Data[0].Attributes, nil
}

// findLocalization returns the localization for (iapID, locale), or (nil, nil)
// when none exists.
func findLocalization(ctx context.Context, c *asc.Client, iapID, locale string) (*asc.Resource[asc.IAPLocalizationAttributes], error) {
	q := url.Values{
		"filter[locale]": {locale},
		"limit":          {"50"},
	}
	page, err := asc.Get[asc.Collection[asc.IAPLocalizationAttributes]](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/inAppPurchaseLocalizations", q,
	)
	if err != nil {
		return nil, err
	}
	for i := range page.Data {
		if page.Data[i].Attributes.Locale == locale {
			return &page.Data[i], nil
		}
	}
	return nil, nil
}

// currentIAPScreenshot returns the attached screenshot's checksum
// (sourceFileChecksum, MD5 hex) and CDN templateURL; ok=false on none or fetch error.
func currentIAPScreenshot(ctx context.Context, c *asc.Client, iapID string) (checksum, templateURL string, ok bool) {
	resp, err := asc.Get[asc.Single[asc.IAPReviewScreenshotAttributes]](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/appStoreReviewScreenshot", nil,
	)
	if err != nil {
		return "", "", false
	}
	if resp.Data.ID == "" {
		return "", "", false
	}
	return resp.Data.Attributes.SourceFileChecksum, resp.Data.Attributes.ImageAsset.TemplateURL, true
}

func isValidIAPType(t string) bool {
	switch t {
	case asc.IAPTypeConsumable, asc.IAPTypeNonConsumable, asc.IAPTypeNonRenewingSubscription:
		return true
	}
	return false
}

// resolveTriBool parses a flag string into *bool: "" → nil (unset), else a
// typed pointer. Three-state distinguishes "leave alone" from "set to false".
func resolveTriBool(flagName, raw string) (*bool, error) {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return nil, nil
	}
	switch v {
	case "true", "yes", "1":
		t := true
		return &t, nil
	case "false", "no", "0":
		f := false
		return &f, nil
	}
	return nil, fmt.Errorf("--%s: %q is not a boolean (use true | false)", flagName, raw)
}

func boolPtrEq(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// fileMD5Hex returns the file's MD5 hex digest. MD5 is Apple's
// sourceFileChecksum protocol: upload integrity, not security.
func fileMD5Hex(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path supplied by trusted caller (--file flag)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := md5.New() //nolint:gosec // Apple's API contract requires MD5
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func baseFileName(p string) string {
	return filepath.Base(p)
}
