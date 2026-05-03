package lint

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ul0gic/skipper/internal/asc"
)

// iapAttachedToReviewSubmissionRule fires when an IAP product is in
// READY_TO_SUBMIT state but is NOT included in the latest review submission's
// items. This is the #1 IAP rejection cause: developers mark the IAP ready,
// assume "ready" means "submitted", and then their app goes through review
// without the IAP attached. Apple either rejects ("IAP referenced but not in
// review") or approves the app with the IAP marked SCHEDULED-but-not-live.
//
// Live-only rule: the check requires reading the IAP list AND the
// reviewSubmissions/items endpoint, which only exist on the wire.
type iapAttachedToReviewSubmissionRule struct{}

func init() { Register(iapAttachedToReviewSubmissionRule{}) }

func (iapAttachedToReviewSubmissionRule) ID() string         { return "iap.attached-to-review-submission" }
func (iapAttachedToReviewSubmissionRule) Severity() Severity { return SeverityError }
func (iapAttachedToReviewSubmissionRule) Mode() Mode         { return ModeLive }

func (r iapAttachedToReviewSubmissionRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve app for bundleId "+ctx.BundleID, err,
			"verify the bundle id and that the API key has access to it.")}
	}

	// READY_TO_SUBMIT IAPs are the candidates. Other states either aren't
	// submittable (MISSING_METADATA, WAITING_FOR_UPLOAD) or are already
	// in-flight (WAITING_FOR_REVIEW, IN_REVIEW). Approved IAPs don't need to
	// be re-attached.
	ready, err := r.readyToSubmitIAPs(ctx, appID)
	if err != nil {
		return []Diagnostic{r.fetchErr("list IAPs for "+ctx.BundleID, err,
			"check ASC API access and rate-limit headroom; retry after a minute.")}
	}
	if len(ready) == 0 {
		return nil
	}

	// Latest review submission and its items. We pick the most recently
	// submitted (or in-flight) submission rather than every historical one;
	// historical submissions can't pull a new IAP into review.
	subID, err := iapLatestSubmissionID(ctx, appID)
	if err != nil {
		return []Diagnostic{r.fetchErr("list review submissions for "+ctx.BundleID, err,
			"check ASC API access; the review-submissions endpoint requires filter[app].")}
	}
	if subID == "" {
		return iapUnattachedDiagnostics(r.ID(), ready, "no open review submission found for this app")
	}

	itemRefs, err := iapSubmissionItemReferences(ctx, subID)
	if err != nil {
		return []Diagnostic{r.fetchErr("list items for submission "+subID, err,
			"verify the submission id; Apple rotates submission ids when state cycles.")}
	}
	return r.unattachedDiagnostics(ctx.BundleID, subID, ready, itemRefs)
}

// readyToSubmitIAPs returns the READY_TO_SUBMIT IAPs for the app — the only
// candidates for the attachment check.
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

// unattachedDiagnostics emits one diagnostic per READY_TO_SUBMIT IAP whose ID
// does NOT appear in the submission's item-reference set.
func (r iapAttachedToReviewSubmissionRule) unattachedDiagnostics(bundleID, subID string, ready []asc.Resource[asc.IAPAttributes], itemRefs []submissionItemRef) []Diagnostic {
	attached := make(map[string]bool, len(itemRefs))
	for _, ref := range itemRefs {
		if ref.Type == "inAppPurchaseV2" || ref.Type == "inAppPurchase" {
			attached[ref.ID] = true
		}
	}
	out := make([]Diagnostic, 0, len(ready))
	for _, iap := range ready {
		if attached[iap.ID] {
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
				"add the IAP to the submission: `skipper review-submissions items %s --submission %s` to inspect, then attach via App Store Connect or the submissions write surface.",
				bundleID, subID,
			),
			Reference: "PRD §L3 — IAP attached-to-review-submission",
		})
	}
	return out
}

func (r iapAttachedToReviewSubmissionRule) fetchErr(what string, err error, fix string) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: %v", what, err),
		FixHint:  fix,
	}
}

// iapResolveAppID is a thin wrapper around the apps filter. We don't reach
// into internal/cmd or internal/state — the lint package is a peer.
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
		return "", fmt.Errorf("no app found")
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

// iapLatestSubmissionID picks the most relevant submission for the IAP-
// attachment check. Preference order: in-flight states (WAITING_FOR_REVIEW,
// IN_REVIEW, READY_FOR_REVIEW, COMPLETING) over completed/canceled ones.
// Returns "" when there are zero submissions at all.
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

// iapSubmissionItemReferences walks /v1/reviewSubmissions/{id}/items and
// extracts the (type, id) pair from each item's relationships block. Items
// with no resolvable reference are dropped (the data block is null between
// states).
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
			ref := extractRefFromRels(r.Relationships)
			if ref.Type != "" || ref.ID != "" {
				out = append(out, ref)
			}
		}
	}
	return out, nil
}

type submissionItemRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func extractRefFromRels(rels map[string]asc.Relationship) submissionItemRef {
	for _, rel := range rels {
		if len(rel.Data) == 0 || string(rel.Data) == "null" {
			continue
		}
		var ref submissionItemRef
		if err := json.Unmarshal(rel.Data, &ref); err != nil {
			continue
		}
		if ref.Type != "" || ref.ID != "" {
			return ref
		}
	}
	return submissionItemRef{}
}

func iapUnattachedDiagnostics(ruleID string, ready []asc.Resource[asc.IAPAttributes], reason string) []Diagnostic {
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
			FixHint: "create or open a review submission for the app and attach the IAP product. " +
				"`skipper review-submissions list <bundleId>` shows current submissions.",
			Reference: "PRD §L3 — IAP attached-to-review-submission",
		})
	}
	return out
}
