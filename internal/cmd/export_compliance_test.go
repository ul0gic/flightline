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

func TestExportComplianceView_JSONShape(t *testing.T) {
	uses := false
	exempt := true
	v := ExportComplianceView{
		BundleID:      "com.example.testapp",
		VersionString: "1.0.1",
		Build: asc.BuildEncryptionView{
			BuildID:                 "9000000042",
			BuildVersion:            "42",
			UsesNonExemptEncryption: &uses,
		},
		Declarations: []EncryptionDeclarationView{
			{
				ID:   "BBBB0000-0000-0000-0000-000000000001",
				Type: "appEncryptionDeclarations",
				Attributes: asc.AppEncryptionDeclarationAttributes{
					Exempt:                        &exempt,
					Platform:                      "IOS",
					AppEncryptionDeclarationState: "APPROVED",
					CodeValue:                     "5D002",
				},
			},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.testapp"`,
		`"versionString":"1.0.1"`,
		`"buildId":"9000000042"`,
		`"buildVersion":"42"`,
		`"usesNonExemptEncryption":false`,
		`"appEncryptionDeclarationState":"APPROVED"`,
		`"codeValue":"5D002"`,
		`"exempt":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestExportComplianceView_TableRows_DeclarationsExpand(t *testing.T) {
	uses := false
	exempt := true
	v := &ExportComplianceView{
		BundleID:      "com.example.testapp",
		VersionString: "1.0.1",
		Build: asc.BuildEncryptionView{
			BuildID:                 "9000000042",
			BuildVersion:            "42",
			UsesNonExemptEncryption: &uses,
		},
		Declarations: []EncryptionDeclarationView{
			{
				ID:   "BBBB0000-0000-0000-0000-000000000001",
				Type: "appEncryptionDeclarations",
				Attributes: asc.AppEncryptionDeclarationAttributes{
					Exempt:                        &exempt,
					Platform:                      "IOS",
					AppEncryptionDeclarationState: "APPROVED",
					CodeValue:                     "5D002",
				},
			},
		},
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 10 {
		t.Errorf("rows = %d, want >= 10 (build summary + per-declaration block)", len(rows))
	}
	foundUses := false
	for _, r := range rows {
		if r[0] == "USES_NON_EXEMPT_ENCRYPTION" && r[1] == "false" {
			foundUses = true
			break
		}
	}
	if !foundUses {
		t.Errorf("expected USES_NON_EXEMPT_ENCRYPTION row with value 'false'")
	}
}

func TestExportComplianceView_TableRows_UnansweredBuild(t *testing.T) {
	v := &ExportComplianceView{
		BundleID:      "com.example.testapp",
		VersionString: "1.0.1",
		Build:         asc.BuildEncryptionView{},
	}
	_, rows := v.TableRows()
	foundUnanswered := false
	for _, r := range rows {
		if r[0] == "USES_NON_EXEMPT_ENCRYPTION" && r[1] == "(unanswered)" {
			foundUnanswered = true
			break
		}
	}
	if !foundUnanswered {
		t.Errorf("expected USES_NON_EXEMPT_ENCRYPTION = (unanswered) row when nil")
	}
}

func TestExportComplianceCommands_RegisteredOnRoot(t *testing.T) {
	var ec *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "export-compliance" {
			ec = c
			break
		}
	}
	if ec == nil {
		t.Fatal("export-compliance not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range ec.Commands() {
		subs[sc.Name()] = true
	}
	if !subs["get"] {
		t.Errorf("export-compliance get subcommand missing")
	}
}

func TestExportCompliance_JSONOutputStability(t *testing.T) {
	uses := false
	v := &ExportComplianceView{
		BundleID:      "com.example.testapp",
		VersionString: "1.0.1",
		Build: asc.BuildEncryptionView{
			BuildID:                 "9000000042",
			BuildVersion:            "42",
			UsesNonExemptEncryption: &uses,
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "versionString", "build"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q: JSON contract drift. Got: %v", key, mapKeys(decoded))
		}
	}
	build, ok := decoded["build"].(map[string]any)
	if !ok {
		t.Fatalf("build is not an object: %T", decoded["build"])
	}
	for _, key := range []string{"buildId", "buildVersion", "usesNonExemptEncryption"} {
		if _, ok := build[key]; !ok {
			t.Errorf("missing build key %q: JSON contract drift. Got: %v", key, mapKeys(build))
		}
	}
}

// TestExportCompliance_FixtureReplay_Happy exercises the full chain:
// resolveAppID → version filter → version's build → app's declarations.
func TestExportCompliance_FixtureReplay_Happy(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":          {File: "export_compliance_version"},
		"GET /v1/appStoreVersions/8000000001/build":         {File: "export_compliance_version_build"},
		"GET /v1/apps/1234567890/appEncryptionDeclarations": {File: "export_compliance_declaration"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	build, err := fetchVersionBuildEncryption(ctx, c, "8000000001")
	if err != nil {
		t.Fatalf("fetchVersionBuildEncryption: %v", err)
	}
	if build.BuildID != "9000000042" {
		t.Errorf("build.id = %q, want 9000000042", build.BuildID)
	}
	if build.UsesNonExemptEncryption == nil || *build.UsesNonExemptEncryption != false {
		t.Errorf("usesNonExemptEncryption should be explicit false, got %v", build.UsesNonExemptEncryption)
	}

	decls, err := collectAppEncryptionDeclarations(ctx, c, appID)
	if err != nil {
		t.Fatalf("collectAppEncryptionDeclarations: %v", err)
	}
	if len(decls) != 1 {
		t.Fatalf("declarations len = %d, want 1", len(decls))
	}
	if decls[0].Attributes.AppEncryptionDeclarationState != "APPROVED" {
		t.Errorf("state = %q, want APPROVED", decls[0].Attributes.AppEncryptionDeclarationState)
	}
	if decls[0].Attributes.CodeValue != "5D002" {
		t.Errorf("codeValue = %q, want 5D002", decls[0].Attributes.CodeValue)
	}
}

// TestExportCompliance_FixtureReplay_NoDeclarations verifies the common case
// where an app has only the per-build boolean answer (no full declaration).
func TestExportCompliance_FixtureReplay_NoDeclarations(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appEncryptionDeclarations": {File: "export_compliance_no_declarations"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	decls, err := collectAppEncryptionDeclarations(ctx, c, "1234567890")
	if err != nil {
		t.Fatalf("collectAppEncryptionDeclarations: %v", err)
	}
	if len(decls) != 0 {
		t.Errorf("declarations len = %d, want 0 (most apps have none)", len(decls))
	}
}
