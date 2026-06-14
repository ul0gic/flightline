package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestIAPView_JSONShape(t *testing.T) {
	fs := true
	ch := false
	v := IAPView{
		ID:   "6500000001",
		Type: "inAppPurchases",
		Attributes: asc.IAPAttributes{
			Name:              "Lifetime Pro",
			ProductID:         "com.example.testapp.lifetime",
			InAppPurchaseType: asc.IAPTypeNonConsumable,
			State:             asc.IAPStateApproved,
			ReviewNote:        "One-time purchase that unlocks all premium features.",
			FamilySharable:    &fs,
			ContentHosting:    &ch,
		},
		ReviewScreenshotURL: "https://api.appstoreconnect.apple.com/assets/iap/review/6500000001/{w}x{h}{f}",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"6500000001"`,
		`"type":"inAppPurchases"`,
		`"productId":"com.example.testapp.lifetime"`,
		`"inAppPurchaseType":"NON_CONSUMABLE"`,
		`"state":"APPROVED"`,
		`"familySharable":true`,
		`"contentHosting":false`,
		`"reviewScreenshotUrl":`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestIAPList_TableRowsHeaders(t *testing.T) {
	list := IAPList{
		IAPs: []IAPView{
			{ID: "1", Attributes: asc.IAPAttributes{ProductID: "com.example.testapp.lifetime", Name: "Lifetime", InAppPurchaseType: "NON_CONSUMABLE", State: "APPROVED"}},
			{ID: "2", Attributes: asc.IAPAttributes{ProductID: "com.example.testapp.coins", Name: "Coins", InAppPurchaseType: "CONSUMABLE", State: "READY_TO_SUBMIT"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"PRODUCT_ID", "NAME", "TYPE", "STATE", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "com.example.testapp.lifetime" {
		t.Errorf("rows[0][0] = %q, want com.example.testapp.lifetime", rows[0][0])
	}
}

func TestIAPView_TableRows_VerticalLayout(t *testing.T) {
	v := &IAPView{ID: "1", Type: "inAppPurchases", Attributes: asc.IAPAttributes{ProductID: "com.example.testapp.lifetime"}}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 8 {
		t.Errorf("rows = %d, want >= 8 (one per attribute)", len(rows))
	}
}

func TestIAPLocalizationList_TableRowsHeaders(t *testing.T) {
	list := IAPLocalizationList{
		Localizations: []IAPLocalizationView{
			{ID: "1", Attributes: asc.IAPLocalizationAttributes{Locale: "en-US", Name: "Lifetime", State: "APPROVED"}},
			{ID: "2", Attributes: asc.IAPLocalizationAttributes{Locale: "fr-FR", Name: "Lifetime", State: "PREPARE_FOR_SUBMISSION"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"LOCALE", "NAME", "STATE", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
}

func TestIAPCommands_RegisteredOnRoot(t *testing.T) {
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
	for _, want := range []string{"list", "get", "localizations"} {
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
	if !locSubs["list"] {
		t.Errorf("iap localizations list subcommand missing")
	}
}

// TestIAP_JSONOutputStability_List asserts the IAPList JSON shape.
// Top-level "iaps" key plus per-row attribute keys are a contract.
func TestIAP_JSONOutputStability_List(t *testing.T) {
	fs := true
	ch := false
	list := IAPList{
		IAPs: []IAPView{
			{
				ID:   "6500000001",
				Type: "inAppPurchases",
				Attributes: asc.IAPAttributes{
					Name:              "Lifetime Pro",
					ProductID:         "com.example.testapp.lifetime",
					InAppPurchaseType: "NON_CONSUMABLE",
					State:             "APPROVED",
					ReviewNote:        "review note",
					FamilySharable:    &fs,
					ContentHosting:    &ch,
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		IAPs []map[string]any `json:"iaps"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.IAPs) != 1 {
		t.Fatalf("iaps len = %d, want 1", len(decoded.IAPs))
	}
	row := decoded.IAPs[0]
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing per-row key %q: JSON contract drift. Got keys: %v", key, mapKeys(row))
		}
	}
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes is not an object: %T", row["attributes"])
	}
	for _, key := range []string{"name", "productId", "inAppPurchaseType", "state", "reviewNote", "familySharable", "contentHosting"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute key %q: JSON contract drift. Got: %v", key, mapKeys(attrs))
		}
	}
}

