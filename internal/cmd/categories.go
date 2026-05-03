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
by --platform; defaults to IOS to match Flightline's default platform.

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
	Example: `  fline categories list
  fline categories list --platform MAC_OS
  fline categories list --output json | jq -r '.categories[].id'`,
}

var categoriesGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Show the category assignments on an app's editable appInfo",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCategoriesGet,
	Example: `  fline categories get com.example.myapp
  fline categories get com.example.myapp --output json | jq .primaryCategory`,
}

// categoriesSetCmd is the write verb for the categories group. It PATCHes
// /v1/appInfos/{id} with a relationships block keyed off the editable
// appInfo. Idempotent: read current assignments, diff, only PATCH if any
// requested slot differs from the current value.
var categoriesSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Assign categories to an app's editable appInfo (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runCategoriesSet,
	Long: `categories set updates an app's primary/secondary category assignments.

Apple stores the assignments on the app's editable appInfo as 6 to-one
relationships (primaryCategory, primarySubcategoryOne, primarySubcategoryTwo,
secondaryCategory, secondarySubcategoryOne, secondarySubcategoryTwo).
The categories must come from /v1/appCategories — see ` + "`fline categories list`" + `.

Only flags that are explicitly passed are written; omitted flags are left
untouched. To clear a slot pass --clear-secondary or one of its
sub-equivalents.

Idempotent: the command first reads the current assignments and only
PATCHes when at least one requested value differs from current.`,
	Example: `  fline categories set com.example.myapp --primary PRODUCTIVITY --primary-subcat BUSINESS
  fline categories set com.example.myapp --secondary UTILITIES
  fline categories set com.example.myapp --clear-secondary
  fline categories set com.example.myapp --primary GAMES --output json`,
}

var (
	categoriesListPlatform string

	categoriesSetPrimary           string
	categoriesSetSecondary         string
	categoriesSetPrimarySubOne     string
	categoriesSetPrimarySubTwo     string
	categoriesSetSecondarySubOne   string
	categoriesSetSecondarySubTwo   string
	categoriesSetClearSecondary    bool
	categoriesSetClearPrimarySub   bool
	categoriesSetClearSecondarySub bool
)

