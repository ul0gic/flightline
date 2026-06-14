package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestIAPWriteCommands_RegisteredOnRoot(t *testing.T) {
	var iap *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "iap" {
			iap = c
			break
		}
	}
	if iap == nil {
		t.Fatal("iap not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range iap.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"create", "update", "delete", "localizations", "review-screenshot"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("iap subcommand %q not registered", want)
		}
	}

	loc := subs["localizations"]
	if loc == nil {
		t.Fatal("iap localizations subcommand missing")
	}
	locSubs := map[string]bool{}
	for _, sc := range loc.Commands() {
		locSubs[sc.Name()] = true
	}
	for _, want := range []string{"list", "set"} {
		if !locSubs[want] {
			t.Errorf("iap localizations %q subcommand missing", want)
		}
	}

	shot := subs["review-screenshot"]
	if shot == nil {
		t.Fatal("iap review-screenshot subcommand missing")
	}
	shotSubs := map[string]bool{}
	for _, sc := range shot.Commands() {
		shotSubs[sc.Name()] = true
	}
	if !shotSubs["upload"] {
		t.Errorf("iap review-screenshot upload subcommand missing")
	}
}

func TestIAPCreate_FlagsRequired(t *testing.T) {
	for _, name := range []string{"product-id", "type", "name"} {
		f := iapCreateCmd.Flag(name)
		if f == nil {
			t.Fatalf("iap create: --%s flag missing", name)
		}
		if v, _ := iapCreateCmd.Flags().GetString(name); v != "" {
			_ = iapCreateCmd.Flags().Set(name, "")
		}
		req := f.Annotations[cobra.BashCompOneRequiredFlag]
		if len(req) != 1 || req[0] != "true" {
			t.Errorf("iap create: --%s should be marked required (got %v)", name, req)
		}
	}
}

func TestIAPDelete_RefusesWithoutYes(t *testing.T) {
	prevYes := iapDeleteYes
	prevProduct := iapDeleteProduct
	t.Cleanup(func() {
		iapDeleteYes = prevYes
		iapDeleteProduct = prevProduct
	})
	iapDeleteYes = false
	iapDeleteProduct = "com.example.testapp.lifetime"

	err := runIAPDelete(iapDeleteCmd, []string{"com.example.alpha"})
	if err == nil {
		t.Fatal("runIAPDelete: want error without --yes, got nil")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("runIAPDelete error %q should name --yes", err)
	}
}

func TestIAPCreate_RejectsBadType(t *testing.T) {
	for _, raw := range []string{"", "consumable", "AUTO_RENEWABLE_SUBSCRIPTION", "garbage"} {
		if isValidIAPType(raw) {
			t.Errorf("isValidIAPType(%q) = true, want false", raw)
		}
	}
	for _, raw := range []string{"CONSUMABLE", "NON_CONSUMABLE", "NON_RENEWING_SUBSCRIPTION"} {
		if !isValidIAPType(raw) {
			t.Errorf("isValidIAPType(%q) = false, want true", raw)
		}
	}
}

// Idempotency relies on "leave alone" round-tripping as omitted, not false.
func TestResolveTriBool(t *testing.T) {
	cases := []struct {
		raw      string
		wantNil  bool
		wantTrue bool
		wantErr  bool
	}{
		{"", true, false, false},
		{"true", false, true, false},
		{"True", false, true, false},
		{"yes", false, true, false},
		{"1", false, true, false},
		{"false", false, false, false},
		{"FALSE", false, false, false},
		{"no", false, false, false},
		{"0", false, false, false},
		{"junk", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, err := resolveTriBool("flag", c.raw)
			assertTriBool(t, got, err, c.wantErr, c.wantNil, c.wantTrue)
		})
	}
}

