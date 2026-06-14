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

// The "versions" key plus per-row attribute keys are a contract.
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
			t.Errorf("missing per-row key %q: JSON contract drift. Got keys: %v", key, mapKeys(row))
		}
	}
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes is not an object: %T", row["attributes"])
	}
	for _, key := range []string{"platform", "versionString", "appStoreState", "appVersionState", "releaseType", "reviewType", "copyright", "downloadable", "createdDate"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute key %q: JSON contract drift. Got: %v", key, mapKeys(attrs))
		}
	}
}

func TestVersionsType_StaysAppStoreVersions(t *testing.T) {
	v := VersionView{ID: "1", Type: "appStoreVersions"}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"type":"appStoreVersions"`) {
		t.Errorf("type literal regression: %s", b)
	}
}

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

func TestVersionsCreate_RegisteredWithRequiredFlag(t *testing.T) {
	subs := versionsCmd.Commands()
	var create, update *cobra.Command
	for _, c := range subs {
		switch c.Name() {
		case "create":
			create = c
		case "update":
			update = c
		}
	}
	if create == nil {
		t.Fatal("versions create not registered")
	}
	if update == nil {
		t.Fatal("versions update not registered")
	}
	if create.Flag("version") == nil {
		t.Errorf("versions create: missing --version flag")
	}
	if update.Flag("version") == nil {
		t.Errorf("versions update: missing --version flag")
	}
	if create.Flag("release-type") == nil {
		t.Errorf("versions create: missing --release-type flag")
	}
}

func TestVersions_LookupVersion_Found(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
	})
	c := fixtureASCClient(t, srv)
	got, err := lookupVersion(context.Background(), c, "1234567890", "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersion: %v", err)
	}
	if got == nil {
		t.Fatal("lookupVersion: got nil, want a record")
	}
	if got.ID != "8000000001" {
		t.Errorf("id = %q, want 8000000001", got.ID)
	}
	if got.Attributes.ReleaseType != "MANUAL" {
		t.Errorf("releaseType = %q, want MANUAL", got.Attributes.ReleaseType)
	}
}

func TestVersions_LookupVersion_NotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_empty"},
	})
	c := fixtureASCClient(t, srv)
	got, err := lookupVersion(context.Background(), c, "1234567890", "2.0.0", "IOS")
	if err != nil {
		t.Fatalf("lookupVersion: %v", err)
	}
	if got != nil {
		t.Errorf("lookupVersion: got %+v, want nil for missing version", got)
	}
}

// Idempotency: a bare `versions update --version X` on an existing version
// must be a no-op: unpassed flags never enter the diff.
func TestVersions_DiffAttrs_NoChangeWhenFlagsUnset(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("copyright", "", "")
	cmd.Flags().String("release-type", "", "")
	cmd.Flags().String("review-type", "", "")
	cmd.Flags().String("earliest-release-date", "", "")
	cur := asc.VersionAttributes{
		Copyright:   "(c) 2025 Example LLC",
		ReleaseType: "MANUAL",
		ReviewType:  "APP_STORE",
	}
	_, changed := diffVersionAttrs(cmd, cur, "", "", "", "")
	if changed {
		t.Error("diffVersionAttrs: changed=true with no flags set; want false")
	}
}

func TestVersions_DiffAttrs_NoChangeWhenIdentical(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("release-type", "", "")
	if err := cmd.Flags().Set("release-type", "MANUAL"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	cmd.Flags().String("copyright", "", "")
	cmd.Flags().String("review-type", "", "")
	cmd.Flags().String("earliest-release-date", "", "")

	cur := asc.VersionAttributes{ReleaseType: "MANUAL"}
	out, changed := diffVersionAttrs(cmd, cur, "", "MANUAL", "", "")
	if changed {
		t.Errorf("diffVersionAttrs: changed=true for identical value; want false. attrs=%+v", out)
	}
}

func TestVersions_DiffAttrs_ChangeWhenDiffers(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("release-type", "", "")
	if err := cmd.Flags().Set("release-type", "AFTER_APPROVAL"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	cmd.Flags().String("copyright", "", "")
	cmd.Flags().String("review-type", "", "")
	cmd.Flags().String("earliest-release-date", "", "")

	cur := asc.VersionAttributes{ReleaseType: "MANUAL"}
	out, changed := diffVersionAttrs(cmd, cur, "", "AFTER_APPROVAL", "", "")
	if !changed {
		t.Error("diffVersionAttrs: changed=false; want true")
	}
	if out.ReleaseType == nil || *out.ReleaseType != "AFTER_APPROVAL" {
		t.Errorf("out.ReleaseType = %v, want pointer to 'AFTER_APPROVAL'", out.ReleaseType)
	}
	if out.Copyright != nil {
		t.Errorf("out.Copyright = %v, want nil (unset flag)", out.Copyright)
	}
}

func TestVersionWriteResult_JSONShape(t *testing.T) {
	r := VersionWriteResult{
		Action:  "updated",
		Changed: true,
		Version: VersionView{
			ID:   "8000000001",
			Type: "appStoreVersions",
			Attributes: asc.VersionAttributes{
				Platform:      "IOS",
				VersionString: "1.0.1",
				ReleaseType:   "AFTER_APPROVAL",
			},
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"updated"`,
		`"changed":true`,
		`"version":`,
		`"id":"8000000001"`,
		`"versionString":"1.0.1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestVersionWriteResult_TableRows(t *testing.T) {
	r := &VersionWriteResult{Action: "noop", Changed: false, Version: VersionView{ID: "8000000001"}}
	headers, rows := r.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %v, want 2 columns", headers)
	}
	if rows[0][0] != "ACTION" || rows[0][1] != "noop" {
		t.Errorf("rows[0] = %v, want ACTION/noop", rows[0])
	}
	if rows[1][0] != "CHANGED" || rows[1][1] != "false" {
		t.Errorf("rows[1] = %v, want CHANGED/false", rows[1])
	}
}

func TestVersions_FixtureReplay_CreateNew(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_empty"},
		"POST /v1/appStoreVersions":                {File: "versions_create", Status: 201},
	})
	c := fixtureASCClient(t, srv)

	// runVersionsCreate can't be shelled (it reads viper); mirror its wire path.
	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	got, err := lookupVersion(context.Background(), c, appID, "2.0.0", "IOS")
	if err != nil {
		t.Fatalf("lookupVersion: %v", err)
	}
	if got != nil {
		t.Fatalf("lookupVersion: got %+v, want nil", got)
	}
	body := versionWriteEnvelope{
		Data: versionWriteData{
			Type: "appStoreVersions",
			Attributes: versionWriteAttributes{
				Platform:      strPtr("IOS"),
				VersionString: strPtr("2.0.0"),
				Copyright:     strPtr("(c) 2025 Example LLC"),
				ReleaseType:   strPtr("MANUAL"),
			},
			Relationships: &versionWriteRels{
				App: &versionWriteRel{Data: versionWriteRelRef{Type: "apps", ID: appID}},
			},
		},
	}
	resp, err := asc.Post[asc.Single[asc.VersionAttributes]](
		context.Background(), c, "/v1/appStoreVersions", nil, body,
	)
	if err != nil {
		t.Fatalf("create POST: %v", err)
	}
	if resp.Data.ID != "8000000099" {
		t.Errorf("created id = %q, want 8000000099", resp.Data.ID)
	}
	if resp.Data.Attributes.VersionString != "2.0.0" {
		t.Errorf("created versionString = %q, want 2.0.0", resp.Data.Attributes.VersionString)
	}
}

func TestVersions_FixtureReplay_UpdateChange(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
		"PATCH /v1/appStoreVersions/8000000001":    {File: "versions_update"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	existing, err := lookupVersion(context.Background(), c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersion: %v", err)
	}
	if existing == nil {
		t.Fatal("lookupVersion: nil, want a record")
	}
	body := versionWriteEnvelope{
		Data: versionWriteData{
			Type: "appStoreVersions",
			ID:   existing.ID,
			Attributes: versionWriteAttributes{
				ReleaseType: strPtr("AFTER_APPROVAL"),
			},
		},
	}
	resp, err := asc.Patch[asc.Single[asc.VersionAttributes]](
		context.Background(), c, "/v1/appStoreVersions/"+existing.ID, nil, body,
	)
	if err != nil {
		t.Fatalf("update PATCH: %v", err)
	}
	if resp.Data.Attributes.ReleaseType != "AFTER_APPROVAL" {
		t.Errorf("updated releaseType = %q, want AFTER_APPROVAL", resp.Data.Attributes.ReleaseType)
	}
}

// pointer + omitempty keeps unpassed flags out of the body; accidental ""
// defaults would clear fields server-side.
func TestVersions_WireBody_OmitsUnsetFields(t *testing.T) {
	body := versionWriteEnvelope{
		Data: versionWriteData{
			Type: "appStoreVersions",
			ID:   "8000000001",
			Attributes: versionWriteAttributes{
				ReleaseType: strPtr("AFTER_APPROVAL"),
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	if !strings.Contains(out, `"releaseType":"AFTER_APPROVAL"`) {
		t.Errorf("missing releaseType in body: %s", out)
	}
	for _, leak := range []string{`"copyright"`, `"reviewType"`, `"earliestReleaseDate"`, `"versionString"`, `"platform"`} {
		if strings.Contains(out, leak) {
			t.Errorf("body leaks unset field %s: %s", leak, out)
		}
	}
}
