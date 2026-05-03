package lint

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
)

// iapReviewScreenshotExistsRule fires when an IAP product has no App Store
// review screenshot attached. Apple requires a review screenshot for any
// IAP submitted for review; missing it is a hard rejection cause.
//
// Live-only: the screenshot lives on the
// /v2/inAppPurchases/{id}/appStoreReviewScreenshot relationship and only
// the live API knows whether it's present.
type iapReviewScreenshotExistsRule struct{}

func init() { Register(iapReviewScreenshotExistsRule{}) }

func (iapReviewScreenshotExistsRule) ID() string         { return "iap.review-screenshot-exists" }
func (iapReviewScreenshotExistsRule) Severity() Severity { return SeverityError }
func (iapReviewScreenshotExistsRule) Mode() Mode         { return ModeLive }

func (r iapReviewScreenshotExistsRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("resolve app for bundleId %q: %v", ctx.BundleID, err),
			FixHint:  "verify the bundle id and that the API key has access to it.",
		}}
	}

	iaps, err := iapListForApp(ctx, appID)
	if err != nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("list IAPs for %s: %v", ctx.BundleID, err),
		}}
	}

	out := make([]Diagnostic, 0, len(iaps))
	for _, iap := range iaps {
		// Skip states that aren't going to review yet — review screenshot is
		// only required at submission time.
		if !needsReviewScreenshot(iap.Attributes.State) {
			continue
		}
		hasShot, ferr := r.hasReviewScreenshot(ctx, iap.ID)
		if ferr != nil {
			out = append(out, Diagnostic{
				RuleID:   r.ID(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("could not verify review screenshot for IAP %q: %v", iap.Attributes.ProductID, ferr),
				Path:     "/spec/iap/products/" + iap.Attributes.ProductID,
				FixHint:  "retry; if the error persists check ASC API rate-limit headroom.",
			})
			continue
		}
		if hasShot {
			continue
		}
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("IAP %q has no App Store review screenshot attached", iap.Attributes.ProductID),
			Path:     "/spec/iap/products/" + iap.Attributes.ProductID + "/reviewScreenshot",
			FixHint: fmt.Sprintf(
				"upload one: `fline iap review-screenshot upload %s --product %s <file>`",
				ctx.BundleID, iap.Attributes.ProductID,
			),
			Reference: "PRD §L3 — IAP review-screenshot-exists",
		})
	}
	return out
}

// needsReviewScreenshot returns true for states where Apple requires a
// review screenshot at submission. MISSING_METADATA / WAITING_FOR_UPLOAD
// are pre-submission states; APPROVED and friends are post-review.
func needsReviewScreenshot(state string) bool {
	switch state {
	case asc.IAPStateReadyToSubmit,
		asc.IAPStateWaitingForReview,
		asc.IAPStateInReview,
		asc.IAPStateDeveloperActionNeeded,
		asc.IAPStatePendingBinaryApproval:
		return true
	default:
		return false
	}
}

// hasReviewScreenshot calls the relationship endpoint and returns whether
// the screenshot exists with a non-empty templated URL. Apple returns
// `data: null` when no screenshot is attached (200 OK), and a 404 when the
// relationship has never been touched. Both map to "no screenshot".
func (iapReviewScreenshotExistsRule) hasReviewScreenshot(ctx CheckContext, iapID string) (bool, error) {
	resp, err := asc.Get[asc.Single[asc.IAPReviewScreenshotAttributes]](
		ctx.Ctx, ctx.Client, "/v2/inAppPurchases/"+iapID+"/appStoreReviewScreenshot", url.Values{},
	)
	if err != nil {
		// 404 surfaces from the asc client as a typed error; treat as no screenshot.
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NOT_FOUND") {
			return false, nil
		}
		return false, err
	}
	return resp.Data.Attributes.ImageAsset.TemplateURL != "" || resp.Data.Attributes.FileName != "", nil
}
