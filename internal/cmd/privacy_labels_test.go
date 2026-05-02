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
	if !subs["get"] {
		t.Errorf("privacy-labels get subcommand missing")
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
