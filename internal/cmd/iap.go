package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type IAPView struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Attributes asc.IAPAttributes `json:"attributes"`
	// Populated by `iap get` only; list mode skips the extra relationship hop.
	ReviewScreenshotURL string `json:"reviewScreenshotUrl,omitempty"`
}

type IAPList struct {
	IAPs []IAPView `json:"iaps"`
}

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

type IAPLocalizationView struct {
	ID         string                        `json:"id"`
	Type       string                        `json:"type"`
	Attributes asc.IAPLocalizationAttributes `json:"attributes"`
}

type IAPLocalizationList struct {
	Localizations []IAPLocalizationView `json:"localizations"`
}

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
resource and are not handled here: see ` + "`flightline subscriptions`" + `.`,
}

var iapListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List in-app purchases for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPList,
	Example: `  flightline iap list com.example.myapp
  flightline iap list com.example.myapp --type CONSUMABLE
  flightline iap list com.example.myapp --output json | jq -r '.iaps[].attributes.productId'`,
}

var iapGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single in-app purchase by productId",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runIAPGet,
	Example: `  flightline iap get com.example.myapp --product com.example.myapp.lifetime
  flightline iap get com.example.myapp --product com.example.myapp.lifetime --output json`,
}

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
	Example: `  flightline iap localizations list com.example.myapp --product com.example.myapp.lifetime
  flightline iap localizations list com.example.myapp --product com.example.myapp.lifetime --output json`,
}

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
		return errors.New("iap: --product is required")
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

	// Missing screenshot (Apple's 200-with-empty-data or 404) is the common case, not fatal.
	if shotURL, err := fetchIAPReviewScreenshotURL(cmd.Context(), c, id); err == nil {
		view.ReviewScreenshotURL = shotURL
	}

	return Render(view, outputMode())
}

func runIAPLocalizationsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(iapLocalizationsListProduct)
	if productID == "" {
		return errors.New("iap: --product is required")
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

// Errors carry both bundleId and productId so the user sees exactly what was missing.
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

// Apple returns 200 with `data: null` (not 404) when no screenshot is uploaded; that surfaces as "".
func fetchIAPReviewScreenshotURL(ctx context.Context, c *asc.Client, iapID string) (string, error) {
	resp, err := asc.Get[asc.Single[asc.IAPReviewScreenshotAttributes]](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/appStoreReviewScreenshot", nil,
	)
	if err != nil {
		return "", err
	}
	return resp.Data.Attributes.ImageAsset.TemplateURL, nil
}
