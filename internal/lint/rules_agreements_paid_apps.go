package lint

// paidAppsAgreementRule reminds IAP-bearing apps that the Paid Apps Agreement is API-invisible.
// A lapsed agreement surfaces in review as "IAP product not found" with no API signal beforehand.
type paidAppsAgreementRule struct{}

func init() { Register(paidAppsAgreementRule{}) }

func (paidAppsAgreementRule) ID() string         { return "agreements.paid-apps-unverifiable" }
func (paidAppsAgreementRule) Severity() Severity { return SeverityInfo }
func (paidAppsAgreementRule) Mode() Mode         { return ModeLive }
func (paidAppsAgreementRule) Doc() string {
	return "Reminds you to confirm the Paid Apps Agreement whenever the app sells in-app purchases. " +
		"Apple exposes no API for agreement status, and a lapsed or re-issued agreement makes every IAP silently invisible to the reviewer's sandbox — the rejection reads \"IAP product not found\" with nothing wrong in ASC. " +
		"Confirm the agreement is Active under Business in App Store Connect before submitting; this diagnostic is informational and never fails preflight."
}

func (r paidAppsAgreementRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}

	// Fetch failures stay silent here: the error-severity IAP rules hit the same endpoints and report them.
	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return nil
	}
	iaps, err := iapListForApp(ctx, appID)
	if err != nil || len(iaps) == 0 {
		return nil
	}

	return []Diagnostic{{
		RuleID:   r.ID(),
		Severity: SeverityInfo,
		Message: "app sells in-app purchases; Paid Apps Agreement status is not exposed by the API — " +
			"confirm it is Active under Business in App Store Connect before submitting",
		Path:      "/spec/iap",
		FixHint:   "App Store Connect → Business → Agreements: the Paid Apps Agreement must show Active, and re-accept after Apple issues updated terms.",
		Reference: "Rejection corpus: NetProbe 2026-03-14 (lapsed agreement surfaced as \"IAP product not found\")",
	}}
}
