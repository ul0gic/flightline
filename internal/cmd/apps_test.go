package cmd

import (
	"bytes"
	"encoding/json"
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

func TestDefaultAppCap(t *testing.T) {
	if got := defaultAppCap(0); got != 32 {
		t.Errorf("defaultAppCap(0) = %d, want 32", got)
	}
	if got := defaultAppCap(50); got != 50 {
		t.Errorf("defaultAppCap(50) = %d, want 50", got)
	}
}
