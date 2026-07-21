package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type SubscriptionGroupView struct {
	ID          string                          `json:"id"`
	Type        string                          `json:"type"`
	Attributes  asc.SubscriptionGroupAttributes `json:"attributes"`
	MemberCount int                             `json:"memberCount"`
}

type SubscriptionGroupList struct {
	BundleID string                  `json:"bundleId"`
	Groups   []SubscriptionGroupView `json:"groups"`
}

func (l SubscriptionGroupList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"REFERENCE_NAME", "MEMBERS", "ID"}
	rows = make([][]string, 0, len(l.Groups))
	for i := range l.Groups {
		g := &l.Groups[i]
		rows = append(rows, []string{
			g.Attributes.ReferenceName,
			strconv.Itoa(g.MemberCount),
			g.ID,
		})
	}
	return headers, rows
}

type SubscriptionDetailView struct {
	BundleID           string                              `json:"bundleId"`
	ID                 string                              `json:"id"`
	Type               string                              `json:"type"`
	Attributes         asc.SubscriptionAttributes          `json:"attributes"`
	Group              *SubscriptionGroupSummary           `json:"group,omitempty"`
	Localizations      []SubscriptionLocalizationItem      `json:"localizations"`
	IntroductoryOffers []SubscriptionIntroductoryOfferItem `json:"introductoryOffers"`
	Prices             []SubscriptionPriceItem             `json:"prices"`
}

type SubscriptionGroupSummary struct {
	ID         string                          `json:"id"`
	Attributes asc.SubscriptionGroupAttributes `json:"attributes"`
}

type SubscriptionLocalizationItem struct {
	ID         string                                 `json:"id"`
	Type       string                                 `json:"type"`
	Attributes asc.SubscriptionLocalizationAttributes `json:"attributes"`
}

type SubscriptionIntroductoryOfferItem struct {
	ID         string                                      `json:"id"`
	Type       string                                      `json:"type"`
	Attributes asc.SubscriptionIntroductoryOfferAttributes `json:"attributes"`
}

type SubscriptionPriceItem struct {
	ID         string                          `json:"id"`
	Type       string                          `json:"type"`
	Attributes asc.SubscriptionPriceAttributes `json:"attributes"`
}

func (v *SubscriptionDetailView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"ID", v.ID},
		{"PRODUCT_ID", v.Attributes.ProductID},
		{"NAME", v.Attributes.Name},
		{"STATE", v.Attributes.State},
		{"PERIOD", v.Attributes.SubscriptionPeriod},
		{"GROUP_LEVEL", strconv.Itoa(v.Attributes.GroupLevel)},
		{"FAMILY_SHARABLE", boolPtrStr(v.Attributes.FamilySharable)},
		{"REVIEW_NOTE", v.Attributes.ReviewNote},
	}
	if v.Group != nil {
		rows = append(rows,
			[]string{"GROUP_ID", v.Group.ID},
			[]string{"GROUP_REFERENCE_NAME", v.Group.Attributes.ReferenceName},
		)
	}
	rows = append(rows,
		[]string{"LOCALIZATIONS", strconv.Itoa(len(v.Localizations))},
		[]string{"INTRODUCTORY_OFFERS", strconv.Itoa(len(v.IntroductoryOffers))},
		[]string{"PRICES", strconv.Itoa(len(v.Prices))},
	)
	return headers, rows
}

var subscriptionsCmd = &cobra.Command{
	Use:   "subscriptions",
	Short: "Read auto-renewable subscription configuration (read-only in v1)",
	Long: `subscriptions groups read commands over Apple's auto-renewable
subscription resources. Apple structures subscriptions as a tree:

  - SubscriptionGroup          : competing-tier group
    └── Subscription           : one product within the group
        ├── Localizations      : per-locale name/description
        ├── IntroductoryOffers : onboarding discount tiers
        └── Prices             : price ladder

  - list <bundleId>                             : list groups + member count
  - get <bundleId> --product <productId>        : full detail for one product

This command group is read-only.`,
}

var subscriptionsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List subscription groups for an app with member counts",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSubscriptionsList,
	Example: `  flightline subscriptions list com.example.myapp
  flightline subscriptions list com.example.myapp --output json | jq '.groups[].memberCount'`,
}

var subscriptionsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single subscription product by productId",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSubscriptionsGet,
	Example: `  flightline subscriptions get com.example.myapp --product com.example.pro.monthly
  flightline subscriptions get com.example.myapp --product com.example.pro.monthly --output json`,
}

var (
	subscriptionsListLimit  int
	subscriptionsGetProduct string
)

func init() {
	subscriptionsListCmd.Flags().IntVar(&subscriptionsListLimit, "limit", 0, "max groups to emit (0 = no cap)")

	subscriptionsGetCmd.Flags().StringVar(&subscriptionsGetProduct, "product", "", "subscription productId (e.g. com.example.pro.monthly)")
	_ = subscriptionsGetCmd.MarkFlagRequired("product")

	subscriptionsCmd.AddCommand(subscriptionsListCmd)
	subscriptionsCmd.AddCommand(subscriptionsGetCmd)
	rootCmd.AddCommand(subscriptionsCmd)
}

func runSubscriptionsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	q := url.Values{
		"limit":   {"200"},
		"include": {"subscriptions"},
	}
	views, err := collectSubscriptionGroups(
		cmd.Context(), c,
		"/v1/apps/"+appID+"/subscriptionGroups",
		q, subscriptionsListLimit,
	)
	if err != nil {
		return err
	}
	return Render(SubscriptionGroupList{BundleID: bundleID, Groups: views}, outputMode())
}

func runSubscriptionsGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	productID := strings.TrimSpace(subscriptionsGetProduct)
	if productID == "" {
		return errors.New("subscriptions: --product is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Apple has no cross-group "find by productId" endpoint, so walk groups then subscriptions; bounded in practice.
	groupViews, err := collectSubscriptionGroups(
		cmd.Context(), c,
		"/v1/apps/"+appID+"/subscriptionGroups",
		url.Values{"limit": {"200"}}, 0,
	)
	if err != nil {
		return err
	}

	var (
		subID    string
		groupRef *SubscriptionGroupSummary
		subAttrs asc.SubscriptionAttributes
		subType  string
	)
	for i := range groupViews {
		g := &groupViews[i]
		page, err := asc.Get[asc.Collection[asc.SubscriptionAttributes]](
			cmd.Context(), c,
			"/v1/subscriptionGroups/"+g.ID+"/subscriptions",
			url.Values{"filter[productId]": {productID}, "limit": {"1"}},
		)
		if err != nil {
			return err
		}
		if len(page.Data) == 0 {
			continue
		}
		subID = page.Data[0].ID
		subType = page.Data[0].Type
		subAttrs = page.Data[0].Attributes
		groupRef = &SubscriptionGroupSummary{ID: g.ID, Attributes: g.Attributes}
		break
	}
	if subID == "" {
		return fmt.Errorf("subscriptions: no subscription with productId %q found in any group of %q", productID, bundleID)
	}

	view := &SubscriptionDetailView{
		BundleID:           bundleID,
		ID:                 subID,
		Type:               subType,
		Attributes:         subAttrs,
		Group:              groupRef,
		Localizations:      []SubscriptionLocalizationItem{},
		IntroductoryOffers: []SubscriptionIntroductoryOfferItem{},
		Prices:             []SubscriptionPriceItem{},
	}

	if err := loadSubscriptionLocalizations(cmd.Context(), c, subID, view); err != nil {
		return err
	}
	if err := loadSubscriptionIntroOffers(cmd.Context(), c, subID, view); err != nil {
		return err
	}
	if err := loadSubscriptionPrices(cmd.Context(), c, subID, view); err != nil {
		return err
	}

	return Render(view, outputMode())
}

// Member count comes from the included subscriptions relationship; Apple drops the include for empty groups, yielding 0.
func collectSubscriptionGroups(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]SubscriptionGroupView, error) {
	out := make([]SubscriptionGroupView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.SubscriptionGroupAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			view := SubscriptionGroupView{ID: r.ID, Type: r.Type, Attributes: r.Attributes}
			if rel, ok := r.Relationships["subscriptions"]; ok {
				view.MemberCount = countRelationshipRefs(rel)
			}
			out = append(out, view)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func countRelationshipRefs(rel asc.Relationship) int {
	if len(rel.Data) == 0 || string(rel.Data) == "null" {
		return 0
	}
	var arr []struct{}
	if err := json.Unmarshal(rel.Data, &arr); err != nil {
		return 0
	}
	return len(arr)
}

func loadSubscriptionLocalizations(ctx context.Context, c *asc.Client, subID string, view *SubscriptionDetailView) error {
	for page, err := range asc.Pages[asc.SubscriptionLocalizationAttributes](
		ctx, c, "/v1/subscriptions/"+subID+"/subscriptionLocalizations", url.Values{"limit": {"200"}},
	) {
		if err != nil {
			return err
		}
		for _, r := range page.Data {
			view.Localizations = append(view.Localizations, SubscriptionLocalizationItem{
				ID: r.ID, Type: r.Type, Attributes: r.Attributes,
			})
		}
	}
	return nil
}

func loadSubscriptionIntroOffers(ctx context.Context, c *asc.Client, subID string, view *SubscriptionDetailView) error {
	for page, err := range asc.Pages[asc.SubscriptionIntroductoryOfferAttributes](
		ctx, c, "/v1/subscriptions/"+subID+"/introductoryOffers", url.Values{"limit": {"200"}},
	) {
		if err != nil {
			return err
		}
		for _, r := range page.Data {
			view.IntroductoryOffers = append(view.IntroductoryOffers, SubscriptionIntroductoryOfferItem{
				ID: r.ID, Type: r.Type, Attributes: r.Attributes,
			})
		}
	}
	return nil
}

func loadSubscriptionPrices(ctx context.Context, c *asc.Client, subID string, view *SubscriptionDetailView) error {
	for page, err := range asc.Pages[asc.SubscriptionPriceAttributes](
		ctx, c, "/v1/subscriptions/"+subID+"/prices", url.Values{"limit": {"200"}},
	) {
		if err != nil {
			return err
		}
		for _, r := range page.Data {
			view.Prices = append(view.Prices, SubscriptionPriceItem{
				ID: r.ID, Type: r.Type, Attributes: r.Attributes,
			})
		}
	}
	return nil
}
