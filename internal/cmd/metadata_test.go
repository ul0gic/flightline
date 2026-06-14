package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMetadataCmd_Registered(t *testing.T) {
	var meta *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "metadata" {
			meta = c
			break
		}
	}
	if meta == nil {
		t.Fatal("metadata not registered on rootCmd")
	}
	var set *cobra.Command
	for _, c := range meta.Commands() {
		if c.Name() == "set" {
			set = c
			break
		}
	}
	if set == nil {
		t.Fatal("metadata set not registered")
	}
	for _, want := range []string{
		"version", "platform", "locale", "name", "subtitle", "description",
		"keywords", "whats-new", "promotional-text", "marketing-url", "support-url",
	} {
		if set.Flag(want) == nil {
			t.Errorf("metadata set: missing --%s flag", want)
		}
	}
}

// TestMetadataView_JSONShape locks the joined-localization output contract:
// every field is tagged with Apple's wire casing.
func TestMetadataView_JSONShape(t *testing.T) {
	v := MetadataView{
		Locale:                "en-US",
		Name:                  "MyApp",
		Subtitle:              "Tagline",
		Description:           "...",
		Keywords:              "k1,k2",
		WhatsNew:              "Notes.",
		PromotionalText:       "Try!",
		MarketingURL:          "https://example.com/m",
		SupportURL:            "https://example.com/s",
		VersionLocalizationID: "AC000000001",
		AppInfoLocalizationID: "AI000000001",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"locale":"en-US"`, `"name":"MyApp"`, `"subtitle":"Tagline"`,
		`"description":"..."`, `"keywords":"k1,k2"`, `"whatsNew":"Notes."`,
		`"promotionalText":"Try!"`, `"marketingUrl":"https://example.com/m"`,
		`"supportUrl":"https://example.com/s"`,
		`"versionLocalizationId":"AC000000001"`, `"appInfoLocalizationId":"AI000000001"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %s", want, out)
		}
	}
}