func init() {
	categoriesListCmd.Flags().StringVar(&categoriesListPlatform, "platform", "IOS", "platform filter (IOS|MAC_OS|TV_OS|VISION_OS); empty = all")

	categoriesSetCmd.Flags().StringVar(&categoriesSetPrimary, "primary", "", "primary category id (e.g. PRODUCTIVITY)")
	categoriesSetCmd.Flags().StringVar(&categoriesSetSecondary, "secondary", "", "secondary category id (e.g. UTILITIES)")
	categoriesSetCmd.Flags().StringVar(&categoriesSetPrimarySubOne, "primary-subcat", "", "primary subcategory (slot one)")
	categoriesSetCmd.Flags().StringVar(&categoriesSetPrimarySubTwo, "primary-subcat-two", "", "primary subcategory (slot two)")
	categoriesSetCmd.Flags().StringVar(&categoriesSetSecondarySubOne, "secondary-subcat", "", "secondary subcategory (slot one)")
	categoriesSetCmd.Flags().StringVar(&categoriesSetSecondarySubTwo, "secondary-subcat-two", "", "secondary subcategory (slot two)")
	categoriesSetCmd.Flags().BoolVar(&categoriesSetClearSecondary, "clear-secondary", false, "clear the secondary category and its subcategories")
	categoriesSetCmd.Flags().BoolVar(&categoriesSetClearPrimarySub, "clear-primary-subcat", false, "clear both primary subcategory slots")
	categoriesSetCmd.Flags().BoolVar(&categoriesSetClearSecondarySub, "clear-secondary-subcat", false, "clear both secondary subcategory slots")

	categoriesCmd.AddCommand(categoriesListCmd)
	categoriesCmd.AddCommand(categoriesGetCmd)
	categoriesCmd.AddCommand(categoriesSetCmd)
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

// CategoriesSetResult is the structured outcome of `categories set`. It
// surfaces what was diffed and whether a PATCH was issued, so machine
// consumers (Flightline plan/apply later) can detect idempotent no-ops without
// having to compare before/after themselves.
type CategoriesSetResult struct {
	BundleID     string                  `json:"bundleId"`
	AppInfoID    string                  `json:"appInfoId"`
	AppInfoState string                  `json:"appInfoState,omitempty"`
	Changed      bool                    `json:"changed"`
	Changes      []CategoriesFieldChange `json:"changes,omitempty"`
	Result       *CategoryAssignmentView `json:"result,omitempty"`
}

// CategoriesFieldChange names one slot that moved during a `categories set`.
// "Field" is one of the six relationship names. "From" is the previous id
// (empty for unassigned); "To" is the requested id (empty when clearing).
type CategoriesFieldChange struct {
	Field string `json:"field"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

// TableRows for a categories set result. Reads as: which slots moved, plus
// the post-PATCH assignments. When idempotent (Changed=false) the changes
// table renders one row that says "(no change)".
func (r *CategoriesSetResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", r.BundleID},
		{"APP_INFO_ID", r.AppInfoID},
		{"APP_INFO_STATE", r.AppInfoState},
		{"CHANGED", boolStr(r.Changed)},
	}
	if !r.Changed {
		rows = append(rows, []string{"NOTE", "no change (idempotent)"})
		return headers, rows
	}
	for _, ch := range r.Changes {
		rows = append(rows, []string{ch.Field, fmt.Sprintf("%s -> %s", categoryCell(ch.From), categoryCell(ch.To))})
	}
	return headers, rows
}

// runCategoriesSet implements the `categories set` write. Pattern:
//  1. resolve app + editable appInfo
//  2. fetch current 6 relationships
//  3. compute desired-state per slot from flags (only flags actually passed
//     contribute; clear flags zero out the matching slots)
//  4. diff vs current; if no slot moves, return idempotent no-op
//  5. PATCH /v1/appInfos/{id} with a single relationships block
//  6. return CategoriesSetResult including the post-PATCH view
func runCategoriesSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	appInfoID, state, err := pickEditableAppInfo(cmd.Context(), c, appID)
	if err != nil {
		return err
	}

	current, err := fetchAllCategoryRelationships(cmd.Context(), c, appInfoID)
	if err != nil {
		return err
	}

	desired, err := categoriesDesiredState(cmd, current)
	if err != nil {
		return err
	}

	changes := categoriesDiff(current, desired)
	result := &CategoriesSetResult{
		BundleID:     bundleID,
		AppInfoID:    appInfoID,
		AppInfoState: state,
	}

	if len(changes) == 0 {
		// Idempotent no-op. Return the current view as the result so JSON
		// consumers always have a coherent post-state to compare against.
		result.Changed = false
		result.Result = currentToAssignmentView(bundleID, appInfoID, state, current)
		return Render(result, outputMode())
	}

	body := buildAppInfoCategoriesPatch(appInfoID, desired, changes)
	if _, err := asc.Patch[asc.Single[asc.AppInfoAttributes]](
		cmd.Context(), c, "/v1/appInfos/"+appInfoID, nil, body,
	); err != nil {
		return err
	}

	result.Changed = true
	result.Changes = changes
	result.Result = currentToAssignmentView(bundleID, appInfoID, state, desired)
	return Render(result, outputMode())
}

// categoryRelationships is a small struct mirroring the 6 to-one relationship
// slots Apple exposes on appInfo. Empty value = unassigned.
type categoryRelationships struct {
	primary       string
	primarySubOne string
	primarySubTwo string
	secondary     string
	secondarySub1 string
	secondarySub2 string
}

// fetchAllCategoryRelationships pulls every category-slot relationship for
// an appInfo. Returns the empty struct's zero values when slots are
// unassigned. Surfaces a typed error naming the slot if any individual hop
// fails for a reason other than 404 (which is treated as unassigned by
// fetchCategoryRelationship).
func fetchAllCategoryRelationships(ctx context.Context, c *asc.Client, appInfoID string) (categoryRelationships, error) {
	var out categoryRelationships
	for _, rel := range []struct {
		name string
		dest *string
	}{
		{"primaryCategory", &out.primary},
		{"primarySubcategoryOne", &out.primarySubOne},
		{"primarySubcategoryTwo", &out.primarySubTwo},
		{"secondaryCategory", &out.secondary},
		{"secondarySubcategoryOne", &out.secondarySub1},
		{"secondarySubcategoryTwo", &out.secondarySub2},
	} {
		id, err := fetchCategoryRelationship(ctx, c, appInfoID, rel.name)
		if err != nil {
			return out, fmt.Errorf("categories: fetch %s: %w", rel.name, err)
		}
		*rel.dest = id
	}
	return out, nil
}

// categoriesDesiredState computes the desired post-set state from the flags
// the user actually passed. A flag that wasn't changed leaves the current
// value alone; an explicit --clear-* flag forces the matching slots empty.
//
// Returns an error if a clear flag and an explicit assignment flag for the
// same field are combined (caller error — surface immediately rather than
// silently picking one).
func categoriesDesiredState(cmd *cobra.Command, current categoryRelationships) (categoryRelationships, error) {
	d := current
	applyCategoriesAssignFlags(cmd, &d)
	if err := applyCategoriesClearFlags(cmd, &d); err != nil {
		return d, err
	}
	if d == current {
		return d, fmt.Errorf("categories: nothing to do — no slot flags supplied (try --primary, --secondary, --primary-subcat, --secondary-subcat, or any --clear-* flag)")
	}
	return d, nil
}

// applyCategoriesAssignFlags writes per-slot assignments from the explicit
// --primary / --secondary / *-subcat flags into d.
func applyCategoriesAssignFlags(cmd *cobra.Command, d *categoryRelationships) {
	flags := cmd.Flags()
	pairs := []struct {
		flag string
		dest *string
		raw  *string
	}{
		{"primary", &d.primary, &categoriesSetPrimary},
		{"secondary", &d.secondary, &categoriesSetSecondary},
		{"primary-subcat", &d.primarySubOne, &categoriesSetPrimarySubOne},
		{"primary-subcat-two", &d.primarySubTwo, &categoriesSetPrimarySubTwo},
		{"secondary-subcat", &d.secondarySub1, &categoriesSetSecondarySubOne},
		{"secondary-subcat-two", &d.secondarySub2, &categoriesSetSecondarySubTwo},
	}
	for _, p := range pairs {
		if flags.Changed(p.flag) {
			*p.dest = strings.TrimSpace(*p.raw)
		}
	}
}

// applyCategoriesClearFlags zeros out groups of slots when the matching
// --clear-* flag is set, surfacing an error if any explicit assignment
// flag for the same group is also passed.
func applyCategoriesClearFlags(cmd *cobra.Command, d *categoryRelationships) error {
	flags := cmd.Flags()
	if categoriesSetClearSecondary {
		if flags.Changed("secondary") || flags.Changed("secondary-subcat") || flags.Changed("secondary-subcat-two") {
			return fmt.Errorf("categories: --clear-secondary cannot be combined with --secondary / --secondary-subcat / --secondary-subcat-two")
		}
		d.secondary = ""
		d.secondarySub1 = ""
		d.secondarySub2 = ""
	}
	if categoriesSetClearPrimarySub {
		if flags.Changed("primary-subcat") || flags.Changed("primary-subcat-two") {
			return fmt.Errorf("categories: --clear-primary-subcat cannot be combined with --primary-subcat / --primary-subcat-two")
		}
		d.primarySubOne = ""
		d.primarySubTwo = ""
	}
	if categoriesSetClearSecondarySub {
		if flags.Changed("secondary-subcat") || flags.Changed("secondary-subcat-two") {
			return fmt.Errorf("categories: --clear-secondary-subcat cannot be combined with --secondary-subcat / --secondary-subcat-two")
		}
		d.secondarySub1 = ""
		d.secondarySub2 = ""
	}
	return nil
}

// categoriesDiff returns the per-field changes between current and desired.
func categoriesDiff(current, desired categoryRelationships) []CategoriesFieldChange {
	var changes []CategoriesFieldChange
	pairs := []struct {
		field string
		from  string
		to    string
	}{
		{"primaryCategory", current.primary, desired.primary},
		{"primarySubcategoryOne", current.primarySubOne, desired.primarySubOne},
		{"primarySubcategoryTwo", current.primarySubTwo, desired.primarySubTwo},
		{"secondaryCategory", current.secondary, desired.secondary},
		{"secondarySubcategoryOne", current.secondarySub1, desired.secondarySub1},
		{"secondarySubcategoryTwo", current.secondarySub2, desired.secondarySub2},
	}
	for _, p := range pairs {
		if p.from != p.to {
			changes = append(changes, CategoriesFieldChange{Field: p.field, From: p.from, To: p.to})
		}
	}
	return changes
}

// buildAppInfoCategoriesPatch crafts the JSON:API PATCH body for
// /v1/appInfos/{id} carrying only the relationship slots that changed.
// Apple accepts a partial relationships block — we explicitly send `null`
// for cleared slots and omit untouched ones to avoid clobbering server
// state we never observed (idempotency invariant: don't include fields
// you're not certain about).
func buildAppInfoCategoriesPatch(appInfoID string, desired categoryRelationships, changes []CategoriesFieldChange) map[string]any {
	rels := map[string]any{}
	changeSet := make(map[string]bool, len(changes))
	for _, ch := range changes {
		changeSet[ch.Field] = true
	}

	emit := func(field, value string) {
		if !changeSet[field] {
			return
		}
		if value == "" {
			rels[field] = map[string]any{"data": nil}
			return
		}
		rels[field] = map[string]any{
			"data": map[string]any{"type": "appCategories", "id": value},
		}
	}

	emit("primaryCategory", desired.primary)
	emit("primarySubcategoryOne", desired.primarySubOne)
	emit("primarySubcategoryTwo", desired.primarySubTwo)
	emit("secondaryCategory", desired.secondary)
	emit("secondarySubcategoryOne", desired.secondarySub1)
	emit("secondarySubcategoryTwo", desired.secondarySub2)

	return map[string]any{
		"data": map[string]any{
			"type":          "appInfos",
			"id":            appInfoID,
			"relationships": rels,
		},
	}
}

// currentToAssignmentView projects a categoryRelationships into the public
// CategoryAssignmentView shape so callers (and the Result.Result field) get
// a stable JSON view.
func currentToAssignmentView(bundleID, appInfoID, state string, r categoryRelationships) *CategoryAssignmentView {
	return &CategoryAssignmentView{
		BundleID:                bundleID,
		AppInfoID:               appInfoID,
		AppInfoState:            state,
		PrimaryCategory:         r.primary,
		PrimarySubcategoryOne:   r.primarySubOne,
		PrimarySubcategoryTwo:   r.primarySubTwo,
		SecondaryCategory:       r.secondary,
		SecondarySubcategoryOne: r.secondarySub1,
		SecondarySubcategoryTwo: r.secondarySub2,
	}
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
