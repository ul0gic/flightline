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

func TestDiagnosticSignatureView_JSONShape(t *testing.T) {
	v := DiagnosticSignatureView{
		ID:   "DIAG-SIG-001",
		Type: "diagnosticSignatures",
		Attributes: asc.DiagnosticSignatureAttributes{
			DiagnosticType: "HANGS",
			Signature:      "Main thread blocked",
			Weight:         12.45,
			Insight: &asc.DiagnosticInsight{
				InsightType: "REGRESSION",
				Direction:   "WORSE",
				ReferenceVersions: []asc.DiagnosticReferenceVersion{
					{Version: "2.3.0", Value: 4.10},
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
		`"id":"DIAG-SIG-001"`,
		`"type":"diagnosticSignatures"`,
		`"diagnosticType":"HANGS"`,
		`"weight":12.45`,
		`"direction":"WORSE"`,
		`"version":"2.3.0"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestDiagnosticSignatureList_TableRowsHeaders(t *testing.T) {
	list := DiagnosticSignatureList{
		BundleID: "com.example.alpha",
		BuildID:  "BUILD-42",
		Signatures: []DiagnosticSignatureView{
			{ID: "S1", Attributes: asc.DiagnosticSignatureAttributes{DiagnosticType: "HANGS", Weight: 12.45, Signature: "x"}},
			{ID: "S2", Attributes: asc.DiagnosticSignatureAttributes{DiagnosticType: "DISK_WRITES", Weight: 3.2, Signature: "y"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"WEIGHT", "TYPE", "SIGNATURE", "INSIGHT", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "12.45" {
		t.Errorf("rows[0][0] (WEIGHT) = %q, want 12.45", rows[0][0])
	}
}

func TestFormatWeight(t *testing.T) {
	cases := map[float64]string{
		0:     "0",
		1.5:   "1.50",
		12.45: "12.45",
	}
	for in, want := range cases {
		if got := formatWeight(in); got != want {
			t.Errorf("formatWeight(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestInsightSummary(t *testing.T) {
	if got := insightSummary(nil); got != "" {
		t.Errorf("nil insight -> %q, want empty", got)
	}
	in := &asc.DiagnosticInsight{
		Direction: "WORSE",
		ReferenceVersions: []asc.DiagnosticReferenceVersion{
			{Version: "1.0"}, {Version: "1.1"},
		},
	}
	if got := insightSummary(in); got != "WORSE 2 refs" {
		t.Errorf("insightSummary = %q, want %q", got, "WORSE 2 refs")
	}
}

func TestDiagnosticsCommand_RegisteredOnRoot(t *testing.T) {
	var d *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "diagnostics" {
			d = c
			break
		}
	}
	if d == nil {
		t.Fatal("diagnostics not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range d.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"list", "get"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("diagnostics subcommand %q missing", want)
		}
	}
}

// TestDiagnostics_JSONOutputStability_List locks the list shape.
func TestDiagnostics_JSONOutputStability_List(t *testing.T) {
	list := DiagnosticSignatureList{
		BundleID:   "com.example.alpha",
		BuildID:    "BUILD-42",
		Signatures: []DiagnosticSignatureView{{ID: "S1", Type: "diagnosticSignatures", Attributes: asc.DiagnosticSignatureAttributes{Weight: 1.0}}},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "buildId", "signatures"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q — JSON contract drift", key)
		}
	}
}

// TestDiagnostics_FixtureReplay_List exercises the build lookup +
// diagnosticSignatures list path.
func TestDiagnostics_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                                 {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds":               {File: "testflight_build_lookup"},
		"GET /v1/builds/BUILD-42/diagnosticSignatures": {File: "diagnostics_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	_, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		ctx, c, "/v1/apps/1234567890/builds", url.Values{"filter[version]": {"42"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("build lookup: %v", err)
	}
	if len(bpage.Data) != 1 {
		t.Fatalf("build lookup data len = %d, want 1", len(bpage.Data))
	}

	views, err := collectDiagnosticSignatures(ctx, c, "/v1/builds/BUILD-42/diagnosticSignatures", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectDiagnosticSignatures: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("signatures len = %d, want 3", len(views))
	}
	if views[0].Attributes.Insight == nil || views[0].Attributes.Insight.Direction != "WORSE" {
		t.Errorf("views[0] insight not populated as expected")
	}
}

// TestDiagnostics_FixtureReplay_Get exercises the /logs endpoint.
func TestDiagnostics_FixtureReplay_Get(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/diagnosticSignatures/DIAG-SIG-001/logs": {File: "diagnostics_get"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	logs, err := asc.Get[asc.DiagnosticLogsResponse](
		ctx, c, "/v1/diagnosticSignatures/DIAG-SIG-001/logs", nil,
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if logs.Version != "1.0" {
		t.Errorf("version = %q, want 1.0", logs.Version)
	}
	if len(logs.ProductData) != 1 {
		t.Fatalf("productData len = %d, want 1", len(logs.ProductData))
	}
	pd := logs.ProductData[0]
	if pd.SignatureID != "DIAG-SIG-001" {
		t.Errorf("productData[0].signatureId = %q, want DIAG-SIG-001", pd.SignatureID)
	}
	if len(pd.DiagnosticLogs) != 1 {
		t.Fatalf("diagnosticLogs len = %d, want 1", len(pd.DiagnosticLogs))
	}
	if pd.DiagnosticLogs[0].DiagnosticMetaData.Event != "hang" {
		t.Errorf("metadata.event = %q, want hang", pd.DiagnosticLogs[0].DiagnosticMetaData.Event)
	}
}

// TestDiagnostics_BuildRequiredErrorMessage confirms --build absence is
// reported clearly to the user.
func TestDiagnostics_BuildRequiredErrorMessage(t *testing.T) {
	prev := diagnosticsListBuild
	t.Cleanup(func() { diagnosticsListBuild = prev })

	diagnosticsListBuild = ""
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := runDiagnosticsList(cmd, []string{"com.example.alpha"})
	if err == nil {
		t.Fatal("expected error when --build is empty")
	}
	if !strings.Contains(err.Error(), "--build") {
		t.Errorf("error %q does not mention --build", err.Error())
	}
}
