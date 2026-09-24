package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF6C_DiffScreenshotOrderIsExplicitAndPreservesDefaultUnorderedCollections(t *testing.T) {
	ordered := true
	desired := &config.State{Spec: config.StateSpec{Screenshots: &config.ScreenshotsSpec{
		Order: &ordered,
		Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {
			"APP_IPHONE_67": {{Path: "02.png", SourceFileChecksum: "two"}, {Path: "01.png", SourceFileChecksum: "one"}},
		}},
	}}}
	live := &config.State{Spec: config.StateSpec{Screenshots: &config.ScreenshotsSpec{
		Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {
			"APP_IPHONE_67": {{Path: "01.png", SourceFileChecksum: "one"}, {Path: "02.png", SourceFileChecksum: "two"}},
		}},
	}}}
	var changes []Change
	diffScreenshotOrder(desired, live, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/screenshots/locales/en-US/APP_IPHONE_67/order" || changes[0].Op != OpUpdate {
		t.Fatalf("order changes = %#v", changes)
	}
	if got, ok := changes[0].To.([]config.ScreenshotFile); !ok || len(got) != 2 || got[0].SourceFileChecksum != "two" || got[1].SourceFileChecksum != "one" {
		t.Fatalf("declared order was not preserved: %#v", got)
	}

	desired.Spec.Screenshots.Order = nil
	changes = nil
	diffScreenshotOrder(desired, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("unordered screenshot collection emitted order change: %#v", changes)
	}
}

func TestF6C_DiffScreenshotOrderEmitsCPPOrderAfterCollectionReconcile(t *testing.T) {
	ordered := true
	desired := &config.State{Spec: config.StateSpec{CustomProductPages: &config.CustomProductPagesSpec{
		"promo": {Localizations: map[string]config.CustomProductPageLocale{"en-US": {
			ScreenshotOrder: &ordered,
			Screenshots: map[string][]config.ScreenshotFile{"APP_IPHONE_67": {
				{Path: "02.png", SourceFileChecksum: "two"}, {Path: "01.png", SourceFileChecksum: "one"},
			}},
		}}},
	}}}
	var changes []Change
	diffScreenshotOrder(desired, &config.State{}, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/customProductPages/promo/localizations/en-US/screenshots/APP_IPHONE_67/order" {
		t.Fatalf("CPP order changes = %#v", changes)
	}
}

func TestF6C_DiffScreenshotOrderEscapesCPPPagePointerToken(t *testing.T) {
	ordered := true
	name := "campaign/summer~2026"
	desired := &config.State{Spec: config.StateSpec{
		CustomProductPages: &config.CustomProductPagesSpec{
			name: {Localizations: map[string]config.CustomProductPageLocale{"en-US": {
				ScreenshotOrder: &ordered,
				Screenshots:     map[string][]config.ScreenshotFile{"APP_IPHONE_67": {{Path: "01.png", SourceFileChecksum: "one"}}},
			}}},
		},
	}}
	var changes []Change
	diffScreenshotOrder(desired, &config.State{}, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/customProductPages/campaign~1summer~02026/localizations/en-US/screenshots/APP_IPHONE_67/order" {
		t.Fatalf("escaped CPP order change = %#v", changes)
	}
}

func TestG6_DiffFrozenRightsAndPreviewsNoop(t *testing.T) {
	text := "Terms"
	territories := []string{"USA"}
	preview := config.PreviewFile{Path: "trailer.mov", SourceFileChecksum: "checksum"}
	state := &config.State{Spec: config.StateSpec{
		AppEULA:  &config.AppEULASpec{AgreementText: &text, Territories: &territories},
		Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {preview}}}},
	}}
	if changes := Diff(state, state); len(changes) != 0 {
		t.Fatalf("identical frozen asset/rights state diffed: %#v", changes)
	}
}
