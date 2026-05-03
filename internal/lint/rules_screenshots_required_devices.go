package lint

import (
	"fmt"
	"net/url"
	"sort"

	"github.com/ul0gic/flightline/internal/asc"
)

// screenshotsRequiredDevicesRule fires when a locale is missing one of the
// device classes Apple requires for new submissions: 6.9" iPhone
// (APP_IPHONE_69) and 6.7" iPhone (APP_IPHONE_67). Apple's submission flow
// blocks Submit-for-Review until both are present per locale.
//
// Mode=Both. Offline scans the spec's locales/devices map. Live re-fetches
// the screenshot sets per locale and re-applies the same check.
type screenshotsRequiredDevicesRule struct{}

func init() { Register(screenshotsRequiredDevicesRule{}) }

func (screenshotsRequiredDevicesRule) ID() string         { return "screenshots.required-devices" }
func (screenshotsRequiredDevicesRule) Severity() Severity { return SeverityError }
func (screenshotsRequiredDevicesRule) Mode() Mode         { return ModeBoth }

// requiredDevices is the v1 set. Apple's required-device list rotates with
// device launches; this list is conservative — both 6.9 and 6.7 are
// currently required for new iPhone submissions.
var requiredDevices = []string{"APP_IPHONE_69", "APP_IPHONE_67"}

func (r screenshotsRequiredDevicesRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.Live {
		return r.checkLive(ctx)
	}
	return r.checkOffline(ctx)
}

func (r screenshotsRequiredDevicesRule) checkOffline(ctx CheckContext) []Diagnostic {
	if ctx.State == nil {
		return nil
	}
	sc := ctx.State.Spec.Screenshots
	if sc == nil || len(sc.Locales) == 0 {
		return nil // not managed
	}
	out := make([]Diagnostic, 0)
	locales := sortedKeys(sc.Locales)
	for _, locale := range locales {
		devices := sc.Locales[locale]
		for _, dev := range requiredDevices {
			files, ok := devices[dev]
			if !ok || len(files) == 0 {
				out = append(out, Diagnostic{
					RuleID:   r.ID(),
					Severity: SeverityError,
					Message:  fmt.Sprintf("locale %q is missing required device %s", locale, dev),
					Path:     "/spec/screenshots/locales/" + locale + "/" + dev,
					FixHint: fmt.Sprintf(
						"add at least one screenshot for the %s device class to spec.screenshots.locales.%s.",
						dev, locale,
					),
					Reference: "PRD §L3 — screenshots.required-devices",
				})
			}
		}
	}
	return out
}

func (r screenshotsRequiredDevicesRule) checkLive(ctx CheckContext) []Diagnostic {
	if ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}
	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve app", err)}
	}
	versionID, err := resolveVersionIDOnApp(ctx, appID, ctx.Version)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve version", err)}
	}

	type locAttrs struct {
		Locale string `json:"locale,omitempty"`
	}
	locResp, err := asc.Get[asc.Collection[locAttrs]](
		ctx.Ctx, ctx.Client, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations",
		url.Values{"limit": {"50"}},
	)
	if err != nil {
		return []Diagnostic{r.fetchErr("list version localizations", err)}
	}
	out := make([]Diagnostic, 0)
	for _, loc := range locResp.Data {
		devices := r.fetchLocaleDevices(ctx, loc.ID)
		for _, dev := range requiredDevices {
			if _, ok := devices[dev]; !ok {
				out = append(out, Diagnostic{
					RuleID:   r.ID(),
					Severity: SeverityError,
					Message:  fmt.Sprintf("locale %q has no live screenshots for required device %s", loc.Attributes.Locale, dev),
					Path:     "/spec/screenshots/locales/" + loc.Attributes.Locale + "/" + dev,
					FixHint: fmt.Sprintf(
						"upload screenshots for %s in locale %s — `fline screenshots upload <bundleId> --version <v> --locale %s --device-set %s ...`",
						dev, loc.Attributes.Locale, loc.Attributes.Locale, dev,
					),
					Reference: "PRD §L3 — screenshots.required-devices",
				})
			}
		}
	}
	// Stable ordering: locale, then device.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// fetchLocaleDevices returns the set of device classes present for a given
// version-localization. Empty map means no screenshot sets at all.
func (screenshotsRequiredDevicesRule) fetchLocaleDevices(ctx CheckContext, locID string) map[string]struct{} {
	type setAttrs struct {
		ScreenshotDisplayType string `json:"screenshotDisplayType,omitempty"`
	}
	resp, err := asc.Get[asc.Collection[setAttrs]](
		ctx.Ctx, ctx.Client, "/v1/appStoreVersionLocalizations/"+locID+"/appScreenshotSets",
		url.Values{"limit": {"50"}},
	)
	if err != nil {
		return nil
	}
	out := make(map[string]struct{}, len(resp.Data))
	for _, set := range resp.Data {
		if set.Attributes.ScreenshotDisplayType != "" {
			out[set.Attributes.ScreenshotDisplayType] = struct{}{}
		}
	}
	return out
}

func (r screenshotsRequiredDevicesRule) fetchErr(what string, err error) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: %v", what, err),
		FixHint:  "rerun preflight; if it persists check ASC API access.",
	}
}

// sortedKeys is a tiny generic helper for stable iteration over locale maps.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
