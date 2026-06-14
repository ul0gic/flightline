package lint

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
)

// iapPromotionalImageDistinctRule fires when an IAP review screenshot shares a checksum with an app screenshot (Guideline 2.3.2 hard rejection).
// Uses /v2/inAppPurchases/{id}/appStoreReviewScreenshot: promotional-artwork hashes are not yet in the public API. Live-only.
type iapPromotionalImageDistinctRule struct{}

func init() { Register(iapPromotionalImageDistinctRule{}) }

func (iapPromotionalImageDistinctRule) ID() string         { return "iap.promotional-image-distinct" }
func (iapPromotionalImageDistinctRule) Severity() Severity { return SeverityError }
func (iapPromotionalImageDistinctRule) Mode() Mode         { return ModeLive }
func (iapPromotionalImageDistinctRule) Doc() string {
	return "Checks that an IAP review screenshot does not reuse one of the app's store screenshots, comparing source file checksums. " +
		"Apple Guideline 2.3.2 hard-rejects reusing a store screenshot as IAP promotional art, and developers who drag the same image into both fields to save time hit this. " +
		"Fix it by supplying a distinct image that specifically represents the IAP purchase rather than the app in general."
}

func (r iapPromotionalImageDistinctRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("resolve app for bundleId %q: %v", ctx.BundleID, err),
		}}
	}

	appHashes, err := r.collectAppScreenshotHashes(ctx, appID)
	if err != nil {
		// Downgrade to warning: screenshot endpoints are deeply nested and occasional 5xx isn't a rejection signal.
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("could not enumerate app screenshots for hash compare: %v", err),
			FixHint:  "rerun preflight; if it persists check ASC API rate limits.",
		}}
	}
	if len(appHashes) == 0 {
		return nil // no screenshots to clash with
	}

	iaps, err := iapListForApp(ctx, appID)
	if err != nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("list IAPs for %s: %v", ctx.BundleID, err),
		}}
	}

	out := make([]Diagnostic, 0)
	for _, iap := range iaps {
		hash := r.fetchIAPScreenshotHash(ctx, iap.ID)
		if hash == "" {
			continue
		}
		if _, clash := appHashes[hash]; !clash {
			continue
		}
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"IAP %q review screenshot reuses an app screenshot (sourceFileChecksum %s)",
				iap.Attributes.ProductID, hash,
			),
			Path: "/spec/iap/products/" + iap.Attributes.ProductID + "/reviewScreenshot",
			FixHint: "use a unique image for the IAP; reusing a store screenshot violates " +
				"Guideline 2.3.2.",
			Reference: "Apple Guideline 2.3.2",
		})
	}
	return out
}

// collectAppScreenshotHashes returns all sourceFileChecksum values across every locale. Skips empty (in-flight uploads).
func (iapPromotionalImageDistinctRule) collectAppScreenshotHashes(ctx CheckContext, appID string) (map[string]struct{}, error) {
	versions, err := iapAppVersions(ctx, appID)
	if err != nil {
		return nil, err
	}
	hashes := map[string]struct{}{}
	for _, ver := range versions {
		locs, lerr := iapVersionLocalizations(ctx, ver.ID)
		if lerr != nil {
			continue
		}
		for _, loc := range locs {
			sets, serr := iapLocalizationSets(ctx, loc.ID)
			if serr != nil {
				continue
			}
			for _, set := range sets {
				addSetHashes(ctx, set.ID, hashes)
			}
		}
	}
	return hashes, nil
}

func addSetHashes(ctx CheckContext, setID string, hashes map[string]struct{}) {
	type shotAttrs struct {
		SourceFileChecksum string `json:"sourceFileChecksum,omitempty"`
	}
	q := url.Values{"limit": {"50"}}
	resp, err := asc.Get[asc.Collection[shotAttrs]](
		ctx.Ctx, ctx.Client, "/v1/appScreenshotSets/"+setID+"/appScreenshots", q,
	)
	if err != nil {
		return
	}
	for _, s := range resp.Data {
		if s.Attributes.SourceFileChecksum != "" {
			hashes[s.Attributes.SourceFileChecksum] = struct{}{}
		}
	}
}

// fetchIAPScreenshotHash returns the IAP review screenshot's sourceFileChecksum or "" when absent.
func (iapPromotionalImageDistinctRule) fetchIAPScreenshotHash(ctx CheckContext, iapID string) string {
	resp, err := asc.Get[asc.Single[asc.IAPReviewScreenshotAttributes]](
		ctx.Ctx, ctx.Client, "/v2/inAppPurchases/"+iapID+"/appStoreReviewScreenshot", url.Values{},
	)
	if err != nil {
		// 404 / no-screenshot is not a rule violation here.
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NOT_FOUND") {
			return ""
		}
		return ""
	}
	return resp.Data.Attributes.SourceFileChecksum
}

type idOnly struct{ ID string }

func iapAppVersions(ctx CheckContext, appID string) ([]idOnly, error) {
	type ver struct{}
	q := url.Values{"limit": {"50"}}
	out := make([]idOnly, 0, 4)
	for page, err := range asc.Pages[ver](ctx.Ctx, ctx.Client, "/v1/apps/"+appID+"/appStoreVersions", q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, idOnly{ID: r.ID})
		}
	}
	return out, nil
}

func iapVersionLocalizations(ctx CheckContext, versionID string) ([]idOnly, error) {
	type loc struct{}
	q := url.Values{"limit": {"50"}}
	out := make([]idOnly, 0, 8)
	for page, err := range asc.Pages[loc](ctx.Ctx, ctx.Client, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations", q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, idOnly{ID: r.ID})
		}
	}
	return out, nil
}

func iapLocalizationSets(ctx CheckContext, locID string) ([]idOnly, error) {
	type set struct{}
	q := url.Values{"limit": {"50"}}
	out := make([]idOnly, 0, 8)
	for page, err := range asc.Pages[set](ctx.Ctx, ctx.Client, "/v1/appStoreVersionLocalizations/"+locID+"/appScreenshotSets", q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, idOnly{ID: r.ID})
		}
	}
	return out, nil
}
