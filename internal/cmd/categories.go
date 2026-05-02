package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// CategoryView is one row of the categories list output. Apple's id on a
// category resource is the stable category key (GAMES, PRODUCTIVITY, …) —
// surfaced via Resource.ID, not nested under attributes.
type CategoryView struct {
	ID         string                    `json:"id"`
	Type       string                    `json:"type"`
	Attributes asc.AppCategoryAttributes `json:"attributes"`
}

// CategoryList is the table-aware view for `categories list`.
type CategoryList struct {
	Categories []CategoryView `json:"categories"`
}

// TableRows implements TableRenderable for the categories list view.
func (l CategoryList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"CATEGORY", "PLATFORMS"}
	rows = make([][]string, 0, len(l.Categories))
	for i := range l.Categories {
		c := &l.Categories[i]
		rows = append(rows, []string{c.ID, strings.Join(c.Attributes.Platforms, ",")})
	}
	return headers, rows
}

// CategoryAssignmentView is the read-side view for `categories get` — the
// categories currently set on an app's editable appInfo. Empty fields mean
// the slot is unassigned (which is a frequent submission-rejection cause).
//
// Apple models category selection as 6 separate to-one relationships on the
// appInfo resource:
//   - primaryCategory + primarySubcategoryOne + primarySubcategoryTwo
//   - secondaryCategory + secondarySubcategoryOne + secondarySubcategoryTwo
//
// The empty slots surface as "(unassigned)" in table mode so visual scans
// catch missing assignments.
type CategoryAssignmentView struct {
	BundleID                string `json:"bundleId"`
	AppInfoID               string `json:"appInfoId"`
	AppInfoState            string `json:"appInfoState,omitempty"`
	PrimaryCategory         string `json:"primaryCategory,omitempty"`
	PrimarySubcategoryOne   string `json:"primarySubcategoryOne,omitempty"`
	PrimarySubcategoryTwo   string `json:"primarySubcategoryTwo,omitempty"`
	SecondaryCategory       string `json:"secondaryCategory,omitempty"`
	SecondarySubcategoryOne string `json:"secondarySubcategoryOne,omitempty"`
	SecondarySubcategoryTwo string `json:"secondarySubcategoryTwo,omitempty"`
}

// TableRows for category assignment. Vertical layout reads better for one
// record. Empty slots render as "(unassigned)".
func (v *CategoryAssignmentView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"APP_INFO_ID", v.AppInfoID},
		{"APP_INFO_STATE", v.AppInfoState},
		{"primaryCategory", categoryCell(v.PrimaryCategory)},
		{"primarySubcategoryOne", categoryCell(v.PrimarySubcategoryOne)},
		{"primarySubcategoryTwo", categoryCell(v.PrimarySubcategoryTwo)},
		{"secondaryCategory", categoryCell(v.SecondaryCategory)},
		{"secondarySubcategoryOne", categoryCell(v.SecondarySubcategoryOne)},
		{"secondarySubcategoryTwo", categoryCell(v.SecondarySubcategoryTwo)},
	}
	return headers, rows
}

// categoryCell formats an unassigned category slot as "(unassigned)" for
// table mode. JSON mode keeps the empty string (omitempty drops it
// entirely) so machine consumers see a missing field rather than a
// human-readable label.
func categoryCell(v string) string {
	if v == "" {
		return "(unassigned)"
	}
	return v
}

var categoriesCmd = &cobra.Command{
	Use:   "categories",
	Short: "Inspect App Store category catalog and per-app assignments",
	Long: `categories groups read commands over the /v1/appCategories resource.

categories list dumps Apple's catalog of top-level categories. Filterable
by --platform; defaults to IOS to match Skipper's default platform.

categories get <bundleId> shows the category assignments on the app's
editable appInfo (primary + secondary plus their subcategories).
Unassigned slots are a frequent rejection cause — surface them visibly.`,
}

var categoriesListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List top-level App Store categories",
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE:         runCategoriesList,
	Example: `  skipper categories list
  skipper categories list --platform MAC_OS
  skipper categories list --output json | jq -r '.categories[].id'`,
}

var categoriesGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Show the category assignments on an app's editable appInfo",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCategoriesGet,
	Example: `  skipper categories get com.example.myapp
  skipper categories get com.example.myapp --output json | jq .primaryCategory`,
}

var (
	categoriesListPlatform string
)

func init() {
	categoriesListCmd.Flags().StringVar(&categoriesListPlatform, "platform", "IOS", "platform filter (IOS|MAC_OS|TV_OS|VISION_OS); empty = all")

	categoriesCmd.AddCommand(categoriesListCmd)
	categoriesCmd.AddCommand(categoriesGetCmd)
	rootCmd.AddCommand(categoriesCmd)
}

