package lint

// versionAccountDeletionAttestedRule reminds the user to confirm Apple's
// in-app account-deletion attestation when the app has user accounts.
//
// API note: Apple's public App Store Connect API does NOT expose the
// account-deletion attestation field. The flag lives behind the
// "Distribution > App Privacy > Account Deletion" panel in App Store
// Connect's web UI and is not surfaced through any /v1 endpoint we can
// query. As a result this rule cannot do a definitive yes/no check —
// instead it emits an Info-severity reminder so the user remembers to
// verify the panel before submission. Apps with user accounts that
// haven't toggled the attestation are a frequent rejection cause
// (Guideline 5.1.1(v), data-collection / account-deletion).
//
// When Apple ships an API surface for the attestation we'll upgrade
// this to a hard Error rule. Until then it's a documentation crutch
// — but a load-bearing one.
//
// Mode=Both. The reminder is the same offline and live; we just want
// users to see it during preflight.
type versionAccountDeletionAttestedRule struct{}

func init() { Register(versionAccountDeletionAttestedRule{}) }

func (versionAccountDeletionAttestedRule) ID() string         { return "version.account-deletion-attested" }
func (versionAccountDeletionAttestedRule) Severity() Severity { return SeverityInfo }
func (versionAccountDeletionAttestedRule) Mode() Mode         { return ModeBoth }

func (r versionAccountDeletionAttestedRule) Check(_ CheckContext) []Diagnostic {
	return []Diagnostic{{
		RuleID:   r.ID(),
		Severity: SeverityInfo,
		Message: "if the app has user accounts, confirm the in-app account-deletion attestation " +
			"is enabled in App Store Connect (App > Distribution > App Privacy > Account Deletion). " +
			"Apple does not expose this field via the public API; preflight cannot verify it.",
		Path: "/spec/appInfo",
		FixHint: "in App Store Connect: App Privacy > Account Deletion > toggle on, " +
			"or document why your app is exempt (no user accounts).",
		Reference: "Apple Guideline 5.1.1(v)",
	}}
}
