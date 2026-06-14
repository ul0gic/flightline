package cmd

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestAgeRatingSet_RegisteredOnRoot(t *testing.T) {
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
	for _, want := range []string{"get", "set"} {
		if !subs[want] {
			t.Errorf("age-rating %q subcommand missing", want)
		}
	}
}

func TestAgeRatingSet_FlagsRequired(t *testing.T) {
	for _, name := range []string{"version", "from"} {
		f := ageRatingSetCmd.Flag(name)
		if f == nil {
			t.Fatalf("age-rating set: --%s flag missing", name)
		}
		req := f.Annotations[cobra.BashCompOneRequiredFlag]
		if len(req) != 1 || req[0] != "true" {
			t.Errorf("age-rating set: --%s should be required", name)
		}
	}
}

func TestLoadAgeRatingPayload_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ar.json")
	if err := os.WriteFile(path, []byte(`{"alcoholTobaccoOrDrugUseOrReferences":"NONE","advertising":false}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := loadAgeRatingPayload(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := p.providedKeys()["alcoholTobaccoOrDrugUseOrReferences"]; !ok {
		t.Error("providedKeys missing alcoholTobaccoOrDrugUseOrReferences")
	}
	attrs, err := p.toAttributes()
	if err != nil {
		t.Fatalf("toAttributes: %v", err)
	}
	if attrs.AlcoholTobaccoOrDrugUseOrReferences != "NONE" {
		t.Errorf("alcohol = %q, want NONE", attrs.AlcoholTobaccoOrDrugUseOrReferences)
	}
	if attrs.Advertising == nil || *attrs.Advertising {
		t.Errorf("advertising = %v, want false ptr", attrs.Advertising)
	}
}

func TestLoadAgeRatingPayload_YAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ar.yaml")
	if err := os.WriteFile(path, []byte("alcoholTobaccoOrDrugUseOrReferences: INFREQUENT_OR_MILD\nadvertising: true\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := loadAgeRatingPayload(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	attrs, err := p.toAttributes()
	if err != nil {
		t.Fatalf("toAttributes: %v", err)
	}
	if attrs.AlcoholTobaccoOrDrugUseOrReferences != "INFREQUENT_OR_MILD" {
		t.Errorf("alcohol = %q", attrs.AlcoholTobaccoOrDrugUseOrReferences)
	}
	if attrs.Advertising == nil || !*attrs.Advertising {
		t.Errorf("advertising = %v, want true ptr", attrs.Advertising)
	}
}

func TestLoadAgeRatingPayload_RejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("alcoholTobaccoOrDrugUseOrReferences: NONE\ntotallyMadeUpKey: true\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := loadAgeRatingPayload(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err = p.toAttributes()
	if err == nil {
		t.Fatal("toAttributes: want error for unknown key")
	}
	if !strings.Contains(err.Error(), "totallyMadeUpKey") {
		t.Errorf("error %q should name the bad key", err)
	}
}

func TestLoadAgeRatingPayload_RejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path, []byte("   \n\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadAgeRatingPayload(path)
	if err == nil {
		t.Fatal("want error for empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty: %v", err)
	}
}

func TestLoadAgeRatingPayload_MissingFile(t *testing.T) {
	_, err := loadAgeRatingPayload(filepath.Join(t.TempDir(), "noSuchFile"))
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestValidateAgeRatingAttributes_RejectsBadFrequency(t *testing.T) {
	a := asc.AgeRatingDeclarationAttributes{
		AlcoholTobaccoOrDrugUseOrReferences: "MEDIUM", // not in the enum
	}
	err := validateAgeRatingAttributes(a)
	if err == nil {
		t.Fatal("want error for bad frequency")
	}
	if !strings.Contains(err.Error(), "MEDIUM") {
		t.Errorf("error should name the bad value: %v", err)
	}
}

func TestValidateAgeRatingAttributes_AllowsEmptyFrequency(t *testing.T) {
	a := asc.AgeRatingDeclarationAttributes{} // all zero
	if err := validateAgeRatingAttributes(a); err != nil {
		t.Errorf("zero-value should validate: %v", err)
	}
}

func TestValidateAgeRatingAttributes_AcceptsValidFrequency(t *testing.T) {
	for _, v := range []string{"NONE", "INFREQUENT_OR_MILD", "FREQUENT_OR_INTENSE", "INFREQUENT", "FREQUENT"} {
		a := asc.AgeRatingDeclarationAttributes{AlcoholTobaccoOrDrugUseOrReferences: v}
		if err := validateAgeRatingAttributes(a); err != nil {
			t.Errorf("%s: want pass, got %v", v, err)
		}
	}
}

func TestDiffAgeRating_OnlyDifferingFields(t *testing.T) {
	tt := true
	ff := false
	current := asc.AgeRatingDeclarationAttributes{
		AlcoholTobaccoOrDrugUseOrReferences: "NONE",
		Advertising:                         &tt,
	}
	desired := asc.AgeRatingDeclarationAttributes{
		AlcoholTobaccoOrDrugUseOrReferences: "INFREQUENT", // differs
		Advertising:                         &tt,          // same
		Gambling:                            &ff,          // new
	}
	supplied := map[string]struct{}{
		"alcoholTobaccoOrDrugUseOrReferences": {},
		"advertising":                         {},
		"gambling":                            {},
	}
	diff := diffAgeRating(current, desired, supplied)
	if _, ok := diff["alcoholTobaccoOrDrugUseOrReferences"]; !ok {
		t.Error("missing alcoholTobaccoOrDrugUseOrReferences in diff")
	}
	if _, ok := diff["gambling"]; !ok {
		t.Error("missing gambling in diff")
	}
	if _, ok := diff["advertising"]; ok {
		t.Error("advertising should NOT be in diff (unchanged)")
	}
}

func TestDiffAgeRating_NotProvided_NotIncluded(t *testing.T) {
	current := asc.AgeRatingDeclarationAttributes{AlcoholTobaccoOrDrugUseOrReferences: "NONE"}
	desired := asc.AgeRatingDeclarationAttributes{} // zero: would differ
	// User did not supply any keys: diff should be empty.
	diff := diffAgeRating(current, desired, map[string]struct{}{})
	if len(diff) != 0 {
		t.Errorf("diff = %v, want empty", diff)
	}
}

func TestDiffAgeRating_NoChanges_Empty(t *testing.T) {
	current := asc.AgeRatingDeclarationAttributes{AlcoholTobaccoOrDrugUseOrReferences: "NONE"}
	desired := asc.AgeRatingDeclarationAttributes{AlcoholTobaccoOrDrugUseOrReferences: "NONE"}
	supplied := map[string]struct{}{"alcoholTobaccoOrDrugUseOrReferences": {}}
	diff := diffAgeRating(current, desired, supplied)
	if len(diff) != 0 {
		t.Errorf("diff = %v, want empty (idempotent)", diff)
	}
}

func TestAgeRatingWriteResult_JSONShape(t *testing.T) {
	r := AgeRatingWriteResult{
		Action:       "set",
		ID:           "AAAA",
		Type:         "ageRatingDeclarations",
		BundleID:     "com.example.alpha",
		Version:      "1.0.1",
		VersionState: "PREPARE_FOR_SUBMISSION",
		NoOp:         false,
		ChangedKeys:  []string{"advertising", "gambling"},
		Attributes:   asc.AgeRatingDeclarationAttributes{AlcoholTobaccoOrDrugUseOrReferences: "NONE"},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"set"`,
		`"bundleId":"com.example.alpha"`,
		`"version":"1.0.1"`,
		`"versionState":"PREPARE_FOR_SUBMISSION"`,
		`"noop":false`,
		`"changedKeys":["advertising","gambling"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestAgeRatingSet_FixtureReplay_Idempotent(t *testing.T) {
	// No PATCH route registered: a write would 404 and fail the test.
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":         {File: "age_rating_version_lookup"},
		"GET /v1/apps/1234567890/appInfos":                 {File: "age_rating_app_infos"},
		"GET /v1/appInfos/9000000001/ageRatingDeclaration": {File: "age_rating_get"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	state, err := lookupVersionState(context.Background(), c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersionState: %v", err)
	}
	appInfoID, err := pickAppInfoForVersion(context.Background(), c, appID, state)
	if err != nil {
		t.Fatalf("pickAppInfoForVersion: %v", err)
	}
	current, declID, err := fetchAgeRatingDeclaration(context.Background(), c, appInfoID)
	if err != nil {
		t.Fatalf("fetchAgeRatingDeclaration: %v", err)
	}
	if declID == "" {
		t.Fatal("declID empty")
	}

	supplied := map[string]struct{}{"alcoholTobaccoOrDrugUseOrReferences": {}}
	diff := diffAgeRating(current, current, supplied)
	if len(diff) != 0 {
		t.Errorf("self-diff non-empty: %v", diff)
	}
}

func TestAssertKnownAgeRatingKeys_NamesAllUnknown(t *testing.T) {
	raw := map[string]any{
		"advertising":  true,
		"madeUpKeyOne": "x",
		"madeUpKeyTwo": "y",
	}
	err := assertKnownAgeRatingKeys(raw)
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"madeUpKeyOne", "madeUpKeyTwo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestSortStrings(t *testing.T) {
	in := []string{"cherry", "apple", "banana"}
	sortStrings(in)
	want := []string{"apple", "banana", "cherry"}
	for i := range want {
		if in[i] != want[i] {
			t.Errorf("sortStrings[%d] = %q, want %q", i, in[i], want[i])
		}
	}
}

func TestLookupVersionState_ReturnsState(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "age_rating_version_lookup"},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	state, err := lookupVersionState(context.Background(), c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersionState: %v", err)
	}
	if state == "" {
		t.Error("state empty")
	}
}

// Silences staticcheck warning on the (unused-locally) url package: the
// import is kept for parity with sibling test files that use url.Values inline.
var _ = url.Values{}
