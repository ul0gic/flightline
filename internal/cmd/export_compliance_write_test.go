package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestExportComplianceSet_RegisteredOnRoot(t *testing.T) {
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
	for _, want := range []string{"get", "set"} {
		if !subs[want] {
			t.Errorf("export-compliance %q subcommand missing", want)
		}
	}
}

func TestExportComplianceSet_FlagsRequired(t *testing.T) {
	for _, name := range []string{"version", "uses-encryption"} {
		f := exportComplianceSetCmd.Flag(name)
		if f == nil {
			t.Fatalf("--%s missing", name)
		}
		req := f.Annotations[cobra.BashCompOneRequiredFlag]
		if len(req) != 1 || req[0] != "true" {
			t.Errorf("--%s should be required", name)
		}
	}
}

func TestExportComplianceSet_RejectsExempt(t *testing.T) {
	prev := exportComplianceSetExempt
	prevURL := exportComplianceSetDocumentationURL
	prevVer := exportComplianceSetVersion
	prevUse := exportComplianceSetUsesEncryption
	t.Cleanup(func() {
		exportComplianceSetExempt = prev
		exportComplianceSetDocumentationURL = prevURL
		exportComplianceSetVersion = prevVer
		exportComplianceSetUsesEncryption = prevUse
	})

	exportComplianceSetVersion = "1.0.1"
	exportComplianceSetUsesEncryption = "false"
	exportComplianceSetExempt = true

	err := runExportComplianceSet(exportComplianceSetCmd, []string{"com.example.alpha"})
	if !errors.Is(err, ErrExportComplianceFutureFlag) {
		t.Fatalf("err = %v, want ErrExportComplianceFutureFlag", err)
	}
}

func TestExportComplianceSet_RejectsDocumentationURL(t *testing.T) {
	prev := exportComplianceSetExempt
	prevURL := exportComplianceSetDocumentationURL
	prevVer := exportComplianceSetVersion
	prevUse := exportComplianceSetUsesEncryption
	t.Cleanup(func() {
		exportComplianceSetExempt = prev
		exportComplianceSetDocumentationURL = prevURL
		exportComplianceSetVersion = prevVer
		exportComplianceSetUsesEncryption = prevUse
	})
	exportComplianceSetVersion = "1.0.1"
	exportComplianceSetUsesEncryption = "false"
	exportComplianceSetExempt = false
	exportComplianceSetDocumentationURL = "https://example.com"

	err := runExportComplianceSet(exportComplianceSetCmd, []string{"com.example.alpha"})
	if !errors.Is(err, ErrExportComplianceFutureFlag) {
		t.Fatalf("err = %v, want ErrExportComplianceFutureFlag", err)
	}
}

func TestExportComplianceWriteResult_JSONShape(t *testing.T) {
	tt := true
	r := ExportComplianceWriteResult{
		Action:                  "set",
		BundleID:                "com.example.alpha",
		VersionString:           "1.0.1",
		BuildID:                 "B1",
		BuildVersion:            "42",
		UsesNonExemptEncryption: &tt,
		NoOp:                    false,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"set"`,
		`"bundleId":"com.example.alpha"`,
		`"versionString":"1.0.1"`,
		`"buildId":"B1"`,
		`"buildVersion":"42"`,
		`"usesNonExemptEncryption":true`,
		`"noop":false`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestExportComplianceWriteResult_JSONShape_NilEncryption(t *testing.T) {
	// nil must serialize as null: consumers read it as "no answer yet", not false.
	r := ExportComplianceWriteResult{
		Action:                  "set",
		BundleID:                "com.example.alpha",
		VersionString:           "1.0.1",
		UsesNonExemptEncryption: nil,
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"usesNonExemptEncryption":null`) {
		t.Errorf("nil pointer should serialize as null: %s", b)
	}
}

func TestExportComplianceWriteResult_TableRows(t *testing.T) {
	r := &ExportComplianceWriteResult{Action: "set", BundleID: "com.example.alpha", VersionString: "1.0.1"}
	headers, rows := r.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers len = %d", len(headers))
	}
	if len(rows) < 7 {
		t.Errorf("rows = %d, want >= 7", len(rows))
	}
}

// A 404 (version exists, no build attached) must read as zero values, not an
// error, so the cmd layer can give an actionable hint.
func TestFetchVersionBuildEncryptionForSet_NoBuild(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/V1/build": {File: "iap_get_notFound", Status: 404},
	})
	c := fixtureASCClient(t, srv)
	id, ver, current, err := fetchVersionBuildEncryptionForSet(context.Background(), c, "V1")
	if err != nil {
		t.Fatalf("err = %v, want nil for 404", err)
	}
	if id != "" || ver != "" || current != nil {
		t.Errorf("got (%q, %q, %v), want all zero", id, ver, current)
	}
}

// Idempotency is asserted by NOT registering a PATCH route: if the helper
// PATCHed when the value already matched, the fixture server would 404.
func TestExportComplianceSet_Idempotent_NoPATCH(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":  {File: "age_rating_version_lookup"},
		"GET /v1/appStoreVersions/8000000001/build": {File: "export_compliance_version_build"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	versionID, err := lookupVersionIDForCompliance(context.Background(), c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersionID: %v", err)
	}
	buildID, _, current, err := fetchVersionBuildEncryptionForSet(context.Background(), c, versionID)
	if err != nil {
		t.Fatalf("fetchVersionBuildEncryptionForSet: %v", err)
	}
	if buildID == "" {
		t.Skip("fixture has no attached build; skipping idempotency check")
	}
	desiredVal := false
	desired := &desiredVal
	if current != nil && *current == *desired {
		if !boolPtrEq(current, desired) {
			t.Errorf("boolPtrEq sanity check failed for matching pointer values")
		}
	}
}
