package lint

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
)

// iapAttachedToReviewSubmissionRule fires when a READY_TO_SUBMIT IAP is missing from the active review submission.
// The #1 IAP rejection: "ready" is not "submitted": Apple rejects or approves without the IAP live. Live-only.
type iapAttachedToReviewSubmissionRule struct{}

func init() { Register(iapAttachedToReviewSubmissionRule{}) }

func (iapAttachedToReviewSubmissionRule) ID() string         { return "iap.attached-to-review-submission" }
func (iapAttachedToReviewSubmissionRule) Severity() Severity { return SeverityError }
func (iapAttachedToReviewSubmissionRule) Mode() Mode         { return ModeLive }
func (iapAttachedToReviewSubmissionRule) Doc() string {
	return "Checks that every in-app purchase marked READY_TO_SUBMIT also appears in the app's open review submission. " +
		"This is the most common IAP rejection cause: developers mark an IAP ready and assume that means submitted, so the app goes through review without it and the IAP never goes live. " +
		"Fix it by attaching the IAP to the review submission, or by creating a submission first if none is open."
}

func (r iapAttachedToReviewSubmissionRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve app for bundleId "+ctx.BundleID, err,
			"verify the bundle id and that the API key has access to it.")}
	}

	// Only READY_TO_SUBMIT IAPs need attachment; other states are pre-submission or already in-flight.
	ready, err := r.readyToSubmitIAPs(ctx, appID)
	if err != nil {
		return []Diagnostic{r.fetchErr("list IAPs for "+ctx.BundleID, err,
			"check ASC API access and rate-limit headroom; retry after a minute.")}
	}
	if len(ready) == 0 {
		return nil
	}

	// Pick the most recent in-flight submission; historical ones can't pull a new IAP into review.
	subID, err := iapLatestSubmissionID(ctx, appID)
	if err != nil {
		return []Diagnostic{r.fetchErr("list review submissions for "+ctx.BundleID, err,
			"check ASC API access; the review-submissions endpoint requires filter[app].")}
	}
	if subID == "" {
		return iapUnattachedDiagnostics(r.ID(), ctx.BundleID, ready, "no open review submission found for this app")
	}

	itemRefs, err := iapSubmissionItemReferences(ctx, subID)
	if err != nil {
		return []Diagnostic{r.fetchErr("list items for submission "+subID, err,
			"verify the submission id; Apple rotates submission ids when state cycles.")}
	}
	diagnostics, err := r.unattachedDiagnostics(ctx, ctx.BundleID, subID, ready, itemRefs)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve IAP versions for submission "+subID, err,
			"check ASC API access and retry; Flightline will not guess whether an IAP is attached.")}
	}
	return diagnostics
}

// readyToSubmitIAPs returns only READY_TO_SUBMIT IAPs for the app.
func (iapAttachedToReviewSubmissionRule) readyToSubmitIAPs(ctx CheckContext, appID string) ([]asc.Resource[asc.IAPAttributes], error) {
	iaps, err := iapListForApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]asc.Resource[asc.IAPAttributes], 0, len(iaps))
	for _, iap := range iaps {
		if iap.Attributes.State == asc.IAPStateReadyToSubmit {
			out = append(out, iap)
		}
	}
	return out, nil
}

// unattachedDiagnostics emits one diagnostic per READY_TO_SUBMIT IAP absent from the submission's item-reference set.
func (r iapAttachedToReviewSubmissionRule) unattachedDiagnostics(ctx CheckContext, bundleID, subID string, ready []asc.Resource[asc.IAPAttributes], itemRefs []submissionItemRef) ([]Diagnostic, error) {
	attachments := classifyIAPItemReferences(itemRefs)
	out := make([]Diagnostic, 0, len(ready))
	for _, iap := range ready {
		if attachments.iaps[iap.ID] {
			continue
		}
		attached, err := iapVersionAttached(ctx, iap.ID, attachments.versions)
		if err != nil {
			return nil, fmt.Errorf("IAP %s: %w", iap.Attributes.ProductID, err)
		}
		if attached {
			continue
		}
		if attachments.hasOpaque {
			out = append(out, Diagnostic{
				RuleID:   r.ID(),
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"could not verify whether IAP %q is attached to review submission %s because it contains an unknown or malformed item",
					iap.Attributes.ProductID, subID,
				),
				Path: "/spec/iap/products/" + iap.Attributes.ProductID,
				FixHint: fmt.Sprintf(
					"inspect the submission with `flightline review-submissions items %s --submission %s`; do not submit until every item is identified.",
					bundleID, subID,
				),
				Reference: publicRuleReference(r.ID()),
			})
			continue
		}
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"IAP %q is READY_TO_SUBMIT but not in review submission %s",
				iap.Attributes.ProductID, subID,
			),
			Path: "/spec/iap/products/" + iap.Attributes.ProductID,
			FixHint: fmt.Sprintf(
				"add the IAP to the submission: `flightline review-submissions items %s --submission %s` to inspect, then attach via App Store Connect or the submissions write surface.",
				bundleID, subID,
			),
			Reference: publicRuleReference(r.ID()),
		})
	}
	return out, nil
}

type iapItemAttachments struct {
	iaps      map[string]bool
	versions  map[string]bool
	hasOpaque bool
}

