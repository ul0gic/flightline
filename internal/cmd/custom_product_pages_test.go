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

func TestCustomProductPageView_JSONShape(t *testing.T) {
	visible := true
	v := CustomProductPageView{
		ID:   "CPP-1",
		Type: "appCustomProductPages",
		Attributes: asc.AppCustomProductPageAttributes{
			Name:    "Holiday Promo",
			URL:     "https://apps.apple.com/app/id1234567890?ppid=CPP-1",
			Visible: &visible,
		},
		CurrentVersion: "2",
		CurrentState:   "APPROVED",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"CPP-1"`,
		`"type":"appCustomProductPages"`,
		`"name":"Holiday Promo"`,
		`"visible":true`,
		`"currentVersion":"2"`,
		`"currentState":"APPROVED"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestCustomProductPageList_TableRowsHeaders(t *testing.T) {
	visible := true
	list := CustomProductPageList{
		Pages: []CustomProductPageView{
			{ID: "CPP-1", Attributes: asc.AppCustomProductPageAttributes{Name: "Holiday", Visible: &visible}, CurrentVersion: "2", CurrentState: "APPROVED"},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"NAME", "VISIBLE", "VERSION", "STATE", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][2] != "2" {
		t.Errorf("rows[0][VERSION] = %q, want 2", rows[0][2])
	}
	if rows[0][3] != "APPROVED" {
		t.Errorf("rows[0][STATE] = %q, want APPROVED", rows[0][3])
	}
}

func TestCustomProductPageDetail_TableRowsVerticalLayout(t *testing.T) {
	visible := true
	v := &CustomProductPageDetail{
		ID:   "CPP-1",
		Type: "appCustomProductPages",
		Attributes: asc.AppCustomProductPageAttributes{
			Name: "Holiday", Visible: &visible,
		},
		Versions: []CustomProductPageVersionView{
			{ID: "CPPV-2", Attributes: asc.AppCustomProductPageVersionAttributes{Version: "2", State: "APPROVED"}},
		},
		Localizations: []CustomProductPageLocalizationView{
			{ID: "CPPL-EN", Attributes: asc.AppCustomProductPageLocalizationAttributes{Locale: "en-US", PromotionalText: "Limited time holiday savings — 50% off Pro!"}},
		},
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 8 {
		t.Errorf("rows = %d, want >= 8 (header + version row + locale row)", len(rows))
	}
}

func TestTruncate_Boundaries(t *testing.T) {
	cases := []struct {
		in  string
		max int
		out string
	}{
		{"short", 60, "short"},
		{"hello world hello world", 5, "hello…"},
		{"", 10, ""},
	}
	for _, c := range cases {
		got := truncate(c.in, c.max)
		if got != c.out {
			t.Errorf("truncate(%q,%d) = %q, want %q", c.in, c.max, got, c.out)
		}
	}
}

func TestCustomProductPagesCommand_RegisteredOnRoot(t *testing.T) {
	var cpp *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "custom-product-pages" {
			cpp = c
			break
		}
	}
	if cpp == nil {
		t.Fatal("custom-product-pages not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range cpp.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"list", "get"} {
		if !subs[want] {
			t.Errorf("custom-product-pages %s subcommand missing", want)
		}
	}
}

// TestCustomProductPages_JSONOutputStability_List asserts the list shape.
func TestCustomProductPages_JSONOutputStability_List(t *testing.T) {
	visible := true
	list := CustomProductPageList{
		Pages: []CustomProductPageView{
			{ID: "CPP-1", Type: "appCustomProductPages", Attributes: asc.AppCustomProductPageAttributes{Name: "Promo", Visible: &visible}},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Pages []map[string]any `json:"pages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Pages) != 1 {
		t.Fatalf("pages len = %d, want 1", len(decoded.Pages))
	}
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := decoded.Pages[0][key]; !ok {
			t.Errorf("missing per-row key %q — JSON contract drift", key)
		}
	}
}

// TestCustomProductPages_JSONOutputStability_Get locks the detail shape.
func TestCustomProductPages_JSONOutputStability_Get(t *testing.T) {
	v := &CustomProductPageDetail{
		ID:            "CPP-1",
		Type:          "appCustomProductPages",
		Attributes:    asc.AppCustomProductPageAttributes{Name: "Promo"},
		Versions:      []CustomProductPageVersionView{},
		Localizations: []CustomProductPageLocalizationView{},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"id", "type", "attributes", "versions", "localizations"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q — JSON contract drift", key)
		}
	}
}

// TestCustomProductPages_FixtureReplay_List exercises collectCustomProductPages
// against the fixture: 2 pages.
func TestCustomProductPages_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appCustomProductPages": {File: "custom_product_pages_list"},

		// Per-page version lookup the list flow performs.
		"GET /v1/appCustomProductPages/CPP-1/appCustomProductPageVersions": {File: "custom_product_pages_versions"},
		"GET /v1/appCustomProductPages/CPP-2/appCustomProductPageVersions": {File: "custom_product_pages_versions"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectCustomProductPages(ctx, c, "/v1/apps/"+appID+"/appCustomProductPages", nil, 0)
	if err != nil {
		t.Fatalf("collectCustomProductPages: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("pages len = %d, want 2", len(views))
	}
	if views[0].Attributes.Name != "Holiday Promo" {
		t.Errorf("pages[0].Name = %q, want Holiday Promo", views[0].Attributes.Name)
	}
	// Try a per-page version lookup.
	ver, state, err := fetchCurrentCustomProductPageVersion(ctx, c, views[0].ID)
	if err != nil {
		t.Fatalf("fetchCurrentCustomProductPageVersion: %v", err)
	}
	if ver != "2" {
		t.Errorf("currentVersion = %q, want 2", ver)
	}
	if state != "APPROVED" {
		t.Errorf("currentState = %q, want APPROVED", state)
	}
}

// TestCustomProductPages_FixtureReplay_GetDetail exercises the full chain:
// page get + versions list + localizations list (on the highest version).
func TestCustomProductPages_FixtureReplay_GetDetail(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                        {File: "apps_get_byBundleId"},
		"GET /v1/appCustomProductPages/CPP-1": {File: "custom_product_pages_get"},
		"GET /v1/appCustomProductPages/CPP-1/appCustomProductPageVersions":              {File: "custom_product_pages_versions"},
		"GET /v1/appCustomProductPageVersions/CPPV-2/appCustomProductPageLocalizations": {File: "custom_product_pages_localizations"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	if _, err := resolveAppID(ctx, c, "com.example.alpha"); err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	pageResp, err := asc.Get[asc.Single[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/appCustomProductPages/CPP-1", nil,
	)
	if err != nil {
		t.Fatalf("page get: %v", err)
	}
	if pageResp.Data.Attributes.Name != "Holiday Promo" {
		t.Errorf("page name = %q, want Holiday Promo", pageResp.Data.Attributes.Name)
	}
	versions, err := collectCustomProductPageVersions(ctx, c, "CPP-1", 0)
	if err != nil {
		t.Fatalf("collectCustomProductPageVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions len = %d, want 2", len(versions))
	}
	// The "current" version is CPPV-2 (version "2", APPROVED).
	locs, err := collectCustomProductPageLocalizations(ctx, c, "CPPV-2", 0)
	if err != nil {
		t.Fatalf("collectCustomProductPageLocalizations: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("localizations len = %d, want 2", len(locs))
	}
	if locs[0].Attributes.Locale != "en-US" {
		t.Errorf("locs[0].Locale = %q, want en-US", locs[0].Attributes.Locale)
	}
}

// TestCustomProductPages_FixtureReplay_AppNotFound asserts the error names
// the bundleId.
func TestCustomProductPages_FixtureReplay_AppNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_notFound"},
	})
	c := fixtureASCClient(t, srv)
	_, err := resolveAppID(context.Background(), c, "com.unknown.app")
	if err == nil {
		t.Fatal("resolveAppID: want error, got nil")
	}
	if !strings.Contains(err.Error(), `"com.unknown.app"`) {
		t.Errorf("error %q does not name the bundleId", err.Error())
	}
}

// TestCustomProductPagesWrites_RegisteredOnGroup confirms create / update /
// delete subcommands and their flags.
func TestCustomProductPagesWrites_RegisteredOnGroup(t *testing.T) {
	var cpp *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "custom-product-pages" {
			cpp = c
			break
		}
	}
	if cpp == nil {
		t.Fatal("custom-product-pages not on root")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range cpp.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"list", "get", "create", "update", "delete"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("custom-product-pages %s subcommand missing", want)
		}
	}
	if subs["create"].Flags().Lookup("name") == nil {
		t.Errorf("create missing --name flag")
	}
	for _, w := range []string{"name", "visible"} {
		if subs["update"].Flags().Lookup(w) == nil {
			t.Errorf("update missing --%s flag", w)
		}
	}
}

// TestBuildCustomProductPageCreate_Shape locks the JSON:API POST body
// shape: required (name, app), with no included block at L1.
func TestBuildCustomProductPageCreate_Shape(t *testing.T) {
	body := buildCustomProductPageCreate("APP-1", "Holiday Promo")
	raw, _ := json.Marshal(body)
	out := string(raw)
	for _, want := range []string{
		`"type":"appCustomProductPages"`,
		`"name":"Holiday Promo"`,
		`"app":{"data":{"id":"APP-1","type":"apps"}}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, `"included"`) {
		t.Errorf("body should not carry 'included' at L1: %s", out)
	}
}

// TestComputeCustomProductPagePatchAttrs_NoOpAndChange asserts that an
// unset flag never produces a patch entry; a same-value flag is filtered
// out; a new value lands in the patch.
func TestComputeCustomProductPagePatchAttrs_NoOpAndChange(t *testing.T) {
	yes := true
	cur := asc.AppCustomProductPageAttributes{
		Name:    "Holiday Promo",
		Visible: &yes,
	}

	// No flags set → empty patch.
	root := &cobra.Command{Use: "x"}
	root.Flags().StringVar(&customProductPagesUpdateName, "name", "", "")
	root.Flags().BoolVar(&customProductPagesUpdateVisible, "visible", false, "")
	patch := computeCustomProductPagePatchAttrs(root, cur)
	if len(patch) != 0 {
		t.Errorf("patch should be empty, got %v", patch)
	}

	// --name to same value → no name in patch.
	root2 := &cobra.Command{Use: "x"}
	root2.Flags().StringVar(&customProductPagesUpdateName, "name", "", "")
	root2.Flags().BoolVar(&customProductPagesUpdateVisible, "visible", false, "")
	if err := root2.ParseFlags([]string{"--name", "Holiday Promo"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	patch = computeCustomProductPagePatchAttrs(root2, cur)
	if _, ok := patch["name"]; ok {
		t.Errorf("name should not be in patch (matches): %v", patch)
	}

	// --name new + --visible to false → both entries.
	root3 := &cobra.Command{Use: "x"}
	root3.Flags().StringVar(&customProductPagesUpdateName, "name", "", "")
	root3.Flags().BoolVar(&customProductPagesUpdateVisible, "visible", false, "")
	if err := root3.ParseFlags([]string{"--name", "Spring 2026", "--visible=false"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	patch = computeCustomProductPagePatchAttrs(root3, cur)
	if patch["name"] != "Spring 2026" {
		t.Errorf("patch[name] = %v, want Spring 2026", patch["name"])
	}
	if patch["visible"] != false {
		t.Errorf("patch[visible] = %v, want false", patch["visible"])
	}
}

// TestFindCustomProductPageByName_FixtureReplay walks the read path used
// by the idempotent create.
func TestFindCustomProductPageByName_FixtureReplay(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/appCustomProductPages": {File: "custom_product_pages_list"},
	})
	c := fixtureASCClient(t, srv)
	got, err := findCustomProductPageByName(context.Background(), c, "1234567890", "Holiday Promo")
	if err != nil {
		t.Fatalf("findCustomProductPageByName: %v", err)
	}
	if got == nil || got.ID != "CPP-1" {
		t.Fatalf("got = %+v, want CPP-1", got)
	}
	miss, err := findCustomProductPageByName(context.Background(), c, "1234567890", "Nonexistent")
	if err != nil {
		t.Fatalf("findCustomProductPageByName miss: %v", err)
	}
	if miss != nil {
		t.Errorf("miss should be nil, got %+v", miss)
	}
}

// TestCustomProductPageSetResult_JSONShape locks the JSON contract for
// the create/update result.
func TestCustomProductPageSetResult_JSONShape(t *testing.T) {
	yes := true
	r := &CustomProductPageSetResult{
		PageID:  "CPP-1",
		Changed: true,
		Created: true,
		Attributes: asc.AppCustomProductPageAttributes{
			Name:    "Holiday Promo",
			Visible: &yes,
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v: %s", err, buf.String())
	}
	for _, key := range []string{"pageId", "changed", "created", "attributes"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing key %q. Got: %v", key, mapKeys(decoded))
		}
	}
}

// TestCustomProductPageDeleteResult_TableRows_NoChange covers the
// idempotent delete row.
func TestCustomProductPageDeleteResult_TableRows_NoChange(t *testing.T) {
	r := &CustomProductPageDeleteResult{
		PageID:  "CPP-1",
		Changed: false,
		Note:    "no change (idempotent) — page already absent",
	}
	_, rows := r.TableRows()
	foundNote := false
	for _, row := range rows {
		if row[0] == "NOTE" {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected NOTE row, rows=%v", rows)
	}
}
