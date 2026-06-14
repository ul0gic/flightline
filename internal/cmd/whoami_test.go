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
	// Exercise Render against a buffer so the test stays hermetic instead of writing os.Stdout.
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

// JSON output is a contract: adding fields is safe, renaming or removing one breaks consumers.
func TestWhoami_JSONOutputStability(t *testing.T) {
	cases := []struct {
		name     string
		info     WhoamiInfo
		wantKeys []string
		// optional: a key that must NOT appear (omitempty respect)
		notKey string
	}{
		{
			name: "fully populated",
			info: WhoamiInfo{
				KeyID:        "TEST123ABC",
				IssuerID:     "11111111-2222-3333-4444-555555555555",
				VendorNumber: "99999999",
				Authorized:   true,
				APIBaseURL:   "https://api.appstoreconnect.apple.com",
			},
			wantKeys: []string{"keyId", "issuerId", "vendorNumber", "authorized", "apiBaseUrl"},
		},
		{
			name: "vendorNumber omitted when empty",
			info: WhoamiInfo{
				KeyID:      "TEST123ABC",
				IssuerID:   "11111111-2222-3333-4444-555555555555",
				Authorized: false,
				APIBaseURL: "https://api.appstoreconnect.apple.com",
			},
			wantKeys: []string{"keyId", "issuerId", "authorized", "apiBaseUrl"},
			notKey:   "vendorNumber",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertWhoamiJSONKeys(t, tc.info, tc.wantKeys, tc.notKey)
		})
	}
}

func assertWhoamiJSONKeys(t *testing.T, info WhoamiInfo, wantKeys []string, notKey string) {
	t.Helper()
	var buf bytes.Buffer
	if err := renderTo(&buf, info, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range wantKeys {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing JSON key %q: JSON output is a contract; "+
				"adding fields is safe but removing/renaming breaks consumers. "+
				"Got keys: %v", key, mapKeys(decoded))
		}
	}
	if notKey != "" {
		if _, ok := decoded[notKey]; ok {
			t.Errorf("JSON key %q leaked when value was empty (omitempty broken): %s",
				notKey, buf.String())
		}
	}
}

// `authorized` must stay a JSON bool; a "true"/"false" string would break `jq -e .authorized`.
func TestWhoami_AuthorizedTypePreservation(t *testing.T) {
	info := WhoamiInfo{KeyID: "K", IssuerID: "I", Authorized: true}
	var buf bytes.Buffer
	if err := renderTo(&buf, info, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, ok := decoded["authorized"]
	if !ok {
		t.Fatal("authorized key missing")
	}
	if _, isBool := got.(bool); !isBool {
		t.Errorf("authorized = %v (%T), want bool", got, got)
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
