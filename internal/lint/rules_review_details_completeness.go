package lint

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
)

// reviewDetailsCompletenessRule warns when an IAP-bearing app submits with empty App Review notes.
// Empty notes invite "Information Needed" rejections asking for the purchase flow and demo credentials.
type reviewDetailsCompletenessRule struct{}

func init() { Register(reviewDetailsCompletenessRule{}) }

func (reviewDetailsCompletenessRule) ID() string         { return "review-details.completeness" }
func (reviewDetailsCompletenessRule) Severity() Severity { return SeverityWarning }
func (reviewDetailsCompletenessRule) Mode() Mode         { return ModeLive }
func (reviewDetailsCompletenessRule) Doc() string {
	return "Warns when an app that sells in-app purchases submits a version whose App Review notes are empty, and escalates to an error once an IAP is attached to the review submission. " +
		"Reviewers who cannot see the purchase flow respond with an Information Needed rejection asking for it — and for demo credentials the app may not even have; trial-gated paywalls are the top reason reviewers cannot find an IAP. " +
		"Fix it by writing notes that give the exact steps to reach the purchase, describe any trial mechanics, and state explicitly when the app has no accounts or sign-in."
}

func (r reviewDetailsCompletenessRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{r.warn("resolve app for bundleId "+ctx.BundleID+": "+err.Error(),
			"verify the bundle id and that the API key has access to it.")}
	}
	iaps, err := iapListForApp(ctx, appID)
	if err != nil {
		return []Diagnostic{r.warn("list IAPs for "+ctx.BundleID+": "+err.Error(),
			"check ASC API access and rate-limit headroom; retry after a minute.")}
	}
	if len(iaps) == 0 {
		return nil
	}

	versionID, err := resolveVersionIDOnApp(ctx, appID, ctx.Version)
	if err != nil {
		return []Diagnostic{r.warn("resolve version for "+ctx.BundleID+": "+err.Error(),
			"verify the version string exists on the app.")}
	}
	notes, err := fetchReviewNotes(ctx, versionID)
	if err != nil {
		return []Diagnostic{r.warn("fetch app review detail: "+err.Error(),
			"check ASC API access; the appStoreReviewDetail endpoint is version-scoped.")}
	}
	if strings.TrimSpace(notes) != "" {
		return nil
	}

	// Escalates once an IAP is in the submission: the reviewer will go looking for the purchase.
	severity := SeverityWarning
	message := fmt.Sprintf(
		"app sells %d in-app purchase(s) but the App Review notes are empty; reviewers will ask for the purchase flow",
		len(iaps),
	)
	if r.iapInOpenSubmission(ctx, appID) {
		severity = SeverityError
		message = "an IAP is in the review submission but the App Review notes are empty; include the exact steps to reach the purchase (trial-gated paywalls are the #1 cause reviewers can't find an IAP)"
	}

	return []Diagnostic{{
		RuleID:    r.ID(),
		Severity:  severity,
		Message:   message,
		Path:      "/spec/review-details/notes",
		FixHint:   fmt.Sprintf("set exact purchase steps and trial behavior with `flightline reviewer-demo set %s --version %s --notes \"<steps>\"`; state explicitly when the app has no accounts or sign-in", ctx.BundleID, ctx.Version),
		Reference: "Guideline 2.1(b) App Completeness; rejection corpus: NetProbe 2026-03-13",
	}}
}

// iapInOpenSubmission reports whether the latest submission carries an IAP item; lookup failures stay false (warning severity).
func (reviewDetailsCompletenessRule) iapInOpenSubmission(ctx CheckContext, appID string) bool {
	subID, err := iapLatestSubmissionID(ctx, appID)
	if err != nil || subID == "" {
		return false
	}
	refs, err := iapSubmissionItemReferences(ctx, subID)
	if err != nil {
		return false
	}
	for _, ref := range refs {
		if ref.Type == "inAppPurchaseV2" || ref.Type == "inAppPurchase" ||
			ref.Type == "inAppPurchases" || ref.Type == "inAppPurchaseVersions" {
			return true
		}
	}
	return false
}

func (r reviewDetailsCompletenessRule) warn(msg, fix string) Diagnostic {
	return Diagnostic{RuleID: r.ID(), Severity: SeverityWarning, Message: msg, FixHint: fix}
}

// fetchReviewNotes returns the version's review notes; a 404 means the detail was never created (empty).
func fetchReviewNotes(ctx CheckContext, versionID string) (string, error) {
	resp, err := asc.Get[asc.Single[asc.AppStoreReviewDetailAttributes]](
		ctx.Ctx, ctx.Client, "/v1/appStoreVersions/"+versionID+"/appStoreReviewDetail", nil,
	)
	if err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return "", nil
		}
		return "", err
	}
	if resp.Data.Attributes.Notes == nil {
		return "", nil
	}
	return *resp.Data.Attributes.Notes, nil
}