func assertTriBool(t *testing.T, got *bool, err error, wantErr, wantNil, wantTrue bool) {
	t.Helper()
	if wantErr {
		if err == nil {
			t.Fatal("want error, got nil")
		}
		return
	}
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if wantNil {
		if got != nil {
			t.Errorf("got = %v, want nil", *got)
		}
		return
	}
	if got == nil {
		t.Fatal("got = nil, want non-nil")
	}
	if *got != wantTrue {
		t.Errorf("got = %v, want %v", *got, wantTrue)
	}
}

func TestBoolPtrEq(t *testing.T) {
	tt := true
	ff := false
	tt2 := true
	cases := []struct {
		a, b *bool
		want bool
	}{
		{nil, nil, true},
		{&tt, nil, false},
		{nil, &ff, false},
		{&tt, &tt2, true},
		{&tt, &ff, false},
	}
	for i, c := range cases {
		if got := boolPtrEq(c.a, c.b); got != c.want {
			t.Errorf("case %d: boolPtrEq = %v, want %v", i, got, c.want)
		}
	}
}

// Renames break LLM consumers and scripted callers parsing `noop`.
func TestIAPWriteResult_JSONShape(t *testing.T) {
	fs := true
	r := IAPWriteResult{
		Action:    "create",
		ID:        "6500000001",
		Type:      "inAppPurchases",
		ProductID: "com.example.testapp.lifetime",
		NoOp:      false,
		Attributes: asc.IAPAttributes{
			Name:              "Lifetime Pro",
			ProductID:         "com.example.testapp.lifetime",
			InAppPurchaseType: "NON_CONSUMABLE",
			State:             "MISSING_METADATA",
			FamilySharable:    &fs,
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"create"`,
		`"id":"6500000001"`,
		`"type":"inAppPurchases"`,
		`"productId":"com.example.testapp.lifetime"`,
		`"noop":false`,
		`"attributes":`,
		`"familySharable":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestIAPWriteResult_JSONShape_Noop(t *testing.T) {
	r := IAPWriteResult{
		Action:    "update",
		ID:        "6500000001",
		Type:      "inAppPurchases",
		ProductID: "com.example.testapp.lifetime",
		NoOp:      true,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"noop":true`) {
		t.Errorf("noop branch missing: %s", b)
	}
}

func TestIAPLocalizationWriteResult_JSONShape(t *testing.T) {
	r := IAPLocalizationWriteResult{
		Action: "create",
		ID:     "loc1",
		Type:   "inAppPurchaseLocalizations",
		NoOp:   false,
		Attributes: asc.IAPLocalizationAttributes{
			Name:        "Lifetime Pro",
			Locale:      "en-US",
			Description: "Unlock everything.",
			State:       "PREPARE_FOR_SUBMISSION",
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"create"`,
		`"id":"loc1"`,
		`"locale":"en-US"`,
		`"name":"Lifetime Pro"`,
		`"state":"PREPARE_FOR_SUBMISSION"`,
		`"noop":false`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestIAPScreenshotUploadResult_JSONShape(t *testing.T) {
	r := IAPScreenshotUploadResult{
		Action:      "upload",
		ID:          "7500000001",
		Type:        "inAppPurchaseAppStoreReviewScreenshots",
		IAPID:       "6500000001",
		ProductID:   "com.example.testapp.lifetime",
		FileName:    "lifetime_review.png",
		Checksum:    "d41d8cd98f00b204e9800998ecf8427e",
		NoOp:        false,
		TemplateURL: "https://api.appstoreconnect.apple.com/assets/iap/review/6500000001/{w}x{h}{f}",
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, &r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`"action": "upload"`,
		`"iapId": "6500000001"`,
		`"productId": "com.example.testapp.lifetime"`,
		`"fileName": "lifetime_review.png"`,
		`"checksum": "d41d8cd98f00b204e9800998ecf8427e"`,
		`"templateUrl":`,
		`"noop": false`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %s", want, out)
		}
	}
}

func TestIAPWriteResult_TableRows(t *testing.T) {
	r := &IAPWriteResult{Action: "create", ID: "1", Type: "inAppPurchases", ProductID: "p1"}
	headers, rows := r.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 8 {
		t.Errorf("rows = %d, want >= 8", len(rows))
	}
	if rows[0][0] != "ACTION" || rows[0][1] != "create" {
		t.Errorf("rows[0] = %v, want [ACTION create]", rows[0])
	}
}

// Apple compares sourceFileChecksum byte-for-byte; a digest divergence
// here would silently break idempotency.
func TestFileMD5Hex_StableHashOfKnownInput(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sample.bin"
	if err := writeTestFile(path, "hello\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := fileMD5Hex(path)
	if err != nil {
		t.Fatalf("fileMD5Hex: %v", err)
	}
	const want = "b1946ac92492d2347c6235b4d2611184"
	if got != want {
		t.Errorf("md5 = %q, want %q", got, want)
	}
}

func TestBaseFileName_TrailingPathElement(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/tmp/lifetime_review.png", "lifetime_review.png"},
		{"lifetime_review.png", "lifetime_review.png"},
		{"./review/lifetime_review.png", "lifetime_review.png"},
	}
	for _, c := range cases {
		if got := baseFileName(c.in); got != c.want {
			t.Errorf("baseFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLookupIAP_FixtureReplay_Found(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_get"},
	})
	c := fixtureASCClient(t, srv)
	id, attrs, err := lookupIAP(context.Background(), c, "1234567890", "com.example.testapp.lifetime")
	if err != nil {
		t.Fatalf("lookupIAP: %v", err)
	}
	if id != "6500000001" {
		t.Errorf("id = %q, want 6500000001", id)
	}
	if attrs.ProductID != "com.example.testapp.lifetime" {
		t.Errorf("productId = %q, want com.example.testapp.lifetime", attrs.ProductID)
	}
}

func TestLookupIAP_FixtureReplay_NotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_get_notFound"},
	})
	c := fixtureASCClient(t, srv)
	_, _, err := lookupIAP(context.Background(), c, "1234567890", "com.example.unknown")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), `"com.example.unknown"`) {
		t.Errorf("error %q should name the productId", err)
	}
}

