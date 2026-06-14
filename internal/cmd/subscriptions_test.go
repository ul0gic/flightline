package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestSubscriptionGroupView_JSONShape(t *testing.T) {
	v := SubscriptionGroupView{
		ID:   "GROUP-PRO",
		Type: "subscriptionGroups",
		Attributes: asc.SubscriptionGroupAttributes{
			ReferenceName: "Pro Tiers",
		},
		MemberCount: 2,
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"GROUP-PRO"`,
		`"type":"subscriptionGroups"`,
		`"referenceName":"Pro Tiers"`,
		`"memberCount":2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestSubscriptionGroupList_TableRowsHeaders(t *testing.T) {
	list := SubscriptionGroupList{
		BundleID: "com.example.alpha",
		Groups: []SubscriptionGroupView{
			{ID: "G1", Attributes: asc.SubscriptionGroupAttributes{ReferenceName: "Pro Tiers"}, MemberCount: 2},
			{ID: "G2", Attributes: asc.SubscriptionGroupAttributes{ReferenceName: "Team Tiers"}, MemberCount: 1},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"REFERENCE_NAME", "MEMBERS", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][1] != "2" {
		t.Errorf("rows[0][1] (MEMBERS) = %q, want 2", rows[0][1])
	}
}

func TestSubscriptionDetailView_TableRows_Vertical(t *testing.T) {
	familySharable := false
	v := &SubscriptionDetailView{
		BundleID: "com.example.alpha",
		ID:       "SUB-PRO-MONTHLY",
		Type:     "subscriptions",
		Attributes: asc.SubscriptionAttributes{
			Name:               "Pro Monthly",
			ProductID:          "com.example.pro.monthly",
			FamilySharable:     &familySharable,
			State:              "APPROVED",
			SubscriptionPeriod: "ONE_MONTH",
			GroupLevel:         1,
		},
		Group: &SubscriptionGroupSummary{
			ID:         "GROUP-PRO",
			Attributes: asc.SubscriptionGroupAttributes{ReferenceName: "Pro Tiers"},
		},
		Localizations: []SubscriptionLocalizationItem{
			{ID: "L1", Attributes: asc.SubscriptionLocalizationAttributes{Locale: "en-US"}},
		},
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 10 {
		t.Errorf("rows = %d, want >= 10", len(rows))
	}
	// Group reference is surfaced.
	foundGroup := false
	for _, r := range rows {
		if r[0] == "GROUP_ID" && r[1] == "GROUP-PRO" {
			foundGroup = true
		}
	}
	if !foundGroup {
		t.Error("expected GROUP_ID row")
	}
}

func TestCountRelationshipRefs(t *testing.T) {
	t.Run("two_refs", func(t *testing.T) {
		rel := asc.Relationship{Data: json.RawMessage(`[{"type":"x","id":"1"},{"type":"x","id":"2"}]`)}
		if got := countRelationshipRefs(rel); got != 2 {
			t.Errorf("got %d, want 2", got)
		}
	})
	t.Run("empty_array", func(t *testing.T) {
		rel := asc.Relationship{Data: json.RawMessage(`[]`)}
		if got := countRelationshipRefs(rel); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
	t.Run("null", func(t *testing.T) {
		rel := asc.Relationship{Data: json.RawMessage(`null`)}
		if got := countRelationshipRefs(rel); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
	t.Run("absent", func(t *testing.T) {
		rel := asc.Relationship{}
		if got := countRelationshipRefs(rel); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
}

func TestSubscriptionsCommand_RegisteredOnRoot(t *testing.T) {
	var s *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "subscriptions" {
			s = c
			break
		}
	}
	if s == nil {
		t.Fatal("subscriptions not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range s.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"list", "get"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("subscriptions subcommand %q missing", want)
		}
	}
}

// TestSubscriptions_JSONOutputStability_List locks the list shape.
func TestSubscriptions_JSONOutputStability_List(t *testing.T) {
	list := SubscriptionGroupList{
		BundleID: "com.example.alpha",
		Groups: []SubscriptionGroupView{
			{ID: "G1", Type: "subscriptionGroups", Attributes: asc.SubscriptionGroupAttributes{ReferenceName: "Pro"}, MemberCount: 2},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		BundleID string           `json:"bundleId"`
		Groups   []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	if decoded.BundleID != "com.example.alpha" {
		t.Errorf("bundleId = %q", decoded.BundleID)
	}
	if len(decoded.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(decoded.Groups))
	}
	for _, key := range []string{"id", "type", "attributes", "memberCount"} {
		if _, ok := decoded.Groups[0][key]; !ok {
			t.Errorf("missing per-row key %q: JSON contract drift", key)
		}
	}
}

// TestSubscriptions_FixtureReplay_GroupsList exercises the groups list.
func TestSubscriptions_FixtureReplay_GroupsList(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/subscriptionGroups": {File: "subscriptions_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectSubscriptionGroups(ctx, c, "/v1/apps/"+appID+"/subscriptionGroups", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectSubscriptionGroups: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("groups len = %d, want 2", len(views))
	}
	if views[0].MemberCount != 2 {
		t.Errorf("groups[0].MemberCount = %d, want 2", views[0].MemberCount)
	}
	if views[1].MemberCount != 1 {
		t.Errorf("groups[1].MemberCount = %d, want 1", views[1].MemberCount)
	}
	if views[0].Attributes.ReferenceName != "Pro Tiers" {
		t.Errorf("groups[0].ReferenceName = %q, want Pro Tiers", views[0].Attributes.ReferenceName)
	}
}

// TestSubscriptions_FixtureReplay_GetWithIncludes exercises the multi-fetch
// detail path: groups list, subscription lookup, then the include loaders.
func TestSubscriptions_FixtureReplay_GetWithIncludes(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/subscriptionGroups":                      {File: "subscriptions_list"},
		"GET /v1/subscriptionGroups/GROUP-PRO/subscriptions":              {File: "subscriptions_get"},
		"GET /v1/subscriptionGroups/GROUP-TEAM/subscriptions":             {File: "apps_get_notFound"},
		"GET /v1/subscriptions/SUB-PRO-MONTHLY/subscriptionLocalizations": {File: "subscriptions_localizations"},
		"GET /v1/subscriptions/SUB-PRO-MONTHLY/introductoryOffers":        {File: "subscriptions_intro_offers"},
		"GET /v1/subscriptions/SUB-PRO-MONTHLY/prices":                    {File: "subscriptions_prices"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	// Walk groups and find the subscription.
	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	groupViews, err := collectSubscriptionGroups(ctx, c, "/v1/apps/"+appID+"/subscriptionGroups", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectSubscriptionGroups: %v", err)
	}
	if len(groupViews) != 2 {
		t.Fatalf("groups len = %d, want 2", len(groupViews))
	}

	subPage, err := asc.Get[asc.Collection[asc.SubscriptionAttributes]](
		ctx, c, "/v1/subscriptionGroups/GROUP-PRO/subscriptions", url.Values{"filter[productId]": {"com.example.alpha.pro.monthly"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("subscription lookup: %v", err)
	}
	if len(subPage.Data) != 1 {
		t.Fatalf("subscription lookup data len = %d, want 1", len(subPage.Data))
	}

	view := &SubscriptionDetailView{
		BundleID:   "com.example.alpha",
		ID:         subPage.Data[0].ID,
		Type:       subPage.Data[0].Type,
		Attributes: subPage.Data[0].Attributes,
	}

	if err := loadSubscriptionLocalizations(ctx, c, view.ID, view); err != nil {
		t.Fatalf("loadSubscriptionLocalizations: %v", err)
	}
	if err := loadSubscriptionIntroOffers(ctx, c, view.ID, view); err != nil {
		t.Fatalf("loadSubscriptionIntroOffers: %v", err)
	}
	if err := loadSubscriptionPrices(ctx, c, view.ID, view); err != nil {
		t.Fatalf("loadSubscriptionPrices: %v", err)
	}

	if len(view.Localizations) != 2 {
		t.Errorf("Localizations len = %d, want 2", len(view.Localizations))
	}
	if len(view.IntroductoryOffers) != 1 {
		t.Errorf("IntroductoryOffers len = %d, want 1", len(view.IntroductoryOffers))
	}
	if len(view.Prices) != 2 {
		t.Errorf("Prices len = %d, want 2", len(view.Prices))
	}
	if view.Attributes.State != "APPROVED" {
		t.Errorf("attributes.state = %q, want APPROVED", view.Attributes.State)
	}
}

// TestSubscriptions_GetMissingProductError exercises the error path when
// no subscription matches the productId across any group.
func TestSubscriptions_GetMissingProductError(t *testing.T) {
	prev := subscriptionsGetProduct
	t.Cleanup(func() { subscriptionsGetProduct = prev })

	subscriptionsGetProduct = ""
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := runSubscriptionsGet(cmd, []string{"com.example.alpha"})
	if err == nil {
		t.Fatal("expected error when --product is empty")
	}
	if !strings.Contains(err.Error(), "--product") {
		t.Errorf("error %q does not mention --product", err.Error())
	}
}