func TestMetadataSetResult_JSONShape(t *testing.T) {
	r := MetadataSetResult{
		Action:   "both",
		Changed:  true,
		Metadata: MetadataView{Locale: "en-US", Name: "MyApp"},
	}
	b, _ := json.Marshal(r)
	out := string(b)
	for _, want := range []string{`"action":"both"`, `"changed":true`, `"metadata":`, `"name":"MyApp"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %s", want, out)
		}
	}
}

func TestMetadataSetResult_TableRows(t *testing.T) {
	r := &MetadataSetResult{
		Action:   "noop",
		Changed:  false,
		Metadata: MetadataView{Locale: "en-US", Name: "MyApp", Description: strings.Repeat("x", 200)},
	}
	headers, rows := r.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %v, want 2 cols", headers)
	}
	if rows[0][0] != "ACTION" || rows[0][1] != "noop" {
		t.Errorf("rows[0] = %v, want ACTION/noop", rows[0])
	}
	// Description is truncated in the table; full value still in JSON.
	for _, row := range rows {
		if row[0] == "DESCRIPTION" {
			if !strings.HasSuffix(row[1], "…") {
				t.Errorf("DESCRIPTION cell not truncated: %q", row[1])
			}
		}
	}
}

func TestComputeAction(t *testing.T) {
	cases := []struct {
		v, a bool
		want string
	}{
		{false, false, "noop"},
		{true, false, "version"},
		{false, true, "app-info"},
		{true, true, "both"},
	}
	for _, c := range cases {
		got := computeAction(c.v, c.a)
		if got != c.want {
			t.Errorf("computeAction(%v,%v) = %q, want %q", c.v, c.a, got, c.want)
		}
	}
}

// TestMetadataFlagSet_Predicates exercises any/anyVersion/anyAppInfo.
func TestMetadataFlagSet_Predicates(t *testing.T) {
	none := metadataFlagSet{}
	if none.any() {
		t.Error("empty flag set: any()=true, want false")
	}
	versionOnly := metadataFlagSet{description: true}
	if !versionOnly.anyVersionFlag() || versionOnly.anyAppInfoFlag() {
		t.Error("description-only: expected version=true, app-info=false")
	}
	appInfoOnly := metadataFlagSet{name: true}
	if appInfoOnly.anyVersionFlag() || !appInfoOnly.anyAppInfoFlag() {
		t.Error("name-only: expected version=false, app-info=true")
	}
}

// TestDiffVersionLocAttrs_NoChangeWhenIdentical asserts the diff path skips
// fields whose intended value already matches the live record.
func TestDiffVersionLocAttrs_NoChangeWhenIdentical(t *testing.T) {
	cur := metadataASCVersionLocalizationAttrs{Description: "same", Keywords: "k1,k2"}
	flags := metadataFlagSet{description: true, keywords: true}
	out, changed := diffVersionLocAttrs(flags, cur, "same", "k1,k2", "", "", "", "")
	if changed {
		t.Errorf("diff: changed=true for identical values, want false. attrs=%+v", out)
	}
}

func TestDiffVersionLocAttrs_OnlyDifferingFields(t *testing.T) {
	cur := metadataASCVersionLocalizationAttrs{Description: "old", Keywords: "k1"}
	flags := metadataFlagSet{description: true, keywords: true, whatsNew: true}
	out, changed := diffVersionLocAttrs(flags, cur, "new", "k1", "Bug fixes.", "", "", "")
	if !changed {
		t.Error("diff: changed=false, want true")
	}
	if out.Description == nil || *out.Description != "new" {
		t.Errorf("Description = %v, want pointer to 'new'", out.Description)
	}
	if out.Keywords != nil {
		t.Errorf("Keywords = %v, want nil (identical)", out.Keywords)
	}
	if out.WhatsNew == nil || *out.WhatsNew != "Bug fixes." {
		t.Errorf("WhatsNew = %v, want pointer to 'Bug fixes.'", out.WhatsNew)
	}
}

// TestDiffVersionLocAttrs_UnsetFlagsIgnored locks the rule that unpassed flags
// stay out of the diff; otherwise empty-string defaults would clear live fields.
func TestDiffVersionLocAttrs_UnsetFlagsIgnored(t *testing.T) {
	cur := metadataASCVersionLocalizationAttrs{Description: "live"}
	flags := metadataFlagSet{} // no flags set
	out, changed := diffVersionLocAttrs(flags, cur, "", "", "", "", "", "")
	if changed {
		t.Errorf("diff: changed=true with no flags set; want false. attrs=%+v", out)
	}
}

func TestDiffAppInfoLocAttrs_OnlyDifferingFields(t *testing.T) {
	cur := metadataAppInfoLocalizationAttrs{Name: "MyApp", Subtitle: "old"}
	flags := metadataFlagSet{name: true, subtitle: true}
	out, changed := diffAppInfoLocAttrs(flags, cur, "MyApp", "new")
	if !changed {
		t.Error("diff: changed=false, want true (subtitle differs)")
	}
	if out.Name != nil {
		t.Errorf("Name = %v, want nil (identical)", out.Name)
	}
	if out.Subtitle == nil || *out.Subtitle != "new" {
		t.Errorf("Subtitle = %v, want pointer to 'new'", out.Subtitle)
	}
}

// TestVersionLocPatch_OmitsUnsetFields locks the wire body shape: pointer +
// omitempty drops unpassed fields so a partial PATCH doesn't clear others.
func TestVersionLocPatch_OmitsUnsetFields(t *testing.T) {
	body := versionLocalizationPatch{
		Data: versionLocalizationPatchData{
			Type: "appStoreVersionLocalizations",
			ID:   "AC000000001",
			Attributes: versionLocalizationPatchAttributes{
				WhatsNew: strPtr("New release notes."),
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	if !strings.Contains(out, `"whatsNew":"New release notes."`) {
		t.Errorf("missing whatsNew: %s", out)
	}
	for _, leak := range []string{`"description"`, `"keywords"`, `"promotionalText"`, `"marketingUrl"`, `"supportUrl"`} {
		if strings.Contains(out, leak) {
			t.Errorf("body leaks unset field %s: %s", leak, out)
		}
	}
}

// TestAppInfoLocPatch_OmitsUnsetFields: same wire-shape lock for the
// appInfo variant.
func TestAppInfoLocPatch_OmitsUnsetFields(t *testing.T) {
	body := appInfoLocalizationPatch{
		Data: appInfoLocalizationPatchData{
			Type: "appInfoLocalizations",
			ID:   "AI000000001",
			Attributes: appInfoLocalizationPatchAttributes{
				Subtitle: strPtr("New tagline"),
			},
		},
	}
	b, _ := json.Marshal(body)
	out := string(b)
	if !strings.Contains(out, `"subtitle":"New tagline"`) {
		t.Errorf("missing subtitle: %s", out)
	}
	if strings.Contains(out, `"name"`) {
		t.Errorf("body leaks unset name: %s", out)
	}
}

// TestMetadata_FixtureReplay_GetVersionLoc exercises the version-localization
// idempotency probe against the fixture server.
func TestMetadata_FixtureReplay_GetVersionLoc(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/appStoreVersionLocalizations": {File: "metadata_version_loc_existing"},
	})
	c := fixtureASCClient(t, srv)
	id, attrs, err := getVersionLocalization(context.Background(), c, "8000000001", "en-US")
	if err != nil {
		t.Fatalf("getVersionLocalization: %v", err)
	}
	if id != "AC000000001" {
		t.Errorf("id = %q, want AC000000001", id)
	}
	if attrs.Locale != "en-US" {
		t.Errorf("locale = %q, want en-US", attrs.Locale)
	}
	if attrs.WhatsNew != "Initial release." {
		t.Errorf("whatsNew = %q, want 'Initial release.'", attrs.WhatsNew)
	}
}

func TestMetadata_FixtureReplay_GetVersionLoc_Missing(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/appStoreVersionLocalizations": {File: "metadata_version_loc_empty"},
	})
	c := fixtureASCClient(t, srv)
	id, _, err := getVersionLocalization(context.Background(), c, "8000000001", "de-DE")
	if err != nil {
		t.Fatalf("getVersionLocalization: %v", err)
	}
	if id != "" {
		t.Errorf("id = %q, want '' for missing locale", id)
	}
}

// TestMetadata_FixtureReplay_PatchVersionLoc exercises the PATCH path; the
// fixture returns the post-state body so the after-image renders correctly.
func TestMetadata_FixtureReplay_PatchVersionLoc(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"PATCH /v1/appStoreVersionLocalizations/AC000000001": {File: "metadata_version_loc_patched"},
	})
	c := fixtureASCClient(t, srv)
	updated, err := patchVersionLocalization(context.Background(), c, "AC000000001", versionLocalizationPatchAttributes{
		Description: strPtr("Updated description for the app."),
		WhatsNew:    strPtr("Bug fixes and performance improvements."),
	})
	if err != nil {
		t.Fatalf("patchVersionLocalization: %v", err)
	}
	if updated.Description != "Updated description for the app." {
		t.Errorf("description = %q", updated.Description)
	}
	if updated.WhatsNew != "Bug fixes and performance improvements." {
		t.Errorf("whatsNew = %q", updated.WhatsNew)
	}
}

func TestMetadata_FixtureReplay_GetAppInfoLoc(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appInfos/AP000000001/appInfoLocalizations": {File: "metadata_appinfo_loc_existing"},
	})
	c := fixtureASCClient(t, srv)
	id, attrs, err := getAppInfoLocalization(context.Background(), c, "AP000000001", "en-US")
	if err != nil {
		t.Fatalf("getAppInfoLocalization: %v", err)
	}
	if id != "AI000000001" {
		t.Errorf("id = %q, want AI000000001", id)
	}
	if attrs.Name != "MyApp" {
		t.Errorf("name = %q, want MyApp", attrs.Name)
	}
}

func TestMetadata_FixtureReplay_PatchAppInfoLoc(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"PATCH /v1/appInfoLocalizations/AI000000001": {File: "metadata_appinfo_loc_patched"},
	})
	c := fixtureASCClient(t, srv)
	updated, err := patchAppInfoLocalization(context.Background(), c, "AI000000001", appInfoLocalizationPatchAttributes{
		Subtitle: strPtr("New tagline"),
	})
	if err != nil {
		t.Fatalf("patchAppInfoLocalization: %v", err)
	}
	if updated.Subtitle != "New tagline" {
		t.Errorf("subtitle = %q, want 'New tagline'", updated.Subtitle)
	}
}

// TestTruncateForTable confirms long copy is summarised in the table cell.
// The contract is "rune count ≤ max", not byte count (`…` is 3 bytes).
func TestTruncateForTable(t *testing.T) {
	short := "hi"
	if got := truncateForTable(short, 10); got != short {
		t.Errorf("short: got %q, want unchanged", got)
	}
	long := strings.Repeat("a", 100)
	got := truncateForTable(long, 10)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long: got %q, want suffix …", got)
	}
	if len([]rune(got)) != 10 {
		t.Errorf("long: rune count = %d, want 10", len([]rune(got)))
	}
}