// TestIAPType_StaysInAppPurchases locks the resource type literal.
func TestIAPType_StaysInAppPurchases(t *testing.T) {
	v := IAPView{ID: "1", Type: "inAppPurchases"}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"type":"inAppPurchases"`) {
		t.Errorf("type literal regression: %s", b)
	}
}

// TestIAP_FixtureReplay_List exercises collectIAPs against the golden fixture
// along the path `iap list` takes.
func TestIAP_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_list"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectIAPs(context.Background(), c, "/v1/apps/"+appID+"/inAppPurchasesV2", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectIAPs: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("iaps len = %d, want 2", len(views))
	}
	if views[0].Attributes.ProductID != "com.example.testapp.lifetime" {
		t.Errorf("views[0].productId = %q, want com.example.testapp.lifetime", views[0].Attributes.ProductID)
	}
	if views[1].Attributes.InAppPurchaseType != "CONSUMABLE" {
		t.Errorf("views[1].inAppPurchaseType = %q, want CONSUMABLE", views[1].Attributes.InAppPurchaseType)
	}
}

// TestIAP_FixtureReplay_GetWithScreenshot exercises the full `iap get` happy
// path including the optional screenshot relationship hop.
func TestIAP_FixtureReplay_GetWithScreenshot(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2":                   {File: "iap_get"},
		"GET /v2/inAppPurchases/6500000001/appStoreReviewScreenshot": {File: "iap_review_screenshot"},
	})
	c := fixtureASCClient(t, srv)

	id, attrs, err := findIAPByProductID(context.Background(), c, "com.example.alpha", "com.example.testapp.lifetime")
	if err != nil {
		t.Fatalf("findIAPByProductID: %v", err)
	}
	if id != "6500000001" {
		t.Errorf("id = %q, want 6500000001", id)
	}
	if attrs.ProductID != "com.example.testapp.lifetime" {
		t.Errorf("productId = %q, want com.example.testapp.lifetime", attrs.ProductID)
	}
	shotURL, err := fetchIAPReviewScreenshotURL(context.Background(), c, id)
	if err != nil {
		t.Fatalf("fetchIAPReviewScreenshotURL: %v", err)
	}
	if !strings.Contains(shotURL, "{w}x{h}{f}") {
		t.Errorf("review screenshot url missing template placeholders: %q", shotURL)
	}
}

// TestIAP_FixtureReplay_GetNotFound asserts the user-facing error message
// echoes the bundleId AND productId when the productId filter yields zero.
func TestIAP_FixtureReplay_GetNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_get_notFound"},
	})
	c := fixtureASCClient(t, srv)

	_, _, err := findIAPByProductID(context.Background(), c, "com.example.alpha", "com.unknown.iap")
	if err == nil {
		t.Fatal("findIAPByProductID: want error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"iap:", "no in-app purchase", `"com.unknown.iap"`, `"com.example.alpha"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing substring %q", msg, want)
		}
	}
}

// TestIAP_FixtureReplay_LocalizationsList exercises collectIAPLocalizations
// against the v2 IAP localizations relationship endpoint.
func TestIAP_FixtureReplay_LocalizationsList(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2":                     {File: "iap_get"},
		"GET /v2/inAppPurchases/6500000001/inAppPurchaseLocalizations": {File: "iap_localizations_list"},
	})
	c := fixtureASCClient(t, srv)

	id, _, err := findIAPByProductID(context.Background(), c, "com.example.alpha", "com.example.testapp.lifetime")
	if err != nil {
		t.Fatalf("findIAPByProductID: %v", err)
	}
	views, err := collectIAPLocalizations(context.Background(), c, "/v2/inAppPurchases/"+id+"/inAppPurchaseLocalizations", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectIAPLocalizations: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("localizations len = %d, want 2", len(views))
	}
	if views[0].Attributes.Locale != "en-US" {
		t.Errorf("views[0].locale = %q, want en-US", views[0].Attributes.Locale)
	}
}