func runCategoriesList(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	q := url.Values{
		"limit":          {"200"},
		"exists[parent]": {"false"}, // top-level only — subcategories follow via /relationships/subcategories
	}
	if p := strings.TrimSpace(categoriesListPlatform); p != "" {
		q.Set("filter[platforms]", p)
	}

	out := make([]CategoryView, 0, 64)
	for page, err := range asc.Pages[asc.AppCategoryAttributes](cmd.Context(), c, "/v1/appCategories", q) {
		if err != nil {
			return err
		}
		for _, r := range page.Data {
			out = append(out, CategoryView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
		}
	}
	return Render(CategoryList{Categories: out}, outputMode())
}

func runCategoriesGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Pick the editable appInfo — that's where the next-submission category
	// assignments live. The live appInfo's categories are read-only history.
	appInfoID, state, err := pickEditableAppInfo(cmd.Context(), c, appID)
	if err != nil {
		return err
	}

	view := &CategoryAssignmentView{
		BundleID:     bundleID,
		AppInfoID:    appInfoID,
		AppInfoState: state,
	}

	// Six independent to-one relationship hops. Apple returns 200 with a null
	// `data` block for unassigned slots — fetchCategoryRelationship handles
	// that as the empty-string return value, never an error.
	for _, rel := range []struct {
		name string
		dest *string
	}{
		{"primaryCategory", &view.PrimaryCategory},
		{"primarySubcategoryOne", &view.PrimarySubcategoryOne},
		{"primarySubcategoryTwo", &view.PrimarySubcategoryTwo},
		{"secondaryCategory", &view.SecondaryCategory},
		{"secondarySubcategoryOne", &view.SecondarySubcategoryOne},
		{"secondarySubcategoryTwo", &view.SecondarySubcategoryTwo},
	} {
		id, ferr := fetchCategoryRelationship(cmd.Context(), c, appInfoID, rel.name)
		if ferr != nil {
			return fmt.Errorf("categories: fetch %s: %w", rel.name, ferr)
		}
		*rel.dest = id
	}

	return Render(view, outputMode())
}

// pickEditableAppInfo lists an app's appInfos and returns the editable one
// (i.e. NOT in the live READY_FOR_DISTRIBUTION bucket). Falls back to the
// first appInfo if none is unambiguously editable. Mirrors the live/editable
// bucketing in age_rating.go's pickAppInfoForVersion but always biases to the
// editable side because category writes only land on editable appInfos.
func pickEditableAppInfo(ctx context.Context, c *asc.Client, appID string) (appInfoID, state string, err error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[asc.AppInfoAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appInfos", q,
	)
	if err != nil {
		return "", "", err
	}
	if len(page.Data) == 0 {
		return "", "", fmt.Errorf("categories: app %q has no appInfo records", appID)
	}
	for i := range page.Data {
		info := &page.Data[i]
		if !isLiveAppInfoState(info.Attributes.State) {
			return info.ID, info.Attributes.State, nil
		}
	}
	// Fallback: first appInfo (rare; happens when all appInfos are live, which
	// in practice means the app is shipping and a new editable appInfo hasn't
	// been spun yet). Surface its state so the caller sees the bucket.
	return page.Data[0].ID, page.Data[0].Attributes.State, nil
}

// isLiveAppInfoState mirrors isLiveVersionState in age_rating.go but lives
// here because file ownership keeps that function in age_rating.go's
// resource. Same logic; renaming would breach the boundary.
func isLiveAppInfoState(state string) bool {
	return state == "READY_FOR_DISTRIBUTION" || state == "READY_FOR_SALE"
}

// categoryRelationshipResp matches Apple's /relationships/<name> shape: a
// JSON:API to-one relationship envelope where Data is either an object
// {type, id} or null. We model it as a pointer so json.Unmarshal can
// distinguish "absent" (nil) from "present-but-empty".
type categoryRelationshipResp struct {
	Data *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"data"`
}

// fetchCategoryRelationship reads a single to-one category relationship off
// an appInfo (e.g. /v1/appInfos/{id}/relationships/primaryCategory) and
// returns the linked category id. Unassigned slots come back as a 200 with
// `data: null`; we surface that as "" + nil error rather than a fault.
//
// We use the /relationships/<name> path (linkage-only) rather than /<name>
// (full resource fetch) because we only need the id; one request, no extra
// payload, no rate-limit waste.
func fetchCategoryRelationship(ctx context.Context, c *asc.Client, appInfoID, relName string) (string, error) {
	path := "/v1/appInfos/" + appInfoID + "/relationships/" + relName
	resp, err := asc.Get[categoryRelationshipResp](ctx, c, path, nil)
	if err != nil {
		// 404 on a relationship is unusual but not fatal; treat as unassigned.
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return "", nil
		}
		return "", err
	}
	if resp.Data == nil {
		return "", nil
	}
	return resp.Data.ID, nil
}
