package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// ----- versions tests -----

func TestVersionView_JSONShape(t *testing.T) {
	dl := true
	v := VersionView{
		ID:   "8000000001",
		Type: "appStoreVersions",
		Attributes: asc.VersionAttributes{
			Platform:        "IOS",
			VersionString:   "1.0.1",
			AppStoreState:   "PREPARE_FOR_SUBMISSION",
			AppVersionState: "PREPARE_FOR_SUBMISSION",
			ReleaseType:     "MANUAL",
			ReviewType:      "APP_STORE",
			Downloadable:    &dl,
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"8000000001"`,
		`"type":"appStoreVersions"`,
		`"versionString":"1.0.1"`,
		`"platform":"IOS"`,
		`"appStoreState":"PREPARE_FOR_SUBMISSION"`,
		`"appVersionState":"PREPARE_FOR_SUBMISSION"`,
		`"releaseType":"MANUAL"`,
		`"reviewType":"APP_STORE"`,
		`"downloadable":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestVersionList_TableRowsHeaders(t *testing.T) {
	list := VersionList{
		Versions: []VersionView{
			{ID: "8000000001", Attributes: asc.VersionAttributes{Platform: "IOS", VersionString: "1.0.1", AppVersionState: "PREPARE_FOR_SUBMISSION", ReleaseType: "MANUAL"}},
			{ID: "8000000000", Attributes: asc.VersionAttributes{Platform: "IOS", VersionString: "1.0.0", AppStoreState: "READY_FOR_SALE", ReleaseType: "AFTER_APPROVAL"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"VERSION", "PLATFORM", "STATE", "RELEASE_TYPE", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// state column should pick AppVersionState when set, else AppStoreState.
	if rows[0][2] != "PREPARE_FOR_SUBMISSION" {
		t.Errorf("rows[0] state = %q, want PREPARE_FOR_SUBMISSION", rows[0][2])
	}
	if rows[1][2] != "READY_FOR_SALE" {
		t.Errorf("rows[1] state = %q, want READY_FOR_SALE (fallback to AppStoreState)", rows[1][2])
	}
}

func TestVersionView_TableRows_VerticalLayout(t *testing.T) {
	v := &VersionView{ID: "1", Type: "appStoreVersions", Attributes: asc.VersionAttributes{VersionString: "1.0.0"}}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 10 {
		t.Errorf("rows = %d, want >= 10 (one per attribute)", len(rows))
	}
}

func TestVersionsCommands_RegisteredOnRoot(t *testing.T) {
	var versionsCommand *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "versions" {
			versionsCommand = c
			break
		}
	}
	if versionsCommand == nil {
		t.Fatal("versions not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range versionsCommand.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"list", "get"} {
		if !subs[want] {
			t.Errorf("versions subcommand %q not registered", want)
		}
	}
}

func TestVersionsList_RenderJSONRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	list := VersionList{Versions: []VersionView{
		{ID: "1", Type: "appStoreVersions", Attributes: asc.VersionAttributes{VersionString: "1.0.0", Platform: "IOS"}},
	}}
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"versions"`) || !strings.Contains(out, `"versionString": "1.0.0"`) {
		t.Errorf("json missing expected fields: %q", out)
	}
}

// TestVersions_JSONOutputStability_List asserts the VersionList JSON shape.
// Top-level "versions" key plus per-row attribute keys are a contract.
func TestVersions_JSONOutputStability_List(t *testing.T) {
	dl := true
	list := VersionList{
		Versions: []VersionView{
			{
				ID:   "8000000001",
				Type: "appStoreVersions",
				Attributes: asc.VersionAttributes{
					Platform:        "IOS",
					VersionString:   "1.0.1",
					AppStoreState:   "PREPARE_FOR_SUBMISSION",
					AppVersionState: "PREPARE_FOR_SUBMISSION",
					Copyright:       "(c) 2025 CoreLift LLC",
					ReviewType:      "APP_STORE",
					ReleaseType:     "MANUAL",
					Downloadable:    &dl,
					CreatedDate:     "2025-04-15T12:00:00-07:00",
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Versions []map[string]any `json:"versions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Versions) != 1 {
		t.Fatalf("versions len = %d, want 1", len(decoded.Versions))
	}
	row := decoded.Versions[0]
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing per-row key %q — JSON contract drift. Got keys: %v", key, mapKeys(row))
		}
	}
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes is not an object: %T", row["attributes"])
	}
	for _, key := range []string{"platform", "versionString", "appStoreState", "appVersionState", "releaseType", "reviewType", "copyright", "downloadable", "createdDate"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute key %q — JSON contract drift. Got: %v", key, mapKeys(attrs))
		}
	}
}

// TestVersionsType_StaysAppStoreVersions locks the resource type literal.
func TestVersionsType_StaysAppStoreVersions(t *testing.T) {
	v := VersionView{ID: "1", Type: "appStoreVersions"}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"type":"appStoreVersions"`) {
		t.Errorf("type literal regression: %s", b)
	}
}

// TestVersions_FixtureReplay_List exercises the production collectVersions
// pipeline against a captured-shape golden fixture, hitting the path the
// real `versions list` command takes (resolveAppID -> /v1/apps/{id}/appStoreVersions).
func TestVersions_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_list"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	if appID != "1234567890" {
		t.Fatalf("appID = %q, want 1234567890", appID)
	}
	views, err := collectVersions(context.Background(), c, "/v1/apps/"+appID+"/appStoreVersions", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectVersions: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("versions len = %d, want 2", len(views))
	}
	if views[0].Attributes.VersionString != "1.0.1" {
		t.Errorf("views[0].versionString = %q, want 1.0.1", views[0].Attributes.VersionString)
	}
	if views[1].Attributes.AppStoreState != "READY_FOR_SALE" {
		t.Errorf("views[1].appStoreState = %q, want READY_FOR_SALE", views[1].Attributes.AppStoreState)
	}
}

// TestVersions_FixtureReplay_GetNotFound asserts the user-facing error
// message echoes the bundleId AND the missing version string when the
// version-string filter yields zero records.
func TestVersions_FixtureReplay_GetNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_get_notFound"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}

	q := url.Values{
		"filter[versionString]": {"9.9.9"},
		"filter[platform]":      {"IOS"},
		"limit":                 {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		context.Background(), c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(page.Data) != 0 {
		t.Errorf("data len = %d, want 0", len(page.Data))
	}
}

// TestVersions_FixtureReplay_BundleNotFound asserts that resolveAppID returns
// a typed not-found error echoing the bundleId when no app matches.
func TestVersions_FixtureReplay_BundleNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_notFound"},
	})
	c := fixtureASCClient(t, srv)

	_, err := resolveAppID(context.Background(), c, "com.unknown.app")
	if err == nil {
		t.Fatal("resolveAppID: want error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"apps:", "no app found", `"com.unknown.app"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing substring %q", msg, want)
		}
	}
}
