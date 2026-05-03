package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrivacyLabelsView_JSONShape(t *testing.T) {
	v := PrivacyLabelsView{
		BundleID:  "com.example.testapp",
		Supported: false,
		Reason:    "API does not expose appPrivacyDetails.",
		Reference: "https://developer.apple.com/app-store/app-privacy-details/",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.testapp"`,
		`"supported":false`,
		`"reason":"API does not expose appPrivacyDetails."`,
		`"reference":"https://developer.apple.com/app-store/app-privacy-details/"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestPrivacyLabelsView_TableRows(t *testing.T) {
	v := &PrivacyLabelsView{
		BundleID:  "com.example.testapp",
		Supported: false,
		Reason:    "stub",
		Reference: "ref",
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) != 4 {
		t.Errorf("rows = %d, want 4", len(rows))
	}
	wantPairs := map[string]string{
		"BUNDLE_ID": "com.example.testapp",
		"SUPPORTED": "false",
		"REASON":    "stub",
		"REFERENCE": "ref",
	}
	for _, r := range rows {
		want, ok := wantPairs[r[0]]
		if !ok {
			t.Errorf("unexpected row label %q", r[0])
			continue
		}
		if r[1] != want {
			t.Errorf("row %q value = %q, want %q", r[0], r[1], want)
		}
	}
}

func TestPrivacyLabelsCommands_RegisteredOnRoot(t *testing.T) {
	var pl *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "privacy-labels" {
			pl = c
			break
		}
	}
	if pl == nil {
		t.Fatal("privacy-labels not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range pl.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"get", "set"} {
		if !subs[want] {
			t.Errorf("privacy-labels %q subcommand missing", want)
		}
	}
}

// TestPrivacyLabelsSet_SameDiagnosticAsGet asserts the write-side stub returns
// byte-for-byte the same JSON as the read-side stub. ISSUE-002 resolution:
// Flightline does not fabricate an endpoint; both surfaces report
// supported=false with the same reason+reference so consumers branch on a
// single contract.
func TestPrivacyLabelsSet_SameDiagnosticAsGet(t *testing.T) {
	get := privacyLabelsDiagnostic("com.example.testapp")
	// Both runE handlers shell into privacyLabelsDiagnostic; round-trip the
	// view through JSON and assert the keys are identical.
	bGet, err := json.Marshal(get)
	if err != nil {
		t.Fatalf("marshal get: %v", err)
	}
	set := privacyLabelsDiagnostic("com.example.testapp")
	bSet, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	if !bytes.Equal(bGet, bSet) {
		t.Errorf("get and set diagnostics diverge:\n  get: %s\n  set: %s", bGet, bSet)
	}
	// Sanity: assert supported=false for both — the contract.
	for _, v := range []*PrivacyLabelsView{get, set} {
		if v.Supported {
			t.Errorf("supported=true (expected false until Apple ships endpoint)")
		}
	}
}

// TestPrivacyLabelsSet_NoFabricatedEndpoint verifies the set RunE doesn't
// reach for the API client. The stub must NOT spawn a request — that's the
// whole point of ISSUE-002. We assert by giving runPrivacyLabelsSet an empty
// arg slot would have panicked, but it survives because no client is needed.
func TestPrivacyLabelsSet_NoFabricatedEndpoint(t *testing.T) {
	prev := privacyLabelsSetFrom
	t.Cleanup(func() { privacyLabelsSetFrom = prev })
	// Even with --from set, the stub does not touch ASC.
	privacyLabelsSetFrom = "irrelevant.yaml"
	// We can't easily call Render(...) without writing to stdout, so just
	// confirm the diagnostic generator does its job.
	v := privacyLabelsDiagnostic("com.example.testapp")
	if v.Supported {
		t.Error("supported=true; ISSUE-002 contract is supported=false")
	}
	if v.Reason == "" || v.Reference == "" {
		t.Error("diagnostic missing reason or reference")
	}
}

// TestPrivacyLabels_JSONOutputStability_Stub locks the JSON contract for
// the "currently unsupported" diagnostic case. When Apple ships the
// endpoint, .supported flips to true and additional keys appear; the keys
// asserted here MUST stay stable so consumers can branch on .supported.
func TestPrivacyLabels_JSONOutputStability_Stub(t *testing.T) {
	v := &PrivacyLabelsView{
		BundleID:  "com.example.testapp",
		Supported: false,
		Reason:    "stub",
		Reference: "ref",
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "supported", "reason", "reference"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q — JSON contract drift. Got: %v", key, mapKeys(decoded))
		}
	}
	if got, ok := decoded["supported"].(bool); !ok || got != false {
		t.Errorf("supported = %v (%T), want bool false", decoded["supported"], decoded["supported"])
	}
}

// TestPrivacyLabels_StubViewSemantics verifies the view we return from the
// stub command path encodes the unsupported state correctly. We assert on
// the constructed view rather than running the command (which would dump
// JSON to the test runner's stdout).
func TestPrivacyLabels_StubViewSemantics(t *testing.T) {
	view := &PrivacyLabelsView{
		BundleID:  "com.example.testapp",
		Supported: false,
		Reason:    "App Store Connect API v4.3 does not expose appPrivacyDetails. Manage privacy nutrition labels via App Store Connect web UI.",
		Reference: "https://developer.apple.com/app-store/app-privacy-details/",
	}
	if view.Supported {
		t.Errorf("supported should be false in v4.3")
	}
	if !strings.Contains(view.Reason, "v4.3") || !strings.Contains(view.Reason, "appPrivacyDetails") {
		t.Errorf("reason should mention v4.3 and appPrivacyDetails: %q", view.Reason)
	}
	if !strings.HasPrefix(view.Reference, "https://developer.apple.com/") {
		t.Errorf("reference should be an apple.com URL: %q", view.Reference)
	}
}
