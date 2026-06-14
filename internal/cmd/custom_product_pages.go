package cmd

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// CustomProductPageView is one row of the custom-product-pages list output.
// Current* fields come from the most-recent version (highest version by lex order).
type CustomProductPageView struct {
	ID             string                             `json:"id"`
	Type           string                             `json:"type"`
	Attributes     asc.AppCustomProductPageAttributes `json:"attributes"`
	CurrentVersion string                             `json:"currentVersion,omitempty"`
	CurrentState   string                             `json:"currentState,omitempty"`
}

// CustomProductPageList is the table-aware view for `custom-product-pages list`.
type CustomProductPageList struct {
	Pages []CustomProductPageView `json:"pages"`
}

// TableRows implements TableRenderable for the pages list view.
func (l CustomProductPageList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"NAME", "VISIBLE", "VERSION", "STATE", "ID"}
	rows = make([][]string, 0, len(l.Pages))
	for i := range l.Pages {
		p := &l.Pages[i]
		rows = append(rows, []string{
			p.Attributes.Name,
			boolPtrStr(p.Attributes.Visible),
			p.CurrentVersion,
			p.CurrentState,
			p.ID,
		})
	}
	return headers, rows
}

// CustomProductPageDetail is the read-side view for `custom-product-pages get`.
type CustomProductPageDetail struct {
	ID            string                              `json:"id"`
	Type          string                              `json:"type"`
	Attributes    asc.AppCustomProductPageAttributes  `json:"attributes"`
	Versions      []CustomProductPageVersionView      `json:"versions"`
	Localizations []CustomProductPageLocalizationView `json:"localizations"`
}

// CustomProductPageVersionView is one row in CustomProductPageDetail.Versions.
type CustomProductPageVersionView struct {
	ID         string                                    `json:"id"`
	Type       string                                    `json:"type"`
	Attributes asc.AppCustomProductPageVersionAttributes `json:"attributes"`
}

// CustomProductPageLocalizationView is one row in CustomProductPageDetail.Localizations.
type CustomProductPageLocalizationView struct {
	ID         string                                         `json:"id"`
	Type       string                                         `json:"type"`
	Attributes asc.AppCustomProductPageLocalizationAttributes `json:"attributes"`
}

func (v *CustomProductPageDetail) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = make([][]string, 0, 6+len(v.Versions)+len(v.Localizations))
	rows = append(rows,
		[]string{"ID", v.ID},
		[]string{"NAME", v.Attributes.Name},
		[]string{"URL", v.Attributes.URL},
		[]string{"VISIBLE", boolPtrStr(v.Attributes.Visible)},
		[]string{"VERSIONS", strconv.Itoa(len(v.Versions))},
		[]string{"LOCALIZATIONS", strconv.Itoa(len(v.Localizations))},
	)
	for i := range v.Versions {
		ver := &v.Versions[i]
		rows = append(rows, []string{
			"VERSION:" + ver.Attributes.Version,
			ver.Attributes.State,
		})
	}
	for i := range v.Localizations {
		loc := &v.Localizations[i]
		rows = append(rows, []string{
			"LOCALE:" + loc.Attributes.Locale,
			truncate(loc.Attributes.PromotionalText, 60),
		})
	}
	return headers, rows
}

// truncate trims a string to maxLen runes, appending "…" when cut.
func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "…"
}

var customProductPagesCmd = &cobra.Command{
	Use:   "custom-product-pages",
	Short: "Inspect App Store Custom Product Pages",
	Long: `custom-product-pages groups read commands over Apple's
AppCustomProductPage resources: alternate App Store listings used to
target ad-driven traffic with different screenshots and descriptions.

  list <bundleId>          : list all configured pages with current state
  get  <bundleId> --page <id>
                           : detail for one page (versions + localizations)`,
}

var customProductPagesListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List custom product pages for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCustomProductPagesList,
	Example: `  flightline custom-product-pages list com.example.myapp
  flightline custom-product-pages list com.example.myapp --output json | jq -r '.pages[].attributes.name'`,
}

var customProductPagesGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Detail for one custom product page (versions + localizations)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCustomProductPagesGet,
	Example: `  flightline custom-product-pages get com.example.myapp --page 8000000001
  flightline custom-product-pages get com.example.myapp --page 8000000001 --output json`,
}