func TestFindLocalization_FixtureReplay_Match(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v2/inAppPurchases/6500000001/inAppPurchaseLocalizations": {File: "iap_localizations_list"},
	})
	c := fixtureASCClient(t, srv)
	got, err := findLocalization(context.Background(), c, "6500000001", "en-US")
	if err != nil {
		t.Fatalf("findLocalization: %v", err)
	}
	if got == nil {
		t.Fatal("findLocalization: want hit, got nil")
	}
	if got.Attributes.Locale != "en-US" {
		t.Errorf("locale = %q, want en-US", got.Attributes.Locale)
	}
}

func TestFindLocalization_FixtureReplay_NoMatch(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v2/inAppPurchases/6500000001/inAppPurchaseLocalizations": {File: "iap_localizations_list"},
	})
	c := fixtureASCClient(t, srv)
	got, err := findLocalization(context.Background(), c, "6500000001", "xx-XX")
	if err != nil {
		t.Fatalf("findLocalization: %v", err)
	}
	if got != nil {
		t.Errorf("findLocalization: want nil for missing locale, got %v", got)
	}
}

func TestCurrentIAPScreenshot_FixtureReplay(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v2/inAppPurchases/6500000001/appStoreReviewScreenshot": {File: "iap_review_screenshot"},
	})
	c := fixtureASCClient(t, srv)
	checksum, tmpl, ok := currentIAPScreenshot(context.Background(), c, "6500000001")
	if !ok {
		t.Fatal("want ok=true with golden screenshot fixture")
	}
	if checksum != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("checksum = %q, want d41d8cd98f00b204e9800998ecf8427e", checksum)
	}
	if !strings.Contains(tmpl, "{w}x{h}{f}") {
		t.Errorf("templateURL missing placeholders: %q", tmpl)
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
