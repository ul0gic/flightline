package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// IAPView is one row of the iap list/get output.
type IAPView struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Attributes asc.IAPAttributes `json:"attributes"`
	// ReviewScreenshotURL is the templated URL of the IAP's App Store review
	// screenshot when one is attached. Empty when not set or when the
	// screenshot relationship was not requested (list mode skips the extra
	// hop). Populated by `iap get` only.
	ReviewScreenshotURL string `json:"reviewScreenshotUrl,omitempty"`
}

// IAPList is the table-aware view for `iap list`.
type IAPList struct {
	IAPs []IAPView `json:"iaps"`
}

// TableRows implements TableRenderable for the iap list view.
func (l IAPList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"PRODUCT_ID", "NAME", "TYPE", "STATE", "ID"}
	rows = make([][]string, 0, len(l.IAPs))
	for i := range l.IAPs {
		v := &l.IAPs[i]
		rows = append(rows, []string{
			v.Attributes.ProductID,
			v.Attributes.Name,
			v.Attributes.InAppPurchaseType,
			v.Attributes.State,
			v.ID,
		})
	}
	return headers, rows
}

// TableRows for a single IAP. Vertical layout reads better for one record.
func (v *IAPView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", v.ID},
		{"TYPE", v.Type},
		{"PRODUCT_ID", v.Attributes.ProductID},
		{"NAME", v.Attributes.Name},
		{"IAP_TYPE", v.Attributes.InAppPurchaseType},
		{"STATE", v.Attributes.State},
		{"REVIEW_NOTE", v.Attributes.ReviewNote},
		{"FAMILY_SHARABLE", boolPtrStr(v.Attributes.FamilySharable)},
		{"CONTENT_HOSTING", boolPtrStr(v.Attributes.ContentHosting)},
		{"REVIEW_SCREENSHOT_URL", v.ReviewScreenshotURL},
	}
	return headers, rows
}

// IAPLocalizationView is one row of the iap localizations list output.
type IAPLocalizationView struct {
	ID         string                        `json:"id"`
	Type       string                        `json:"type"`
	Attributes asc.IAPLocalizationAttributes `json:"attributes"`
}

// IAPLocalizationList is the table-aware view for `iap localizations list`.
type IAPLocalizationList struct {
	Localizations []IAPLocalizationView `json:"localizations"`
}

// TableRows implements TableRenderable for the iap localizations list view.
func (l IAPLocalizationList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"LOCALE", "NAME", "STATE", "ID"}
	rows = make([][]string, 0, len(l.Localizations))
	for i := range l.Localizations {
		v := &l.Localizations[i]
		rows = append(rows, []string{
			v.Attributes.Locale,
			v.Attributes.Name,
			v.Attributes.State,
			v.ID,
		})
	}
	return headers, rows
}

var iapCmd = &cobra.Command{
	Use:   "iap",
	Short: "Manage and inspect non-subscription In-App Purchases",
	Long: `iap groups read commands over the /v2/inAppPurchases resource.

Auto-renewable subscriptions live under a separate /v1/subscriptionGroups
resource and are not handled here — see ` + "`fline subscriptions`" + `.`,
}

var iapListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List in-app purchases for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPList,
	Example: `  fline iap list com.example.myapp
  fline iap list com.example.myapp --type CONSUMABLE
  fline iap list com.example.myapp --output json | jq -r '.iaps[].attributes.productId'`,
}

var iapGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single in-app purchase by productId",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPGet,
	Example: `  fline iap get com.example.myapp --product com.example.myapp.lifetime
  fline iap get com.example.myapp --product com.example.myapp.lifetime --output json`,
}

// iapLocalizationsCmd groups localizations subcommands. Wired under iapCmd
// so the user-facing path is `fline iap localizations list`.
var iapLocalizationsCmd = &cobra.Command{
	Use:   "localizations",
	Short: "Manage and inspect IAP localizations",
}

var iapLocalizationsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List localizations for an in-app purchase",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPLocalizationsList,
	Example: `  fline iap localizations list com.example.myapp --product com.example.myapp.lifetime
  fline iap localizations list com.example.myapp --product com.example.myapp.lifetime --output json`,
}

// Per-subcommand flag state. Separate variables so cobra defaults don't
// collide between siblings.
var (
	iapListType                 string
	iapListLimit                int
	iapGetProduct               string
	iapLocalizationsListProduct string
	iapLocalizationsListLimit   int
)

