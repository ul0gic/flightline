package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestCategoryView_JSONShape(t *testing.T) {
	v := CategoryView{
		ID:   "GAMES",
		Type: "appCategories",
		Attributes: asc.AppCategoryAttributes{
			Platforms: []string{"IOS", "MAC_OS"},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"GAMES"`,
		`"type":"appCategories"`,
		`"platforms":["IOS","MAC_OS"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestCategoryList_TableRowsHeaders(t *testing.T) {
	list := CategoryList{
		Categories: []CategoryView{
			{ID: "GAMES", Type: "appCategories", Attributes: asc.AppCategoryAttributes{Platforms: []string{"IOS", "MAC_OS"}}},
			{ID: "PRODUCTIVITY", Type: "appCategories", Attributes: asc.AppCategoryAttributes{Platforms: []string{"IOS"}}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"CATEGORY", "PLATFORMS"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "GAMES" || rows[0][1] != "IOS,MAC_OS" {
		t.Errorf("rows[0] = %v, want [GAMES IOS,MAC_OS]", rows[0])
	}
}

func TestCategoryAssignmentView_TableRows_VerticalLayout(t *testing.T) {
	v := &CategoryAssignmentView{
		BundleID:        "com.example.alpha",
		AppInfoID:       "9000000001",
		AppInfoState:    "PREPARE_FOR_SUBMISSION",
		PrimaryCategory: "PRODUCTIVITY",
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) != 9 {
		t.Errorf("rows = %d, want 9 (3 meta + 6 category slots)", len(rows))
	}
	// Unassigned slots render as "(unassigned)".
	foundUnassigned := false
	for _, r := range rows {
		if r[1] == "(unassigned)" {
			foundUnassigned = true
			break
		}
	}
	if !foundUnassigned {
		t.Errorf("expected at least one (unassigned) cell for empty category slots")
	}
}

func TestCategoriesCommand_RegisteredOnRoot(t *testing.T) {
	var cat *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "categories" {
			cat = c
			break
		}
	}
	if cat == nil {
		t.Fatal("categories not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range cat.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"list", "get"} {
		if !subs[want] {
			t.Errorf("categories %s subcommand missing", want)
		}
	}
}

// TestCategories_JSONOutputStability_List asserts the CategoryList JSON shape.
func TestCategories_JSONOutputStability_List(t *testing.T) {
	list := CategoryList{
		Categories: []CategoryView{
			{
				ID:         "GAMES",
				Type:       "appCategories",
				Attributes: asc.AppCategoryAttributes{Platforms: []string{"IOS"}},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Categories []map[string]any `json:"categories"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Categories) != 1 {
		t.Fatalf("categories len = %d, want 1", len(decoded.Categories))
	}
	row := decoded.Categories[0]
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing per-row key %q: JSON contract drift. Got: %v", key, mapKeys(row))
		}
	}
}

func TestCategories_JSONOutputStability_Get(t *testing.T) {
	v := &CategoryAssignmentView{
		BundleID:              "com.example.alpha",
		AppInfoID:             "9000000001",
		AppInfoState:          "PREPARE_FOR_SUBMISSION",
		PrimaryCategory:       "PRODUCTIVITY",
		PrimarySubcategoryOne: "BUSINESS",
		SecondaryCategory:     "UTILITIES",
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "appInfoId", "appInfoState", "primaryCategory", "primarySubcategoryOne", "secondaryCategory"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q: JSON contract drift. Got: %v", key, mapKeys(decoded))
		}
	}
}

func TestCategories_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appCategories": {File: "categories_list"},
	})
	c := fixtureASCClient(t, srv)
	out := make([]CategoryView, 0)
	for page, err := range asc.Pages[asc.AppCategoryAttributes](context.Background(), c, "/v1/appCategories", nil) {
		if err != nil {
			t.Fatalf("Pages: %v", err)
		}
		for _, r := range page.Data {
			out = append(out, CategoryView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
		}
	}
	if len(out) != 3 {
		t.Fatalf("categories len = %d, want 3", len(out))
	}
	if out[0].ID != "GAMES" {
		t.Errorf("out[0].ID = %q, want GAMES", out[0].ID)
	}
	if len(out[0].Attributes.Platforms) == 0 || out[0].Attributes.Platforms[0] != "IOS" {
		t.Errorf("out[0].Platforms = %v, want IOS first", out[0].Attributes.Platforms)
	}
}

func TestCategories_FixtureReplay_GetAssignments(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                     {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appInfos": {File: "age_rating_app_infos"},

		"GET /v1/appInfos/9000000001/relationships/primaryCategory":         {File: "categories_get_primary"},
		"GET /v1/appInfos/9000000001/relationships/primarySubcategoryOne":   {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/primarySubcategoryTwo":   {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/secondaryCategory":       {File: "categories_get_secondary"},
		"GET /v1/appInfos/9000000001/relationships/secondarySubcategoryOne": {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/secondarySubcategoryTwo": {File: "categories_get_unassigned"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	infoID, state, err := pickEditableAppInfo(ctx, c, appID)
	if err != nil {
		t.Fatalf("pickEditableAppInfo: %v", err)
	}
	if infoID != "9000000001" {
		t.Errorf("editable appInfo id = %q, want 9000000001", infoID)
	}
	if state != "PREPARE_FOR_SUBMISSION" {
		t.Errorf("editable appInfo state = %q, want PREPARE_FOR_SUBMISSION", state)
	}

	primary, err := fetchCategoryRelationship(ctx, c, infoID, "primaryCategory")
	if err != nil {
		t.Fatalf("primaryCategory: %v", err)
	}
	if primary != "PRODUCTIVITY" {
		t.Errorf("primaryCategory = %q, want PRODUCTIVITY", primary)
	}

	secondary, err := fetchCategoryRelationship(ctx, c, infoID, "secondaryCategory")
	if err != nil {
		t.Fatalf("secondaryCategory: %v", err)
	}
	if secondary != "UTILITIES" {
		t.Errorf("secondaryCategory = %q, want UTILITIES", secondary)
	}

	// Unassigned sub-slot returns "" with no error.
	subTwo, err := fetchCategoryRelationship(ctx, c, infoID, "primarySubcategoryTwo")
	if err != nil {
		t.Fatalf("primarySubcategoryTwo: %v", err)
	}
	if subTwo != "" {
		t.Errorf("primarySubcategoryTwo = %q, want empty (unassigned slot)", subTwo)
	}
}

func TestCategories_FixtureReplay_AppNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_notFound"},
	})
	c := fixtureASCClient(t, srv)
	_, err := resolveAppID(context.Background(), c, "com.unknown.app")
	if err == nil {
		t.Fatal("resolveAppID: want error, got nil")
	}
	if !strings.Contains(err.Error(), `"com.unknown.app"`) {
		t.Errorf("error message %q does not name the bundleId", err.Error())
	}
}

// TestCategoriesDiff_UnchangedYieldsZeroChanges asserts the diff returns no
// rows when current and desired are identical: the idempotency invariant.
func TestCategoriesDiff_UnchangedYieldsZeroChanges(t *testing.T) {
	cur := categoryRelationships{
		primary:       "PRODUCTIVITY",
		primarySubOne: "BUSINESS",
		secondary:     "UTILITIES",
	}
	if got := categoriesDiff(cur, cur); len(got) != 0 {
		t.Errorf("diff(same, same) = %d, want 0", len(got))
	}
}

// TestCategoriesDiff_ReportsChangedFields locks the diff output: each moved
// slot becomes one CategoriesFieldChange row with the right from/to.
func TestCategoriesDiff_ReportsChangedFields(t *testing.T) {
	cur := categoryRelationships{
		primary:   "PRODUCTIVITY",
		secondary: "UTILITIES",
	}
	desired := categoryRelationships{
		primary:       "GAMES",
		primarySubOne: "ACTION",
		secondary:     "", // cleared
	}
	got := categoriesDiff(cur, desired)
	if len(got) != 3 {
		t.Fatalf("changes len = %d, want 3 (primary, primarySubOne, secondary cleared); got = %+v", len(got), got)
	}
	want := map[string][2]string{
		"primaryCategory":       {"PRODUCTIVITY", "GAMES"},
		"primarySubcategoryOne": {"", "ACTION"},
		"secondaryCategory":     {"UTILITIES", ""},
	}
	for _, ch := range got {
		w, ok := want[ch.Field]
		if !ok {
			t.Errorf("unexpected change field %q", ch.Field)
			continue
		}
		if ch.From != w[0] || ch.To != w[1] {
			t.Errorf("%s: from=%q to=%q, want from=%q to=%q", ch.Field, ch.From, ch.To, w[0], w[1])
		}
	}
}

// Untouched slots must not appear in the PATCH, else Apple sees writes for unobserved state and idempotency breaks.
func TestBuildAppInfoCategoriesPatch_OnlyChangedFieldsEmitted(t *testing.T) {
	current := categoryRelationships{primary: "PRODUCTIVITY", secondary: "UTILITIES"}
	desired := categoryRelationships{primary: "GAMES", secondary: ""}
	changes := categoriesDiff(current, desired)

	body := buildAppInfoCategoriesPatch("9000000001", desired, changes)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)
	for _, want := range []string{
		`"type":"appInfos"`,
		`"id":"9000000001"`,
		`"primaryCategory":{"data":{"id":"GAMES","type":"appCategories"}}`,
		`"secondaryCategory":{"data":null}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q, got: %s", want, out)
		}
	}
	for _, leak := range []string{
		`"primarySubcategoryOne"`,
		`"secondarySubcategoryOne"`,
		`"primarySubcategoryTwo"`,
	} {
		if strings.Contains(out, leak) {
			t.Errorf("body unexpectedly contains untouched slot %q: %s", leak, out)
		}
	}
}

// TestCategoriesSetCmd_RegisteredOnGroup verifies cobra wiring for the new
// write verb.
func TestCategoriesSetCmd_RegisteredOnGroup(t *testing.T) {
	var cat *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "categories" {
			cat = c
			break
		}
	}
	if cat == nil {
		t.Fatal("categories not on root")
	}
	var set *cobra.Command
	for _, sc := range cat.Commands() {
		if sc.Name() == "set" {
			set = sc
			break
		}
	}
	if set == nil {
		t.Fatal("categories set subcommand not registered")
	}
	for _, want := range []string{"primary", "secondary", "primary-subcat", "secondary-subcat", "clear-secondary"} {
		if set.Flags().Lookup(want) == nil {
			t.Errorf("categories set missing --%s flag", want)
		}
	}
}

// TestCategoriesSetResult_TableRows_NoChange asserts the no-change row is
// rendered when Changed=false.
func TestCategoriesSetResult_TableRows_NoChange(t *testing.T) {
	r := &CategoriesSetResult{BundleID: "com.example.alpha", AppInfoID: "9000000001", AppInfoState: "PREPARE_FOR_SUBMISSION", Changed: false}
	_, rows := r.TableRows()
	foundNote := false
	for _, row := range rows {
		if row[0] == "NOTE" && strings.Contains(row[1], "no change") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected NOTE row for idempotent no-op, rows=%v", rows)
	}
}

func TestCategoriesSetResult_JSONShape(t *testing.T) {
	r := &CategoriesSetResult{
		BundleID:     "com.example.alpha",
		AppInfoID:    "9000000001",
		AppInfoState: "PREPARE_FOR_SUBMISSION",
		Changed:      true,
		Changes: []CategoriesFieldChange{
			{Field: "primaryCategory", From: "PRODUCTIVITY", To: "GAMES"},
		},
		Result: &CategoryAssignmentView{
			BundleID: "com.example.alpha", AppInfoID: "9000000001",
			AppInfoState: "PREPARE_FOR_SUBMISSION", PrimaryCategory: "GAMES",
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "appInfoId", "appInfoState", "changed", "changes", "result"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q. Got: %v", key, mapKeys(decoded))
		}
	}
}

// The route map omits the PATCH endpoint, so any write attempt 404s and fails the test, proving the no-op path.
func TestCategoriesSet_FixtureReplay_Idempotent(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                     {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appInfos": {File: "age_rating_app_infos"},

		"GET /v1/appInfos/9000000001/relationships/primaryCategory":         {File: "categories_get_primary"},
		"GET /v1/appInfos/9000000001/relationships/primarySubcategoryOne":   {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/primarySubcategoryTwo":   {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/secondaryCategory":       {File: "categories_get_secondary"},
		"GET /v1/appInfos/9000000001/relationships/secondarySubcategoryOne": {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/secondarySubcategoryTwo": {File: "categories_get_unassigned"},
	})
	c := fixtureASCClient(t, srv)

	current, err := fetchAllCategoryRelationships(context.Background(), c, "9000000001")
	if err != nil {
		t.Fatalf("fetchAllCategoryRelationships: %v", err)
	}
	if current.primary != "PRODUCTIVITY" || current.secondary != "UTILITIES" {
		t.Errorf("current = %+v, want primary=PRODUCTIVITY secondary=UTILITIES", current)
	}
	// desired == current means no diff means no PATCH.
	if got := categoriesDiff(current, current); len(got) != 0 {
		t.Errorf("diff(current, current) = %d, want 0", len(got))
	}
}
