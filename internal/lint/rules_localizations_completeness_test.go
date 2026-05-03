package lint

import (
	"context"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestLocalizationsCompleteness_NoOpWhenAllSurfacesMatch(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata:    &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": {}, "fr-FR": {}}},
		Screenshots: &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {}, "fr-FR": {}}},
	}}
	got := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0: %+v", len(got), got)
	}
}

func TestLocalizationsCompleteness_FiresWhenLocaleOnlyInMetadata(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata:    &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": {}, "fr-FR": {}}},
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

func TestLocalizationsCompleteness_NoOpWhenSingleSurface(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata: &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": {}}},
	}}
	got := localizationsCompletenessRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 with single surface: %+v", len(got), got)
	}
}

func TestLocalizationsCompleteness_DeterministicOrder(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Metadata:    &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": {}, "fr-FR": {}, "de-DE": {}}},
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