func classifyIAPItemReferences(itemRefs []submissionItemRef) iapItemAttachments {
	attachments := iapItemAttachments{
		iaps:     make(map[string]bool, len(itemRefs)),
		versions: make(map[string]bool, len(itemRefs)),
	}
	for _, ref := range itemRefs {
		switch ref.Type {
		case "inAppPurchaseV2", "inAppPurchase", "inAppPurchases":
			attachments.iaps[ref.ID] = true
		case "inAppPurchaseVersions":
			if ref.Canonical {
				attachments.versions[ref.ID] = true
			}
		}
		attachments.hasOpaque = attachments.hasOpaque || ref.Opaque
	}
	return attachments
}

func iapVersionAttached(ctx CheckContext, iapID string, attachedVersions map[string]bool) (bool, error) {
	versions, err := iapVersionsForIAP(ctx, iapID)
	if err != nil {
		return false, err
	}
	for _, version := range versions {
		if attachedVersions[version.ID] {
			return true, nil
		}
	}
	return false, nil
}

func (r iapAttachedToReviewSubmissionRule) fetchErr(what string, err error, fix string) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: %v", what, err),
		FixHint:  fix,
	}
}

// iapResolveAppID wraps the apps filter. Lint is a peer package: no imports from cmd or state.
func iapResolveAppID(ctx CheckContext, bundleID string) (string, error) {
	type appAttrs struct {
		BundleID string `json:"bundleId,omitempty"`
	}
	q := url.Values{
		"filter[bundleId]": {bundleID},
		"limit":            {"1"},
	}
	page, err := asc.Get[asc.Collection[appAttrs]](ctx.Ctx, ctx.Client, "/v1/apps", q)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", errors.New("no app found")
	}
	return page.Data[0].ID, nil
}

func iapListForApp(ctx CheckContext, appID string) ([]asc.Resource[asc.IAPAttributes], error) {
	q := url.Values{"limit": {"200"}}
	out := make([]asc.Resource[asc.IAPAttributes], 0, 16)
	for page, err := range asc.Pages[asc.IAPAttributes](ctx.Ctx, ctx.Client, "/v1/apps/"+appID+"/inAppPurchasesV2", q) {
		if err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
	}
	return out, nil
}

func iapVersionsForIAP(ctx CheckContext, iapID string) ([]asc.Resource[asc.IAPVersionAttributes], error) {
	q := url.Values{"limit": {"200"}}
	out := make([]asc.Resource[asc.IAPVersionAttributes], 0, 2)
	for page, err := range asc.Pages[asc.IAPVersionAttributes](
		ctx.Ctx, ctx.Client, "/v2/inAppPurchases/"+iapID+"/versions", q,
	) {
		if err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
	}
	return out, nil
}

// iapLatestSubmissionID picks the highest-priority in-flight submission (prefers WAITING/IN_REVIEW over completed).
// Returns "" when there are no submissions.
func iapLatestSubmissionID(ctx CheckContext, appID string) (string, error) {
	q := url.Values{
		"filter[app]": {appID},
		"limit":       {"50"},
	}
	page, err := asc.Get[asc.Collection[asc.ReviewSubmissionAttributes]](
		ctx.Ctx, ctx.Client, "/v1/reviewSubmissions", q,
	)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", nil
	}
	priority := map[string]int{
		asc.ReviewSubmissionStateReadyForReview:   3,
		asc.ReviewSubmissionStateWaitingForReview: 4,
		asc.ReviewSubmissionStateInReview:         5,
		asc.ReviewSubmissionStateCompleting:       2,
	}
	bestID := page.Data[0].ID
	bestRank := priority[page.Data[0].Attributes.State]
	for _, r := range page.Data[1:] {
		if priority[r.Attributes.State] > bestRank {
			bestID = r.ID
			bestRank = priority[r.Attributes.State]
		}
	}
	return bestID, nil
}

// iapSubmissionItemReferences resolves relationships and encoded item IDs.
func iapSubmissionItemReferences(ctx CheckContext, subID string) ([]submissionItemRef, error) {
	q := url.Values{"limit": {"200"}}
	out := make([]submissionItemRef, 0, 16)
	for page, err := range asc.Pages[asc.ReviewSubmissionItemAttributes](
		ctx.Ctx, ctx.Client, "/v1/reviewSubmissions/"+subID+"/items", q,
	) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, asc.ResolveReviewSubmissionItemReference(r.ID, subID, r.Relationships))
		}
	}
	return out, nil
}

type submissionItemRef = asc.ReviewSubmissionItemReference

func iapUnattachedDiagnostics(ruleID, bundleID string, ready []asc.Resource[asc.IAPAttributes], reason string) []Diagnostic {
	out := make([]Diagnostic, 0, len(ready))
	for _, iap := range ready {
		out = append(out, Diagnostic{
			RuleID:   ruleID,
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"IAP %q is READY_TO_SUBMIT but %s",
				iap.Attributes.ProductID, reason,
			),
			Path: "/spec/iap/products/" + iap.Attributes.ProductID,
			FixHint: fmt.Sprintf("create or open a review submission and attach IAP %s; `%s` shows current submissions", iap.Attributes.ProductID,
				"flightline review-submissions list "+bundleID),
			Reference: publicRuleReference(ruleID),
		})
	}
	return out
}
