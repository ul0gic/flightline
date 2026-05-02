package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
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
			t.Errorf("missing per-row key %q — JSON contract drift. Got: %v", key, mapKeys(row))
		}
	}
}

// TestCategories_JSONOutputStability_Get locks the CategoryAssignmentView
// top-level keys for the `get` command.
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
			t.Errorf("missing top-level key %q — JSON contract drift. Got: %v", key, mapKeys(decoded))
		}
	}
}

// TestCategories_FixtureReplay_List exercises runCategoriesList's underlying
// pages walk: GET /v1/appCategories with the platform filter.
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

// TestCategories_FixtureReplay_GetAssignments exercises the full chain:
// resolveAppID → pickEditableAppInfo → fetchCategoryRelationship across the 6
// to-one relationships, including an unassigned slot (data: null).
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

// TestCategories_FixtureReplay_AppNotFound exercises the resolveAppID error
// path: bundleId that doesn't exist surfaces a typed error message naming
// the bundleId.
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
