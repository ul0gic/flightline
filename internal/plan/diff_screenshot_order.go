package plan

import (
	"fmt"

	"github.com/ul0gic/flightline/internal/config"
)

// diffScreenshotOrder emits opt-in relationship-order changes. Existing
// screenshot collection diffs deliberately remain order-independent: Order is
// the explicit switch that turns each declared file sequence into desired
// complete linkage order after its collection reconciliation has finished.
func diffScreenshotOrder(desired, live *config.State, out *[]Change) {
	if desired == nil {
		return
	}

	diffMainScreenshotOrder(desired.Spec.Screenshots, liveScreenshots(live), out)
	diffCustomProductPageScreenshotOrder(derefCPP(desired.Spec.CustomProductPages), liveCustomProductPages(live), out)
}

func liveScreenshots(live *config.State) *config.ScreenshotsSpec {
	if live == nil {
		return nil
	}
	return live.Spec.Screenshots
}

func liveCustomProductPages(live *config.State) config.CustomProductPagesSpec {
	if live == nil {
		return nil
	}
	return derefCPP(live.Spec.CustomProductPages)
}

func diffMainScreenshotOrder(desired, live *config.ScreenshotsSpec, out *[]Change) {
	if desired == nil || desired.Order == nil || !*desired.Order {
		return
	}

	liveLocales := map[string]map[string][]config.ScreenshotFile{}
	if live != nil {
		liveLocales = live.Locales
	}
	for _, locale := range sortedKeys(desired.Locales) {
		for _, deviceSet := range sortedKeys(desired.Locales[locale]) {
			path := "/spec/screenshots/locales/" + locale + "/" + deviceSet
			appendScreenshotOrderChange(out, "screenshots."+locale+"."+deviceSet, path,
				desired.Locales[locale][deviceSet], liveLocales[locale][deviceSet], "screenshots")
		}
	}
}

func diffCustomProductPageScreenshotOrder(desired, live config.CustomProductPagesSpec, out *[]Change) {
	for _, name := range sortedKeys(desired) {
		page := desired[name]
		livePage := live[name]
		for _, locale := range sortedKeys(page.Localizations) {
			localization := page.Localizations[locale]
			if localization.ScreenshotOrder == nil || !*localization.ScreenshotOrder {
				continue
			}
			liveLocalization := livePage.Localizations[locale]
			for _, deviceSet := range sortedKeys(localization.Screenshots) {
				path := "/spec/customProductPages/" + escapeJSONPointerToken(name) + "/localizations/" + locale + "/screenshots/" + deviceSet
				resource := "customProductPages." + name + ".loc." + locale + ".screenshots"
				appendScreenshotOrderChange(out, resource, path,
					localization.Screenshots[deviceSet], liveLocalization.Screenshots[deviceSet], "CPP "+name)
			}
		}
	}
}

func appendScreenshotOrderChange(out *[]Change, resource, collectionPath string, desired, live []config.ScreenshotFile, target string) {
	if len(desired) == 0 || equalOrderedScreenshotFiles(desired, live) {
		return
	}
	*out = append(*out, Change{
		Op:       OpUpdate,
		Resource: resource,
		Path:     collectionPath + "/order",
		From:     live,
		To:       desired,
		Hint:     fmt.Sprintf("reorder %d screenshot(s) for %s", len(desired), target),
	})
}

func equalOrderedScreenshotFiles(desired, live []config.ScreenshotFile) bool {
	if len(desired) != len(live) {
		return false
	}
	for i := range desired {
		if screenshotIdentity(desired[i]) != screenshotIdentity(live[i]) {
			return false
		}
	}
	return true
}
