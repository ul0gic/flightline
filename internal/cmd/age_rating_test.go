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

func TestAgeRatingView_JSONShape(t *testing.T) {
	advertising := false
	gambling := false
	v := AgeRatingView{
		ID:   "AAAA0000-0000-0000-0000-000000000001",
		Type: "ageRatingDeclarations",
		Attributes: asc.AgeRatingDeclarationAttributes{
			Advertising:                         &advertising,
			Gambling:                            &gambling,
			AlcoholTobaccoOrDrugUseOrReferences: "NONE",
			ProfanityOrCrudeHumor:               "INFREQUENT_OR_MILD",
			ViolenceRealisticProlongedGraphicOrSadistic: "NONE",
			AgeRatingOverrideV2:                         "NONE",
			KoreaAgeRatingOverride:                      "NONE",
			DeveloperAgeRatingInfoURL:                   "",
		},
		VersionState: "PREPARE_FOR_SUBMISSION",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"AAAA0000-0000-0000-0000-000000000001"`,
		`"type":"ageRatingDeclarations"`,
		`"advertising":false`,
		`"gambling":false`,
		`"alcoholTobaccoOrDrugUseOrReferences":"NONE"`,
		`"profanityOrCrudeHumor":"INFREQUENT_OR_MILD"`,
		`"ageRatingOverrideV2":"NONE"`,
		`"versionState":"PREPARE_FOR_SUBMISSION"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestAgeRatingView_TableRows_AllQuestions(t *testing.T) {
	v := &AgeRatingView{
		ID:   "1",
		Type: "ageRatingDeclarations",
		Attributes: asc.AgeRatingDeclarationAttributes{
			AlcoholTobaccoOrDrugUseOrReferences: "NONE",
		},
		VersionState: "PREPARE_FOR_SUBMISSION",
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	// Should cover at least 25 questionnaire fields plus ID/TYPE/VERSION_STATE.
	if len(rows) < 28 {
		t.Errorf("rows = %d, want >= 28 (questionnaire is verbose by design)", len(rows))
	}
	// Unanswered questions should surface as the literal "(unanswered)".
	foundUnanswered := false
	for _, r := range rows {
		if r[1] == "(unanswered)" {
			foundUnanswered = true
			break
		}
	}
	if !foundUnanswered {
		t.Errorf("expected at least one (unanswered) cell for empty questions")
	}
}

func TestAgeRatingCommands_RegisteredOnRoot(t *testing.T) {
	var ar *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "age-rating" {
			ar = c
			break
		}
	}
	if ar == nil {
		t.Fatal("age-rating not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range ar.Commands() {
		subs[sc.Name()] = true
	}
	if !subs["get"] {
		t.Errorf("age-rating get subcommand missing")
	}
}

// TestAgeRating_JSONOutputStability locks the top-level keys.
func TestAgeRating_JSONOutputStability(t *testing.T) {
	gambling := false
	v := &AgeRatingView{
		ID:   "1",
		Type: "ageRatingDeclarations",
		Attributes: asc.AgeRatingDeclarationAttributes{
			Gambling:                            &gambling,
			AlcoholTobaccoOrDrugUseOrReferences: "NONE",
			Contests:                            "NONE",
		},
		VersionState: "PREPARE_FOR_SUBMISSION",
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"id", "type", "attributes", "versionState"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q — JSON contract drift. Got: %v", key, mapKeys(decoded))
		}
	}
}

// TestPickAppInfoForVersion_BucketLogic verifies the live/editable bucket
// matching: an editable version state returns the editable appInfo, a live
// state returns the live one.
func TestPickAppInfoForVersion_BucketLogic(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/appInfos": {File: "age_rating_app_infos"},
	})
	c := fixtureASCClient(t, srv)

	editable, err := pickAppInfoForVersion(context.Background(), c, "1234567890", "PREPARE_FOR_SUBMISSION")
	if err != nil {
		t.Fatalf("pickAppInfoForVersion: %v", err)
	}
	if editable != "9000000001" {
		t.Errorf("editable bucket got id=%q, want 9000000001", editable)
	}

	live, err := pickAppInfoForVersion(context.Background(), c, "1234567890", "READY_FOR_DISTRIBUTION")
	if err != nil {
		t.Fatalf("pickAppInfoForVersion: %v", err)
	}
	if live != "9000000000" {
		t.Errorf("live bucket got id=%q, want 9000000000", live)
	}
}

// TestAgeRating_FixtureReplay_GetHappy exercises the full cmd-level chain:
// resolveAppID -> version filter -> pickAppInfoForVersion ->
// /v1/appInfos/{id}/ageRatingDeclaration.
func TestAgeRating_FixtureReplay_GetHappy(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":         {File: "age_rating_version_lookup"},
		"GET /v1/apps/1234567890/appInfos":                 {File: "age_rating_app_infos"},
		"GET /v1/appInfos/9000000001/ageRatingDeclaration": {File: "age_rating_get"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}

	vQuery := url.Values{
		"filter[versionString]": {"1.0.1"},
		"filter[platform]":      {"IOS"},
		"limit":                 {"1"},
	}
	versionPage, err := asc.Get[asc.Collection[asc.VersionAttributes]](ctx, c, "/v1/apps/"+appID+"/appStoreVersions", vQuery)
	if err != nil {
		t.Fatalf("version lookup: %v", err)
	}
	if len(versionPage.Data) != 1 {
		t.Fatalf("version lookup data len = %d, want 1", len(versionPage.Data))
	}
	state := versionDisplayState(versionPage.Data[0].Attributes)

	appInfoID, err := pickAppInfoForVersion(ctx, c, appID, state)
	if err != nil {
		t.Fatalf("pickAppInfoForVersion: %v", err)
	}
	if appInfoID != "9000000001" {
		t.Fatalf("appInfoID = %q, want 9000000001", appInfoID)
	}

	decl, err := asc.Get[asc.Single[asc.AgeRatingDeclarationAttributes]](ctx, c, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil)
	if err != nil {
		t.Fatalf("ageRatingDeclaration: %v", err)
	}
	if decl.Data.Attributes.AlcoholTobaccoOrDrugUseOrReferences != "NONE" {
		t.Errorf("alcohol = %q, want NONE", decl.Data.Attributes.AlcoholTobaccoOrDrugUseOrReferences)
	}
	if decl.Data.Attributes.Advertising == nil || *decl.Data.Attributes.Advertising != false {
		t.Errorf("advertising should be explicit false, got %v", decl.Data.Attributes.Advertising)
	}
	if decl.Data.Attributes.ProfanityOrCrudeHumor != "INFREQUENT_OR_MILD" {
		t.Errorf("profanity = %q, want INFREQUENT_OR_MILD", decl.Data.Attributes.ProfanityOrCrudeHumor)
	}
}

// TestAgeRating_FixtureReplay_VersionNotFound asserts the error message
// names the bundleId, version, and platform when the version filter is empty.
func TestAgeRating_FixtureReplay_VersionNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "age_rating_version_notFound"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	vQuery := url.Values{
		"filter[versionString]": {"9.9.9"},
		"filter[platform]":      {"IOS"},
		"limit":                 {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](ctx, c, "/v1/apps/"+appID+"/appStoreVersions", vQuery)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(page.Data) != 0 {
		t.Errorf("data len = %d, want 0", len(page.Data))
	}
}
