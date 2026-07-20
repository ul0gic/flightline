package lint

import (
	"fmt"
	"net/url"
	"sort"

	"github.com/ul0gic/flightline/internal/asc"
)

// screenshotsRequiredDevicesRule fires when a locale has no screenshot set from the large-iPhone tier.
// Apple requires 6.9" screenshots UNLESS 6.5" is provided; any one tier member satisfies submission. Mode=Both.
type screenshotsRequiredDevicesRule struct{}

func init() { Register(screenshotsRequiredDevicesRule{}) }

func (screenshotsRequiredDevicesRule) ID() string         { return "screenshots.required-devices" }
func (screenshotsRequiredDevicesRule) Severity() Severity { return SeverityError }
func (screenshotsRequiredDevicesRule) Mode() Mode         { return ModeBoth }
func (screenshotsRequiredDevicesRule) Doc() string {
	return "Checks that every locale has at least one screenshot set from the large-iPhone tier Apple accepts for submission: 6.9 inch, 6.7 inch, or 6.5 inch. " +
		"Apple requires the 6.9-inch size unless a 6.5-inch set is provided, and scales the largest set you supply down to smaller displays — so any one tier member unblocks Submit for Review. " +
		"Fix it by uploading screenshots for one of the accepted device classes in each affected locale; 6.9 inch gives the best scaled quality."
}

// acceptedLargeIPhoneTier: any one satisfies Apple's requirement (6.9" required unless 6.5" provided; 6.7" counts as the large class).
var acceptedLargeIPhoneTier = []string{"APP_IPHONE_69", "APP_IPHONE_67", "APP_IPHONE_65"}

func hasLargeIPhoneSet(devices map[string]struct{}) bool {
	for _, dev := range acceptedLargeIPhoneTier {
		if _, ok := devices[dev]; ok {
			return true
		}
	}
	return false
}

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
		present := make(map[string]struct{}, len(sc.Locales[locale]))
		for dev, files := range sc.Locales[locale] {
			if len(files) > 0 {
				present[dev] = struct{}{}
			}
		}
		if hasLargeIPhoneSet(present) {
			continue
		}
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("locale %q has no large-iPhone screenshot set (need one of APP_IPHONE_69, APP_IPHONE_67, APP_IPHONE_65)", locale),
			Path:     "/spec/screenshots/locales/" + locale,
			FixHint: fmt.Sprintf(
				"add screenshots for one accepted device class to spec.screenshots.locales.%s; APP_IPHONE_69 scales best.",
				locale,
			),
			Reference: publicRuleReference(r.ID()),
		})
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
		if hasLargeIPhoneSet(devices) {
			continue
		}
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("locale %q has no live large-iPhone screenshot set (need one of APP_IPHONE_69, APP_IPHONE_67, APP_IPHONE_65)", loc.Attributes.Locale),
			Path:     "/spec/screenshots/locales/" + loc.Attributes.Locale,
			FixHint: fmt.Sprintf(
				"upload screenshots for one accepted device class: `flightline screenshots upload %s --version %s --locale %s --device-set APP_IPHONE_69 <files...>`",
				ctx.BundleID, ctx.Version, loc.Attributes.Locale,
			),
			Reference: publicRuleReference(r.ID()),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// fetchLocaleDevices returns the screenshotDisplayType set for a version-localization; nil means no sets.
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

// sortedKeys returns map keys sorted for stable iteration.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
