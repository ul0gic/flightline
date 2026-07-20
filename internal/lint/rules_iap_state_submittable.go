package lint

import (
	"fmt"

	"github.com/ul0gic/flightline/internal/asc"
)

// iapStateSubmittableRule errors on IAPs stuck in states Apple will not carry through review.
// Attachment alone is not enough: a DEVELOPER_ACTION_NEEDED IAP fails review even when submitted.
type iapStateSubmittableRule struct{}

func init() { Register(iapStateSubmittableRule{}) }

func (iapStateSubmittableRule) ID() string         { return "iap.state-submittable" }
func (iapStateSubmittableRule) Severity() Severity { return SeverityError }
func (iapStateSubmittableRule) Mode() Mode         { return ModeLive }
func (iapStateSubmittableRule) Doc() string {
	return "Errors when any in-app purchase is in DEVELOPER_ACTION_NEEDED, MISSING_METADATA, or REJECTED state at preflight time. " +
		"Resubmitting the app without fixing these guarantees another rejection loop: the reviewer cannot find or approve a product Apple has flagged back to you. " +
		"Fix it by resolving the flagged action or completing the metadata in App Store Connect, then re-running preflight."
}

var iapBlockedStates = map[string]string{
	asc.IAPStateDeveloperActionNeeded: "resolve the requested action on the IAP in App Store Connect, then resubmit its metadata.",
	asc.IAPStateMissingMetadata:       "complete the IAP's metadata: display name, description, review screenshot, and at least one localization.",
	asc.IAPStateRejected:              "edit the rejected IAP's detail information and resubmit it for review.",
}

func (r iapStateSubmittableRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{{RuleID: r.ID(), Severity: SeverityError,
			Message: "resolve app for bundleId " + ctx.BundleID + ": " + err.Error(),
			FixHint: "verify the bundle id and that the API key has access to it."}}
	}
	iaps, err := iapListForApp(ctx, appID)
	if err != nil {
		return []Diagnostic{{RuleID: r.ID(), Severity: SeverityError,
			Message: "list IAPs for " + ctx.BundleID + ": " + err.Error(),
			FixHint: "check ASC API access and rate-limit headroom; retry after a minute."}}
	}

	var out []Diagnostic
	for _, iap := range iaps {
		fix, blocked := iapBlockedStates[iap.Attributes.State]
		if !blocked {
			continue
		}
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message: fmt.Sprintf("IAP %q is in state %s and cannot pass review",
				iap.Attributes.ProductID, iap.Attributes.State),
			Path:      "/spec/iap/products/" + iap.Attributes.ProductID,
			FixHint:   fix,
			Reference: "Guideline 2.1(b) App Completeness; rejection corpus: NetProbe 2026-03-17",
		})
	}
	return out
}
