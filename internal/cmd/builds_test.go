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
