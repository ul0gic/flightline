package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWhoamiInfo_TableRows(t *testing.T) {
	info := WhoamiInfo{
		KeyID:        "TESTABCDEF",
		IssuerID:     "iss-uuid",
		VendorNumber: "12345678",
		Authorized:   true,
		APIBaseURL:   "https://api.appstoreconnect.apple.com",
	}
	headers, rows := info.TableRows()
	if len(headers) != 2 {
		t.Fatalf("headers = %d, want 2", len(headers))
	}
	if len(rows) != 5 {
		t.Errorf("rows = %d, want 5", len(rows))
	}
	// Authorized cell should serialize as "true".
	for _, r := range rows {
		if r[0] == "AUTHORIZED" && r[1] != "true" {
			t.Errorf("AUTHORIZED row = %q, want \"true\"", r[1])
		}
	}
}

func TestWhoamiInfo_JSONFieldStability(t *testing.T) {
	info := WhoamiInfo{
		KeyID:      "K",
		IssuerID:   "I",
		Authorized: true,
		APIBaseURL: "https://api.appstoreconnect.apple.com",
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{`"keyId":"K"`, `"issuerId":"I"`, `"authorized":true`, `"apiBaseUrl":"https://api.appstoreconnect.apple.com"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q in %q", want, out)
		}
	}
	// vendorNumber omitted when empty (omitempty).
	if strings.Contains(out, `"vendorNumber"`) {
		t.Errorf("vendorNumber leaked when empty: %q", out)
	}
}

func TestRenderWhoami_JSONRoundtrip(t *testing.T) {
	// Capture stdout via a sink writer; renderWhoami writes via Render which
	// targets os.Stdout. To keep the test hermetic we exercise Render through
	// a TableRenderable+JSON path on a buffer.
	var buf bytes.Buffer
	info := WhoamiInfo{KeyID: "K", IssuerID: "I", Authorized: true}
	if err := renderTo(&buf, info, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	if !strings.Contains(buf.String(), `"keyId": "K"`) {
		t.Errorf("json output missing keyId: %q", buf.String())
	}
}

func TestWhoamiCommand_RegisteredOnRoot(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "whoami" {
			found = true
			break
		}
	}
	if !found {
		t.Error("whoami not registered on rootCmd; init() AddCommand failed")
	}
}