func init() {
	iapListCmd.Flags().StringVar(&iapListType, "type", "", "filter by IAP type (CONSUMABLE|NON_CONSUMABLE|NON_RENEWING_SUBSCRIPTION); empty = all")
	iapListCmd.Flags().IntVar(&iapListLimit, "limit", 0, "max IAPs to emit (0 = no cap)")

	iapGetCmd.Flags().StringVar(&iapGetProduct, "product", "", "productId of the IAP to fetch (e.g. com.example.myapp.lifetime)")
	_ = iapGetCmd.MarkFlagRequired("product")

	iapLocalizationsListCmd.Flags().StringVar(&iapLocalizationsListProduct, "product", "", "productId of the parent IAP")
	iapLocalizationsListCmd.Flags().IntVar(&iapLocalizationsListLimit, "limit", 0, "max localizations to emit (0 = no cap)")
	_ = iapLocalizationsListCmd.MarkFlagRequired("product")

	iapLocalizationsCmd.AddCommand(iapLocalizationsListCmd)
	iapCmd.AddCommand(iapListCmd)
	iapCmd.AddCommand(iapGetCmd)
	iapCmd.AddCommand(iapLocalizationsCmd)
	rootCmd.AddCommand(iapCmd)
}

func runIAPList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	q := url.Values{"limit": {"200"}}
	if t := strings.TrimSpace(iapListType); t != "" {
		q.Set("filter[inAppPurchaseType]", t)
	}

	views, err := collectIAPs(cmd.Context(), c, "/v1/apps/"+appID+"/inAppPurchasesV2", q, iapListLimit)
	if err != nil {
		return err
	}
	return Render(IAPList{IAPs: views}, outputMode())
}

func runIAPGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapGetProduct)
	if productID == "" {
		return fmt.Errorf("iap: --product is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	id, attrs, err := findIAPByProductID(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}

	view := &IAPView{
		ID:         id,
		Type:       "inAppPurchases",
		Attributes: attrs,
	}

	// Best-effort fetch of the review screenshot URL. A missing screenshot is
	// the common case (Apple returns 200 with empty data, or 404 for resources
	// that have never had one); both are treated as "no screenshot" rather
	// than fatal — the user can still see core IAP data.
	if shotURL, err := fetchIAPReviewScreenshotURL(cmd.Context(), c, id); err == nil {
		view.ReviewScreenshotURL = shotURL
	}

	return Render(view, outputMode())
}

func runIAPLocalizationsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapLocalizationsListProduct)
	if productID == "" {
		return fmt.Errorf("iap: --product is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	id, _, err := findIAPByProductID(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return err
	}

	q := url.Values{"limit": {"200"}}
	views, err := collectIAPLocalizations(
		cmd.Context(), c,
		"/v2/inAppPurchases/"+id+"/inAppPurchaseLocalizations",
		q, iapLocalizationsListLimit,
	)
	if err != nil {
		return err
	}
	return Render(IAPLocalizationList{Localizations: views}, outputMode())
}

// findIAPByProductID resolves bundle → appID, then filters
// /v1/apps/{appID}/inAppPurchasesV2?filter[productId]=<productID> and returns
// (asc-id, attributes). Errors carry the bundleId AND productId so users see
// what's missing.
func findIAPByProductID(ctx context.Context, c *asc.Client, bundleID, productID string) (string, asc.IAPAttributes, error) {
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return "", asc.IAPAttributes{}, err
	}
	q := url.Values{
		"filter[productId]": {productID},
		"limit":             {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.IAPAttributes]](
		ctx, c, "/v1/apps/"+appID+"/inAppPurchasesV2", q,
	)
	if err != nil {
		return "", asc.IAPAttributes{}, err
	}
	if len(page.Data) == 0 {
		return "", asc.IAPAttributes{}, fmt.Errorf("iap: no in-app purchase with productId %q found for %q", productID, bundleID)
	}
	return page.Data[0].ID, page.Data[0].Attributes, nil
}

// collectIAPs walks the paging iterator and returns flattened IAPView rows.
func collectIAPs(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]IAPView, error) {
	out := make([]IAPView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.IAPAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, IAPView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// collectIAPLocalizations walks the paging iterator and returns flattened
// IAPLocalizationView rows.
func collectIAPLocalizations(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]IAPLocalizationView, error) {
	out := make([]IAPLocalizationView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.IAPLocalizationAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, IAPLocalizationView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// fetchIAPReviewScreenshotURL pulls the IAP's appStoreReviewScreenshot
// relationship and returns the templated URL of the rendered image asset.
// Returns empty string + nil if the relationship has no data block (no
// screenshot uploaded). Apple's API returns 200 with `data: null` in that
// case rather than a 404.
func fetchIAPReviewScreenshotURL(ctx context.Context, c *asc.Client, iapID string) (string, error) {
	resp, err := asc.Get[asc.Single[asc.IAPReviewScreenshotAttributes]](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/appStoreReviewScreenshot", nil,
	)
	if err != nil {
		return "", err
	}
	return resp.Data.Attributes.ImageAsset.TemplateURL, nil
}
