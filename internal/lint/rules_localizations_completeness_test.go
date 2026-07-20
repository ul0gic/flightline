package lint

import (
	"context"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func completeMetadataLocale() config.MetadataLocale {
	name := "Example"
	description := "Example description"
	supportURL := "https://example.com/support"
	return config.MetadataLocale{Name: &name, Description: &description, SupportURL: &supportURL}
}

func TestLocalizationsCompleteness_NoOpWhenAllSurfacesMatch(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata:    &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": completeMetadataLocale(), "fr-FR": completeMetadataLocale()}},
		Screenshots: &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {}, "fr-FR": {}}},
	}}
	got := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0: %+v", len(got), got)
	}
}

func TestLocalizationsCompleteness_FiresWhenLocaleOnlyInMetadata(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata:    &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": completeMetadataLocale(), "fr-FR": completeMetadataLocale()}},
		Screenshots: &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {}}},
	}}
	got := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "localizations.completeness" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", got[0].Severity)
	}
	if got[0].Path != "/spec/screenshots/locales/fr-FR" {
		t.Errorf("path = %q, want /spec/screenshots/locales/fr-FR", got[0].Path)
	}
}

func TestLocalizationsCompleteness_ChecksFieldsWithSingleSurface(t *testing.T) {
	name := "Example"
	s := &config.State{Spec: config.StateSpec{
		Metadata: &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": {Name: &name}}},
	}}
	got := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 2 {
		t.Fatalf("got %d diags, want missing description and supportUrl: %+v", len(got), got)
	}
	if got[0].Path != "/spec/metadata/locales/en-US/description" || got[1].Path != "/spec/metadata/locales/en-US/supportUrl" {
		t.Errorf("unexpected field paths: %+v", got)
	}
}

func TestLocalizationsCompleteness_WhitespaceOnlyFieldIsMissing(t *testing.T) {
	locale := completeMetadataLocale()
	blank := "  \n"
	locale.SupportURL = &blank
	s := &config.State{Spec: config.StateSpec{Metadata: &config.MetadataSpec{
		Locales: map[string]config.MetadataLocale{"en-US": locale},
	}}}
	got := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 1 || got[0].Path != "/spec/metadata/locales/en-US/supportUrl" {
		t.Errorf("whitespace supportUrl should fail: %+v", got)
	}
}

func TestLocalizationsCompleteness_DeterministicOrder(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata:    &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": completeMetadataLocale(), "fr-FR": completeMetadataLocale(), "de-DE": completeMetadataLocale()}},
		Screenshots: &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {}}},
	}}
	first := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	second := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(first) != len(second) {
		t.Fatalf("len differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Errorf("nondeterministic order at %d: %q vs %q", i, first[i].Path, second[i].Path)
		}
	}
}
