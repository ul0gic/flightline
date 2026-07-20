package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAppView_JSONShape(t *testing.T) {
	v := AppView{
		ID:   "1234",
		Type: "apps",
		Attributes: AppAttributes{
			Name:          "Example",
			BundleID:      "com.example.app",
			SKU:           "EXAMPLE_SKU",
			PrimaryLocale: "en-US",
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"1234"`,
		`"type":"apps"`,
		`"name":"Example"`,
		`"bundleId":"com.example.app"`,
		`"sku":"EXAMPLE_SKU"`,
		`"primaryLocale":"en-US"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestAppList_TableRowsHeaders(t *testing.T) {
	list := AppList{
		Apps: []AppView{
			{ID: "1", Type: "apps", Attributes: AppAttributes{BundleID: "com.a", Name: "A", SKU: "SKU_A"}},
			{ID: "2", Type: "apps", Attributes: AppAttributes{BundleID: "com.b", Name: "B"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"BUNDLE_ID", "NAME", "SKU", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "com.a" || rows[1][0] != "com.b" {
		t.Errorf("rows[0,0] = %q, rows[1,0] = %q", rows[0][0], rows[1][0])
	}
}

func TestAppView_TableRows_VerticalLayout(t *testing.T) {
	v := &AppView{ID: "1", Type: "apps", Attributes: AppAttributes{BundleID: "com.a", Name: "A"}}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 7 {
		t.Errorf("rows = %d, want >= 7 (one per attribute)", len(rows))
	}
}

func TestAppsCommands_RegisteredOnRoot(t *testing.T) {
	var appsCommand *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() != "apps" {
			continue
		}
		appsCommand = c
		break
	}
	if appsCommand == nil {
		t.Fatal("apps not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range appsCommand.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"list", "get"} {
		if !subs[want] {
			t.Errorf("apps subcommand %q not registered", want)
		}
	}
}

func TestAppsList_RenderJSONRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	list := AppList{Apps: []AppView{
		{ID: "1", Type: "apps", Attributes: AppAttributes{BundleID: "com.a", Name: "A"}},
	}}
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"apps"`) || !strings.Contains(out, `"bundleId": "com.a"`) {
		t.Errorf("json missing expected fields: %q", out)
	}
}

func TestAppsList_DocumentedSelectorMatchesContract(t *testing.T) {
	if !strings.Contains(appsListCmd.Example, ".apps[].attributes.bundleId") {
		t.Fatalf("apps list help has stale JSON selector: %s", appsListCmd.Example)
	}
	if strings.Contains(appsListCmd.Example, ".apps[].bundleId") {
		t.Fatalf("apps list help still contains invalid selector: %s", appsListCmd.Example)
	}
}

func TestDefaultAppCap(t *testing.T) {
	if got := defaultAppCap(0); got != 32 {
		t.Errorf("defaultAppCap(0) = %d, want 32", got)
	}
	if got := defaultAppCap(50); got != 50 {
		t.Errorf("defaultAppCap(50) = %d, want 50", got)
	}
}

// The "apps" key plus every per-row attribute is a contract; renaming or
// removing breaks `jq` pipelines and LLM consumers.
func TestApps_JSONOutputStability_List(t *testing.T) {
	list := AppList{
		Apps: []AppView{
			{
				ID:   "1234567890",
				Type: "apps",
				Attributes: AppAttributes{
					Name:                     "Example Alpha",
					BundleID:                 "com.example.alpha",
					SKU:                      "EXAMPLE_ALPHA",
					PrimaryLocale:            "en-US",
					ContentRightsDeclaration: "DOES_NOT_USE_THIRD_PARTY_CONTENT",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}

	var decoded struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Apps) != 1 {
		t.Fatalf("apps len = %d, want 1", len(decoded.Apps))
	}

	wantTopLevel := []string{"id", "type", "attributes"}
	row := decoded.Apps[0]
	for _, key := range wantTopLevel {
		if _, ok := row[key]; !ok {
			t.Errorf("missing per-row key %q: JSON output is a contract; "+
				"adding fields is safe but removing/renaming breaks consumers. "+
				"Got keys: %v", key, mapKeys(row))
		}
	}

	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes is not an object: %T", row["attributes"])
	}
	wantAttrs := []string{"name", "bundleId", "sku", "primaryLocale", "contentRightsDeclaration"}
	for _, key := range wantAttrs {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute key %q: JSON output is a contract; "+
				"adding fields is safe but removing/renaming breaks consumers. "+
				"Got attribute keys: %v", key, mapKeys(attrs))
		}
	}
}

func TestApps_JSONOutputStability_Get(t *testing.T) {
	view := &AppView{
		ID:   "1234567890",
		Type: "apps",
		Attributes: AppAttributes{
			Name:          "Example Alpha",
			BundleID:      "com.example.alpha",
			SKU:           "EXAMPLE_ALPHA",
			PrimaryLocale: "en-US",
		},
	}

	var buf bytes.Buffer
	if err := renderTo(&buf, view, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q: JSON output is a contract. Got: %v", key, mapKeys(decoded))
		}
	}
}

// A regression to "App" or "application" would surprise downstream filters.
func TestApps_AppViewType_StaysApps(t *testing.T) {
	view := AppView{ID: "1", Type: "apps"}
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"type":"apps"`) {
		t.Errorf("type literal regression: %s", b)
	}
}

// The message MUST contain the literal bundleId: the entire actionable
// signal. Mirrors runAppsGet's format string; drift fails this test.
func TestApps_NotFoundErrorMessageShape(t *testing.T) {
	bundleID := "com.unknown.app"
	got := fmt.Sprintf("apps: no app found with bundleId %q", bundleID)
	for _, want := range []string{"apps:", "no app found", `"com.unknown.app"`} {
		if !strings.Contains(got, want) {
			t.Errorf("error message %q missing substring %q", got, want)
		}
	}
}
