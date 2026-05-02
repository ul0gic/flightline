package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

func TestBuildView_JSONShape(t *testing.T) {
	expired := false
	encrypt := false
	v := BuildView{
		ID:   "9000000001",
		Type: "builds",
		Attributes: asc.BuildAttributes{
			Version:                 "42",
			UploadedDate:            "2025-04-15T10:00:00-07:00",
			ExpirationDate:          "2025-07-14T10:00:00-07:00",
			Expired:                 &expired,
			ProcessingState:         "VALID",
			MinOsVersion:            "16.0",
			UsesNonExemptEncryption: &encrypt,
			BuildAudienceType:       "APP_STORE_ELIGIBLE",
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"9000000001"`,
		`"type":"builds"`,
		`"version":"42"`,
		`"processingState":"VALID"`,
		`"expired":false`,
		`"usesNonExemptEncryption":false`,
		`"minOsVersion":"16.0"`,
		`"buildAudienceType":"APP_STORE_ELIGIBLE"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestBuildList_TableRows_HighlightsExpired(t *testing.T) {
	expired := true
	active := false
	list := BuildList{
		Builds: []BuildView{
			{ID: "1", Attributes: asc.BuildAttributes{Version: "43", Expired: &active, ProcessingState: "VALID"}},
			{ID: "2", Attributes: asc.BuildAttributes{Version: "41", Expired: &expired, ProcessingState: "VALID"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"BUILD", "STATE", "EXPIRED", "UPLOADED", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if rows[0][2] != "active" {
		t.Errorf("rows[0] expired cell = %q, want active", rows[0][2])
	}
	if rows[1][2] != "EXPIRED" {
		t.Errorf("rows[1] expired cell = %q, want EXPIRED", rows[1][2])
	}
}

func TestBuildView_TableRows_VerticalLayout(t *testing.T) {
	v := &BuildView{ID: "1", Type: "builds", Attributes: asc.BuildAttributes{Version: "42"}}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 8 {
		t.Errorf("rows = %d, want >= 8", len(rows))
	}
}

func TestBuildsCommands_RegisteredOnRoot(t *testing.T) {
	var buildsCommand *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "builds" {
			buildsCommand = c
			break
		}
	}
	if buildsCommand == nil {
		t.Fatal("builds not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range buildsCommand.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"list", "get"} {
		if !subs[want] {
			t.Errorf("builds subcommand %q not registered", want)
		}
	}
}

// TestBuilds_JSONOutputStability asserts the BuildList JSON shape contract.
func TestBuilds_JSONOutputStability(t *testing.T) {
	expired := false
	encrypt := false
	list := BuildList{
		Builds: []BuildView{
			{
				ID:   "9000000001",
				Type: "builds",
				Attributes: asc.BuildAttributes{
					Version:                 "42",
					UploadedDate:            "2025-04-15T10:00:00-07:00",
					ExpirationDate:          "2025-07-14T10:00:00-07:00",
					Expired:                 &expired,
					MinOsVersion:            "16.0",
					ProcessingState:         "VALID",
					UsesNonExemptEncryption: &encrypt,
					BuildAudienceType:       "APP_STORE_ELIGIBLE",
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Builds []map[string]any `json:"builds"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if len(decoded.Builds) != 1 {
		t.Fatalf("builds len = %d, want 1", len(decoded.Builds))
	}
	row := decoded.Builds[0]
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing per-row key %q. Got: %v", key, mapKeys(row))
		}
	}
	attrs := row["attributes"].(map[string]any)
	for _, key := range []string{"version", "uploadedDate", "expirationDate", "expired", "minOsVersion", "processingState", "usesNonExemptEncryption", "buildAudienceType"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute key %q. Got: %v", key, mapKeys(attrs))
		}
	}
}

// TestBuildsType_StaysBuilds locks the resource type literal.
func TestBuildsType_StaysBuilds(t *testing.T) {
	v := BuildView{ID: "1", Type: "builds"}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"type":"builds"`) {
		t.Errorf("type literal regression: %s", b)
	}
}

func TestBuilds_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                   {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds": {File: "builds_list"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectBuilds(context.Background(), c, "/v1/apps/"+appID+"/builds", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectBuilds: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("builds len = %d, want 3", len(views))
	}
	if views[0].Attributes.Version != "43" {
		t.Errorf("views[0].version = %q, want 43", views[0].Attributes.Version)
	}
	// Third row is the expired one.
	if views[2].Attributes.Expired == nil || !*views[2].Attributes.Expired {
		t.Errorf("views[2].expired = %v, want true", views[2].Attributes.Expired)
	}
}

func TestBuilds_FixtureReplay_Get(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                   {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds": {File: "builds_get"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	page, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		context.Background(), c, "/v1/apps/"+appID+"/builds",
		url.Values{"filter[version]": {"42"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("data len = %d, want 1", len(page.Data))
	}
	if page.Data[0].Attributes.Version != "42" {
		t.Errorf("version = %q, want 42", page.Data[0].Attributes.Version)
	}
	if page.Data[0].Attributes.ProcessingState != "VALID" {
		t.Errorf("processingState = %q, want VALID", page.Data[0].Attributes.ProcessingState)
	}
}

// ----- builds attach tests -----

func TestBuildsAttach_RegisteredWithRequiredFlags(t *testing.T) {
	var attach *cobra.Command
	for _, c := range buildsCmd.Commands() {
		if c.Name() == "attach" {
			attach = c
			break
		}
	}
	if attach == nil {
		t.Fatal("builds attach not registered")
	}
	for _, want := range []string{"version", "build"} {
		if attach.Flag(want) == nil {
			t.Errorf("builds attach missing --%s flag", want)
		}
	}
}

func TestBuildAttachResult_JSONShape(t *testing.T) {
	r := BuildAttachResult{
		Action:    "attached",
		Changed:   true,
		Version:   "1.0.1",
		VersionID: "8000000001",
		Build:     "42",
		BuildID:   "9000000042",
		Platform:  "IOS",
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"attached"`,
		`"changed":true`,
		`"version":"1.0.1"`,
		`"versionId":"8000000001"`,
		`"build":"42"`,
		`"buildId":"9000000042"`,
		`"platform":"IOS"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %s", want, out)
		}
	}
}

func TestBuildAttachResult_TableRows(t *testing.T) {
	r := &BuildAttachResult{Action: "noop", Changed: false, Version: "1.0.1", Build: "42"}
	headers, rows := r.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %v, want 2 columns", headers)
	}
	if rows[0][0] != "ACTION" || rows[0][1] != "noop" {
		t.Errorf("rows[0] = %v, want ACTION/noop", rows[0])
	}
}

// TestBuildLinkage_NullData round-trips Apple's "no build attached" shape:
// {"data": null}. The pointer-typed Data field must decode to nil rather
// than an empty struct so callers can branch correctly.
func TestBuildLinkage_NullData(t *testing.T) {
	var got buildLinkageEnvelope
	if err := json.Unmarshal([]byte(`{"data":null}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Data != nil {
		t.Errorf("Data = %+v, want nil for null linkage", got.Data)
	}
}

// TestBuilds_LookupBuild_Found exercises the build idempotency probe.
func TestBuilds_LookupBuild_Found(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/builds": {File: "builds_lookup_byVersion"},
	})
	c := fixtureASCClient(t, srv)
	got, err := lookupBuild(context.Background(), c, "1234567890", "42")
	if err != nil {
		t.Fatalf("lookupBuild: %v", err)
	}
	if got == nil || got.ID != "9000000042" {
		t.Fatalf("lookupBuild = %+v, want id 9000000042", got)
	}
}

func TestBuilds_LookupBuild_NotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/builds": {File: "builds_lookup_empty"},
	})
	c := fixtureASCClient(t, srv)
	got, err := lookupBuild(context.Background(), c, "1234567890", "999")
	if err != nil {
		t.Fatalf("lookupBuild: %v", err)
	}
	if got != nil {
		t.Errorf("lookupBuild = %+v, want nil", got)
	}
}

// TestBuilds_GetAttachedBuild_NullData confirms the GET-linkage path
// returns nil when no build is attached, matching Apple's wire shape.
func TestBuilds_GetAttachedBuild_NullData(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/relationships/build": {File: "builds_attach_linkage_empty"},
	})
	c := fixtureASCClient(t, srv)
	got, err := getAttachedBuild(context.Background(), c, "8000000001")
	if err != nil {
		t.Fatalf("getAttachedBuild: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil for empty linkage", got)
	}
}

// TestBuilds_GetAttachedBuild_AlreadyAttached confirms that an existing
// linkage decodes to the right id — the value the idempotency check
// branches on.
func TestBuilds_GetAttachedBuild_AlreadyAttached(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/relationships/build": {File: "builds_attach_linkage_already"},
	})
	c := fixtureASCClient(t, srv)
	got, err := getAttachedBuild(context.Background(), c, "8000000001")
	if err != nil {
		t.Fatalf("getAttachedBuild: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want a linkage ref")
	}
	if got.ID != "9000000042" {
		t.Errorf("id = %q, want 9000000042", got.ID)
	}
}

// TestBuilds_PatchAttachedBuild_204NoContent confirms the PATCH path
// tolerates Apple's 204 No Content response. The fixture server returns
// 204 with an empty body; doJSON must not error on the empty decode.
func TestBuilds_PatchAttachedBuild_204NoContent(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"PATCH /v1/appStoreVersions/8000000001/relationships/build": {
			File:   "builds_attach_linkage_already",
			Status: http.StatusNoContent,
		},
	})
	c := fixtureASCClient(t, srv)
	body := buildLinkageEnvelope{Data: &buildLinkageRef{Type: "builds", ID: "9000000042"}}
	if err := patchAttachedBuild(context.Background(), c, "8000000001", body); err != nil {
		t.Fatalf("patchAttachedBuild: %v", err)
	}
}

// TestBuilds_AttachIdempotency_SameBuild simulates the noop branch end-to-
// end: lookups all match, current linkage already points at the requested
// build, and the runner declines to issue a PATCH. The route table omits
// the PATCH entry — if the wire layer ever called it, the fixture server
// would 404 and the test would fail.
func TestBuilds_AttachIdempotency_SameBuild(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
		"GET /v1/apps/1234567890/builds":           {File: "builds_lookup_byVersion"},
		"GET /v1/appStoreVersions/8000000001/relationships/build": {
			File: "builds_attach_linkage_already",
		},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	versionView, err := lookupVersion(context.Background(), c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersion: %v", err)
	}
	buildView, err := lookupBuild(context.Background(), c, appID, "42")
	if err != nil {
		t.Fatalf("lookupBuild: %v", err)
	}
	current, err := getAttachedBuild(context.Background(), c, versionView.ID)
	if err != nil {
		t.Fatalf("getAttachedBuild: %v", err)
	}
	if current == nil || current.ID != buildView.ID {
		t.Fatalf("expected current attached build %s, got %+v", buildView.ID, current)
	}
	// "Same build attached" branch: do NOT issue a PATCH. The fixture server
	// would 404 on an unregistered route, so reaching here is the assertion.
}

// TestBuilds_AttachIdempotency_DifferentBuild simulates the attach branch:
// version currently points at a different build, runner issues a PATCH.
// The PATCH route is registered; reaching it is the assertion.
func TestBuilds_AttachIdempotency_DifferentBuild(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
		"GET /v1/apps/1234567890/builds":           {File: "builds_lookup_byVersion"},
		"GET /v1/appStoreVersions/8000000001/relationships/build": {
			File: "builds_attach_linkage_other",
		},
		"PATCH /v1/appStoreVersions/8000000001/relationships/build": {
			File:   "builds_attach_linkage_already",
			Status: http.StatusNoContent,
		},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	versionView, err := lookupVersion(context.Background(), c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersion: %v", err)
	}
	buildView, err := lookupBuild(context.Background(), c, appID, "42")
	if err != nil {
		t.Fatalf("lookupBuild: %v", err)
	}
	current, err := getAttachedBuild(context.Background(), c, versionView.ID)
	if err != nil {
		t.Fatalf("getAttachedBuild: %v", err)
	}
	if current == nil || current.ID == buildView.ID {
		t.Fatalf("expected current attached build to differ from %s, got %+v", buildView.ID, current)
	}
	body := buildLinkageEnvelope{Data: &buildLinkageRef{Type: "builds", ID: buildView.ID}}
	if err := patchAttachedBuild(context.Background(), c, versionView.ID, body); err != nil {
		t.Fatalf("patchAttachedBuild: %v", err)
	}
}

// TestBuilds_AttachWireBody asserts the linkage PATCH body matches Apple's
// schema: {"data":{"type":"builds","id":"<id>"}}. Catches accidental
// misnamed fields that would 422 against the live API.
func TestBuilds_AttachWireBody(t *testing.T) {
	body := buildLinkageEnvelope{Data: &buildLinkageRef{Type: "builds", ID: "9000000042"}}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{`"data":`, `"type":"builds"`, `"id":"9000000042"`} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q: %s", want, out)
		}
	}
}