// customProductPagesCreateCmd creates an AppCustomProductPage. Idempotent on
// (app, name): an existing same-name page returns changed=false, no POST.
var customProductPagesCreateCmd = &cobra.Command{
	Use:          "create <bundleId>",
	Short:        "Create a custom product page (idempotent on name)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCustomProductPagesCreate,
	Example: `  flightline custom-product-pages create com.example.myapp --name "Holiday Promo"
  flightline custom-product-pages create com.example.myapp --name "Spring 2026" --output json`,
}

// customProductPagesUpdateCmd PATCHes mutable attributes (name, visible).
// Idempotent: only PATCHes when a supplied attribute differs from current.
var customProductPagesUpdateCmd = &cobra.Command{
	Use:          "update <pageId>",
	Short:        "Update a custom product page's mutable attributes (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCustomProductPagesUpdate,
	Example: `  flightline custom-product-pages update CPP-1 --visible
  flightline custom-product-pages update CPP-1 --name "Updated Holiday"`,
}

// customProductPagesDeleteCmd deletes a custom product page. Idempotent:
// 404 (already absent) reports changed=false rather than erroring.
var customProductPagesDeleteCmd = &cobra.Command{
	Use:          "delete <pageId>",
	Short:        "Delete a custom product page (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCustomProductPagesDelete,
	Example:      `  flightline custom-product-pages delete CPP-1`,
}

var (
	customProductPagesListLimit int
	customProductPagesGetPage   string

	customProductPagesCreateName string

	customProductPagesUpdateName    string
	customProductPagesUpdateVisible bool
)

func init() {
	customProductPagesListCmd.Flags().IntVar(&customProductPagesListLimit, "limit", 0, "max pages to emit (0 = no cap)")

	customProductPagesGetCmd.Flags().StringVar(&customProductPagesGetPage, "page", "", "AppCustomProductPage ID to fetch")
	_ = customProductPagesGetCmd.MarkFlagRequired("page")

	customProductPagesCreateCmd.Flags().StringVar(&customProductPagesCreateName, "name", "", "developer-friendly page name (must be unique per app)")
	_ = customProductPagesCreateCmd.MarkFlagRequired("name")

	customProductPagesUpdateCmd.Flags().StringVar(&customProductPagesUpdateName, "name", "", "rename the page")
	customProductPagesUpdateCmd.Flags().BoolVar(&customProductPagesUpdateVisible, "visible", false, "set visibility (true = public)")

	customProductPagesCmd.AddCommand(customProductPagesListCmd)
	customProductPagesCmd.AddCommand(customProductPagesGetCmd)
	customProductPagesCmd.AddCommand(customProductPagesCreateCmd)
	customProductPagesCmd.AddCommand(customProductPagesUpdateCmd)
	customProductPagesCmd.AddCommand(customProductPagesDeleteCmd)
	rootCmd.AddCommand(customProductPagesCmd)
}

func runCustomProductPagesList(cmd *cobra.Command, args []string) error {
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
	views, err := collectCustomProductPages(cmd.Context(), c, "/v1/apps/"+appID+"/appCustomProductPages", q, customProductPagesListLimit)
	if err != nil {
		return err
	}

	// One version lookup per page; cap at 50 so apps with dozens of CPPs
	// don't burn the per-hour rate-limit quota. Beyond 50, fields stay empty.
	const versionLookupCap = 50
	for i := range views {
		if i >= versionLookupCap {
			break
		}
		ver, vstate, verr := fetchCurrentCustomProductPageVersion(cmd.Context(), c, views[i].ID)
		if verr == nil {
			views[i].CurrentVersion = ver
			views[i].CurrentState = vstate
		}
	}

	return Render(CustomProductPageList{Pages: views}, outputMode())
}

func runCustomProductPagesGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	pageID := strings.TrimSpace(customProductPagesGetPage)
	if pageID == "" {
		return errors.New("custom-product-pages: --page is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	// Resolve the app so a typo in --page surfaces against the right app.
	if _, err := resolveAppID(cmd.Context(), c, bundleID); err != nil {
		return err
	}

	pageResp, err := asc.Get[asc.Single[asc.AppCustomProductPageAttributes]](
		cmd.Context(), c, "/v1/appCustomProductPages/"+pageID, nil,
	)
	if err != nil {
		return err
	}

	versions, err := collectCustomProductPageVersions(cmd.Context(), c, pageID, 0)
	if err != nil {
		return err
	}

	// Highest version string wins; Apple's are monotonic ints, lex-compare is safe.
	var current *CustomProductPageVersionView
	for i := range versions {
		v := &versions[i]
		if current == nil || v.Attributes.Version > current.Attributes.Version {
			current = v
		}
	}

	var locs []CustomProductPageLocalizationView
	if current != nil {
		locs, err = collectCustomProductPageLocalizations(cmd.Context(), c, current.ID, 0)
		if err != nil {
			return err
		}
	}

	view := &CustomProductPageDetail{
		ID:            pageResp.Data.ID,
		Type:          pageResp.Data.Type,
		Attributes:    pageResp.Data.Attributes,
		Versions:      versions,
		Localizations: locs,
	}
	return Render(view, outputMode())
}

// fetchCurrentCustomProductPageVersion returns the highest-version row's
// (version, state) for the page.
func fetchCurrentCustomProductPageVersion(ctx context.Context, c *asc.Client, pageID string) (version, state string, err error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageVersionAttributes]](
		ctx, c, "/v1/appCustomProductPages/"+pageID+"/appCustomProductPageVersions", q,
	)
	if err != nil {
		return "", "", err
	}
	for _, r := range page.Data {
		if r.Attributes.Version > version {
			version = r.Attributes.Version
			state = r.Attributes.State
		}
	}
	return version, state, nil
}

