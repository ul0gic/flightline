package cmd

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// AppAttributes is the subset of Apple's App.attributes Skipper reads.
//
// Field names match Apple's wire casing exactly (`bundleId`, not `bundle_id`).
// JSON output is a stable contract — adding fields is OK; renaming is a break.
//
// Source: jq '.components.schemas.App.properties.attributes.properties' openapi.oas.json
type AppAttributes struct {
	Name                     string `json:"name,omitempty"`
	BundleID                 string `json:"bundleId,omitempty"`
	SKU                      string `json:"sku,omitempty"`
	PrimaryLocale            string `json:"primaryLocale,omitempty"`
	ContentRightsDeclaration string `json:"contentRightsDeclaration,omitempty"`
	IsOrEverWasMadeForKids   bool   `json:"isOrEverWasMadeForKids,omitempty"`
}

// AppView is one row of the apps list/get output. Embeds the wire-shape
// attributes plus ID so JSON consumers don't have to reach into a nested
// envelope.
type AppView struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Attributes AppAttributes `json:"attributes"`
}

// AppList is the table-aware view for `apps list`. The TableRows method
// flattens to a tidy 4-column layout; the JSON output preserves the typed
// envelope per the stability contract.
type AppList struct {
	Apps []AppView `json:"apps"`
}

// TableRows implements TableRenderable for the apps list view.
func (l AppList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"BUNDLE_ID", "NAME", "SKU", "ID"}
	rows = make([][]string, 0, len(l.Apps))
	for _, a := range l.Apps {
		rows = append(rows, []string{a.Attributes.BundleID, a.Attributes.Name, a.Attributes.SKU, a.ID})
	}
	return headers, rows
}

// TableRows for a single app. Vertical layout reads better for one record.
func (a *AppView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", a.ID},
		{"TYPE", a.Type},
		{"NAME", a.Attributes.Name},
		{"BUNDLE_ID", a.Attributes.BundleID},
		{"SKU", a.Attributes.SKU},
		{"PRIMARY_LOCALE", a.Attributes.PrimaryLocale},
		{"CONTENT_RIGHTS_DECLARATION", a.Attributes.ContentRightsDeclaration},
	}
	return headers, rows
}

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Manage and inspect apps in App Store Connect",
	Long:  `apps groups read commands over the /v1/apps resource.`,
}

var appsListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List apps visible to the configured ASC key",
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE:         runAppsList,
	Example: `  skipper apps list
  skipper apps list --output json | jq -r '.apps[].bundleId'
  skipper apps list --limit 50`,
}

var appsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single app by bundle ID",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAppsGet,
	Example: `  skipper apps get com.example.myapp
  skipper apps get com.example.myapp --output json | jq .attributes.name`,
}

// listLimit caps how many apps are emitted in `apps list`. 0 means no cap
// (paging continues until Apple says stop). For the personal-account scale
// where Skipper lives this is fine; we'll add cursor support later if needed.
var listLimit int

func init() {
	appsListCmd.Flags().IntVar(&listLimit, "limit", 0, "max number of apps to emit (0 = no cap)")

	appsCmd.AddCommand(appsListCmd)
	appsCmd.AddCommand(appsGetCmd)
	rootCmd.AddCommand(appsCmd)
}

func runAppsList(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	// 200 is Apple's max page size for /v1/apps. Pages keeps following next
	// until exhausted; --limit caps the emitted count after that.
	q := url.Values{"limit": {"200"}}
	apps, err := collectApps(cmd.Context(), c, "/v1/apps", q, listLimit)
	if err != nil {
		return err
	}

	return Render(AppList{Apps: apps}, outputMode())
}

func runAppsGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	// /v1/apps?filter[bundleId]=<id>&limit=1 — Apple guarantees bundle IDs
	// are unique within an account, so a single-result filter is sufficient.
	q := url.Values{
		"filter[bundleId]": {bundleID},
		"limit":            {"1"},
	}
	page, err := asc.Get[asc.Collection[AppAttributes]](cmd.Context(), c, "/v1/apps", q)
	if err != nil {
		return err
	}
	if len(page.Data) == 0 {
		return fmt.Errorf("apps: no app found with bundleId %q", bundleID)
	}

	view := &AppView{
		ID:         page.Data[0].ID,
		Type:       page.Data[0].Type,
		Attributes: page.Data[0].Attributes,
	}
	return Render(view, outputMode())
}

// collectApps walks the paging iterator and returns flattened AppView rows.
// limit 0 means "no cap" — return everything Apple paginates through.
func collectApps(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]AppView, error) {
	out := make([]AppView, 0, defaultAppCap(limit))
	for page, err := range asc.Pages[AppAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, AppView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func defaultAppCap(limit int) int {
	if limit > 0 {
		return limit
	}
	return 32
}
