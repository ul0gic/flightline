package cmd

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// AppAttributes mirrors Apple's App.attributes; JSON tags are the wire contract.
type AppAttributes struct {
	Name                     string `json:"name,omitempty"`
	BundleID                 string `json:"bundleId,omitempty"`
	SKU                      string `json:"sku,omitempty"`
	PrimaryLocale            string `json:"primaryLocale,omitempty"`
	ContentRightsDeclaration string `json:"contentRightsDeclaration,omitempty"`
	IsOrEverWasMadeForKids   bool   `json:"isOrEverWasMadeForKids,omitempty"`
}

// AppView is one row of the apps list/get output.
type AppView struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Attributes AppAttributes `json:"attributes"`
}

type AppList struct {
	Apps []AppView `json:"apps"`
}

func (l AppList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"BUNDLE_ID", "NAME", "SKU", "ID"}
	rows = make([][]string, 0, len(l.Apps))
	for _, a := range l.Apps {
		rows = append(rows, []string{a.Attributes.BundleID, a.Attributes.Name, a.Attributes.SKU, a.ID})
	}
	return headers, rows
}

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
	Example: `  flightline apps list
  flightline apps list --output json | jq -r '.apps[].bundleId'
  flightline apps list --limit 50`,
}

var appsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single app by bundle ID",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAppsGet,
	Example: `  flightline apps get com.example.myapp
  flightline apps get com.example.myapp --output json | jq .attributes.name`,
}

// listLimit caps emitted apps; 0 = no cap (page until Apple stops).
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

	// 200 is Apple's max page size for /v1/apps.
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

	// Bundle IDs are unique within an account, so limit=1 is sufficient.
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

// collectApps walks the paging iterator; limit 0 means no cap.
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
