package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// SubscriptionGroupView is one row of the subscriptions list output. It
// embeds the count of member subscriptions so the table is glanceable
// without a second fetch.
type SubscriptionGroupView struct {
	ID          string                          `json:"id"`
	Type        string                          `json:"type"`
	Attributes  asc.SubscriptionGroupAttributes `json:"attributes"`
	MemberCount int                             `json:"memberCount"`
}

// SubscriptionGroupList is the table-aware view for `subscriptions list`.
type SubscriptionGroupList struct {
	BundleID string                  `json:"bundleId"`
	Groups   []SubscriptionGroupView `json:"groups"`
}

// TableRows implements TableRenderable for the groups list view.
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

// SubscriptionDetailView is the read-side view for `subscriptions get`.
//
// Embeds the parent group reference, the per-locale subscription
// localizations, the price ladder (raw — pricepoints decode separately),
// and any introductory offers. Phase 3 writes will reuse these typed
// structs so the JSON contract stays stable across read/write paths.
type SubscriptionDetailView struct {
	BundleID           string                              `json:"bundleId"`
	ID                 string                              `json:"id"`
	Type               string                              `json:"type"`
	Attributes         asc.SubscriptionAttributes          `json:"attributes"`
	Group              *SubscriptionGroupSummary           `json:"group,omitempty"`
	Localizations      []SubscriptionLocalizationItem      `json:"localizations,omitempty"`
	IntroductoryOffers []SubscriptionIntroductoryOfferItem `json:"introductoryOffers,omitempty"`
	Prices             []SubscriptionPriceItem             `json:"prices,omitempty"`
}

// SubscriptionGroupSummary is the parent group reference embedded on a
// detail view.
type SubscriptionGroupSummary struct {
	ID         string                          `json:"id"`
	Attributes asc.SubscriptionGroupAttributes `json:"attributes"`
}

// SubscriptionLocalizationItem wraps one localization resource.
type SubscriptionLocalizationItem struct {
	ID         string                                 `json:"id"`
	Type       string                                 `json:"type"`
	Attributes asc.SubscriptionLocalizationAttributes `json:"attributes"`
}

// SubscriptionIntroductoryOfferItem wraps one intro offer resource.
type SubscriptionIntroductoryOfferItem struct {
	ID         string                                      `json:"id"`
	Type       string                                      `json:"type"`
	Attributes asc.SubscriptionIntroductoryOfferAttributes `json:"attributes"`
}

// SubscriptionPriceItem wraps one price-record resource.
type SubscriptionPriceItem struct {
	ID         string                          `json:"id"`
	Type       string                          `json:"type"`
	Attributes asc.SubscriptionPriceAttributes `json:"attributes"`
}

// TableRows for the subscription detail view. Vertical layout reads better
// for one product; full price ladder + locale list goes to JSON.
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

  - SubscriptionGroup           — competing-tier group
    └── Subscription            — one product within the group
        ├── Localizations       — per-locale name/description
        ├── IntroductoryOffers  — onboarding discount tiers
        └── Prices              — price ladder

  - list <bundleId>                              — list groups + member count
  - get <bundleId> --product <productId>         — full detail for one product

v1 is read-only; full CRUD lands in Phase 3.`,
}

var subscriptionsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List subscription groups for an app with member counts",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSubscriptionsList,
	Example: `  skipper subscriptions list com.example.myapp
  skipper subscriptions list com.example.myapp --output json | jq '.groups[].memberCount'`,
}

var subscriptionsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single subscription product by productId",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSubscriptionsGet,
	Example: `  skipper subscriptions get com.example.myapp --product com.example.pro.monthly
  skipper subscriptions get com.example.myapp --product com.example.pro.monthly --output json`,
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
		return fmt.Errorf("subscriptions: --product is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Subscriptions live under groups, but Apple does not expose a direct
	// "find subscription by productId across all groups" endpoint. Walk
	// the groups, then walk each group's subscriptions until the productId
	// matches. Most apps have <5 groups and <10 subscriptions per group;
	// this is bounded.
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
		BundleID:   bundleID,
		ID:         subID,
		Type:       subType,
		Attributes: subAttrs,
		Group:      groupRef,
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

// collectSubscriptionGroups walks the paging iterator and returns the
// groups with member counts. Member count is read off the included
// subscriptions relationship's data array length when present, falling
// back to 0 (which renders as a hint to consumers that the include was
// dropped — Apple sometimes drops includes when a group has 0 members).
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

// countRelationshipRefs returns the count of {type, id} entries in a
// to-many relationship's data array, or 0 if the data is absent / null.
func countRelationshipRefs(rel asc.Relationship) int {
	if len(rel.Data) == 0 || string(rel.Data) == "null" {
		return 0
	}
	// To-many relationships serialize as a JSON array. Decode as a slice
	// of empty structs — only the count matters here.
	var arr []struct{}
	if err := json.Unmarshal(rel.Data, &arr); err != nil {
		return 0
	}
	return len(arr)
}

// loadSubscriptionLocalizations populates view.Localizations.
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

// loadSubscriptionIntroOffers populates view.IntroductoryOffers.
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

// loadSubscriptionPrices populates view.Prices.
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