// collectCustomProductPages walks the paging iterator and returns flattened
// CustomProductPageView rows.
func collectCustomProductPages(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]CustomProductPageView, error) {
	out := make([]CustomProductPageView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.AppCustomProductPageAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, CustomProductPageView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// collectCustomProductPageVersions walks a page's versions iterator.
func collectCustomProductPageVersions(ctx context.Context, c *asc.Client, pageID string, limit int) ([]CustomProductPageVersionView, error) {
	out := make([]CustomProductPageVersionView, 0, defaultListCap(limit))
	q := url.Values{"limit": {"50"}}
	path := "/v1/appCustomProductPages/" + pageID + "/appCustomProductPageVersions"
	for page, err := range asc.Pages[asc.AppCustomProductPageVersionAttributes](ctx, c, path, q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, CustomProductPageVersionView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// collectCustomProductPageLocalizations walks a version's localizations
// iterator.
func collectCustomProductPageLocalizations(ctx context.Context, c *asc.Client, versionID string, limit int) ([]CustomProductPageLocalizationView, error) {
	out := make([]CustomProductPageLocalizationView, 0, defaultListCap(limit))
	q := url.Values{"limit": {"200"}}
	path := "/v1/appCustomProductPageVersions/" + versionID + "/appCustomProductPageLocalizations"
	for page, err := range asc.Pages[asc.AppCustomProductPageLocalizationAttributes](ctx, c, path, q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, CustomProductPageLocalizationView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// CustomProductPageSetResult is the outcome of create / update; Changed reports
// whether a write was issued.
type CustomProductPageSetResult struct {
	PageID     string                             `json:"pageId"`
	Changed    bool                               `json:"changed"`
	Created    bool                               `json:"created,omitempty"`
	Note       string                             `json:"note,omitempty"`
	Attributes asc.AppCustomProductPageAttributes `json:"attributes"`
}

// TableRows for a custom-product-page set result.
func (r *CustomProductPageSetResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"PAGE_ID", r.PageID},
		{"CHANGED", boolStrCPP(r.Changed)},
		{"CREATED", boolStrCPP(r.Created)},
		{"NAME", r.Attributes.Name},
		{"VISIBLE", boolPtrStr(r.Attributes.Visible)},
		{"URL", r.Attributes.URL},
	}
	if r.Note != "" {
		rows = append(rows, []string{"NOTE", r.Note})
	}
	return headers, rows
}

// CustomProductPageDeleteResult is the structured outcome of
// `custom-product-pages delete`.
type CustomProductPageDeleteResult struct {
	PageID  string `json:"pageId"`
	Changed bool   `json:"changed"`
	Note    string `json:"note,omitempty"`
}

// TableRows for the delete result.
func (r *CustomProductPageDeleteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"PAGE_ID", r.PageID},
		{"CHANGED", boolStrCPP(r.Changed)},
	}
	if r.Note != "" {
		rows = append(rows, []string{"NOTE", r.Note})
	}
	return headers, rows
}

func boolStrCPP(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func runCustomProductPagesCreate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	name := strings.TrimSpace(customProductPagesCreateName)
	if name == "" {
		return errors.New("custom-product-pages: --name is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	existing, err := findCustomProductPageByName(cmd.Context(), c, appID, name)
	if err != nil {
		return err
	}
	if existing != nil {
		return Render(&CustomProductPageSetResult{
			PageID:     existing.ID,
			Changed:    false,
			Created:    false,
			Note:       "no change (idempotent): page with same name already exists",
			Attributes: existing.Attributes,
		}, outputMode())
	}

	body := buildCustomProductPageCreate(appID, name)
	resp, err := asc.Post[asc.Single[asc.AppCustomProductPageAttributes]](
		cmd.Context(), c, "/v1/appCustomProductPages", nil, body,
	)
	if err != nil {
		return err
	}
	return Render(&CustomProductPageSetResult{
		PageID:     resp.Data.ID,
		Changed:    true,
		Created:    true,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

func runCustomProductPagesUpdate(cmd *cobra.Command, args []string) error {
	pageID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	cur, err := asc.Get[asc.Single[asc.AppCustomProductPageAttributes]](
		cmd.Context(), c, "/v1/appCustomProductPages/"+pageID, nil,
	)
	if err != nil {
		return err
	}

	patchAttrs := computeCustomProductPagePatchAttrs(cmd, cur.Data.Attributes)
	if len(patchAttrs) == 0 {
		return Render(&CustomProductPageSetResult{
			PageID:     pageID,
			Changed:    false,
			Note:       "no change (idempotent): all requested attributes already match",
			Attributes: cur.Data.Attributes,
		}, outputMode())
	}

	body := map[string]any{
		"data": map[string]any{
			"type":       "appCustomProductPages",
			"id":         pageID,
			"attributes": patchAttrs,
		},
	}
	resp, err := asc.Patch[asc.Single[asc.AppCustomProductPageAttributes]](
		cmd.Context(), c, "/v1/appCustomProductPages/"+pageID, nil, body,
	)
	if err != nil {
		return err
	}
	return Render(&CustomProductPageSetResult{
		PageID:     pageID,
		Changed:    true,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

// computeCustomProductPagePatchAttrs builds the partial PATCH attributes.
// Only passed flags contribute, and same-value flags are filtered for idempotency.
func computeCustomProductPagePatchAttrs(cmd *cobra.Command, cur asc.AppCustomProductPageAttributes) map[string]any {
	patch := map[string]any{}
	flags := cmd.Flags()
	if flags.Changed("name") {
		newName := strings.TrimSpace(customProductPagesUpdateName)
		if newName != cur.Name {
			patch["name"] = newName
		}
	}
	if flags.Changed("visible") {
		curVal := false
		if cur.Visible != nil {
			curVal = *cur.Visible
		}
		if curVal != customProductPagesUpdateVisible {
			patch["visible"] = customProductPagesUpdateVisible
		}
	}
	return patch
}

// runCustomProductPagesDelete deletes a page. Idempotent: a 404 reports
// changed=false rather than erroring so delete-script re-runs are safe.
func runCustomProductPagesDelete(cmd *cobra.Command, args []string) error {
	pageID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}
	if err := c.Delete(cmd.Context(), "/v1/appCustomProductPages/"+pageID, nil); err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return Render(&CustomProductPageDeleteResult{
				PageID:  pageID,
				Changed: false,
				Note:    "no change (idempotent): page already absent",
			}, outputMode())
		}
		return err
	}
	return Render(&CustomProductPageDeleteResult{
		PageID:  pageID,
		Changed: true,
	}, outputMode())
}

// findCustomProductPageByName returns the first page whose name matches
// (case-sensitive), or (nil, nil) when none match.
func findCustomProductPageByName(ctx context.Context, c *asc.Client, appID, name string) (*asc.Resource[asc.AppCustomProductPageAttributes], error) {
	q := url.Values{"limit": {"200"}}
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appCustomProductPages", q,
	)
	if err != nil {
		return nil, err
	}
	for i := range page.Data {
		if page.Data[i].Attributes.Name == name {
			return &page.Data[i], nil
		}
	}
	return nil, nil
}

// buildCustomProductPageCreate crafts the POST body with only the required
// (name, app) fields; versions/localizations are created via subresources later.
func buildCustomProductPageCreate(appID, name string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type":       "appCustomProductPages",
			"attributes": map[string]any{"name": name},
			"relationships": map[string]any{
				"app": map[string]any{
					"data": map[string]any{"type": "apps", "id": appID},
				},
			},
		},
	}
}
