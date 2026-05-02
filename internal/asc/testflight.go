package asc

// TestFlight read surface. Apple groups TestFlight under several resources:
//
//   - BetaGroup: a group of testers (internal team or external public).
//   - BetaTester: an individual tester (email + invite type + state).
//   - BetaAppReviewSubmission: per-build beta-review submission state.
//   - BetaBuildLocalization: per-locale "What's New" text shown to testers.
//
// Source:
//
//	jq '.components.schemas.BetaGroup.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.BetaTester.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.BetaAppReviewSubmission.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.BetaBuildLocalization.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.BetaInviteType.enum' openapi.oas.json
//	jq '.components.schemas.BetaTesterState.enum' openapi.oas.json
//	jq '.components.schemas.BetaReviewState.enum' openapi.oas.json
//
// Reused by Phase 3 write verbs (`testflight invite`, `testflight create-group`);
// the wire shape stays the same on POST/PATCH.

// Apple-defined enums. Surfaced as named constants so command code can
// filter/compare without typos and readers can grep for the canonical set.
const (
	BetaInviteTypeEmail      = "EMAIL"
	BetaInviteTypePublicLink = "PUBLIC_LINK"

	BetaTesterStateNotInvited = "NOT_INVITED"
	BetaTesterStateInvited    = "INVITED"
	BetaTesterStateAccepted   = "ACCEPTED"
	BetaTesterStateInstalled  = "INSTALLED"
	BetaTesterStateRevoked    = "REVOKED"

	BetaReviewStateWaitingForReview = "WAITING_FOR_REVIEW"
	BetaReviewStateInReview         = "IN_REVIEW"
	BetaReviewStateRejected         = "REJECTED"
	BetaReviewStateApproved         = "APPROVED"
)

// BetaGroupAttributes is the subset of Apple's BetaGroup.attributes Skipper
// reads. Internal-vs-external groups share the same resource type;
// IsInternalGroup distinguishes them.
//
// PublicLink + PublicLinkLimit are surfaced because they're frequently
// needed during onboarding flows ("here's the join URL"). HasAccessToAllBuilds
// captures the auto-promote setting that's a frequent rejection-cause
// during external review when developers forget the new build needs to be
// approved before it's auto-distributed.
type BetaGroupAttributes struct {
	Name                                 string `json:"name,omitempty"`
	CreatedDate                          string `json:"createdDate,omitempty"`
	IsInternalGroup                      *bool  `json:"isInternalGroup,omitempty"`
	HasAccessToAllBuilds                 *bool  `json:"hasAccessToAllBuilds,omitempty"`
	PublicLinkEnabled                    *bool  `json:"publicLinkEnabled,omitempty"`
	PublicLinkID                         string `json:"publicLinkId,omitempty"`
	PublicLinkLimitEnabled               *bool  `json:"publicLinkLimitEnabled,omitempty"`
	PublicLinkLimit                      int    `json:"publicLinkLimit,omitempty"`
	PublicLink                           string `json:"publicLink,omitempty"`
	FeedbackEnabled                      *bool  `json:"feedbackEnabled,omitempty"`
	IosBuildsAvailableForAppleSiliconMac *bool  `json:"iosBuildsAvailableForAppleSiliconMac,omitempty"`
	IosBuildsAvailableForAppleVision     *bool  `json:"iosBuildsAvailableForAppleVision,omitempty"`
}

// BetaTesterAttributes is the subset of Apple's BetaTester.attributes
// Skipper reads. The id on the resource is Apple's internal tester id;
// email is the operator-meaningful identifier.
//
// AppDevices is omitted from this struct (it's a per-tester device list
// that's only relevant on the detail endpoint, and including it would
// pollute the list view). Phase 3 device-detail commands can re-introduce
// it on a dedicated view.
type BetaTesterAttributes struct {
	FirstName  string `json:"firstName,omitempty"`
	LastName   string `json:"lastName,omitempty"`
	Email      string `json:"email,omitempty"`
	InviteType string `json:"inviteType,omitempty"`
	State      string `json:"state,omitempty"`
}

// BetaAppReviewSubmissionAttributes is the subset of Apple's
// BetaAppReviewSubmission.attributes Skipper reads. Per-build beta-review
// submission carries a state (WAITING_FOR_REVIEW | IN_REVIEW | REJECTED |
// APPROVED) plus a submittedDate. Apple does not expose review notes via
// the API for beta submissions.
type BetaAppReviewSubmissionAttributes struct {
	BetaReviewState string `json:"betaReviewState,omitempty"`
	SubmittedDate   string `json:"submittedDate,omitempty"`
}

// BetaBuildLocalizationAttributes is the subset of Apple's
// BetaBuildLocalization.attributes Skipper reads. Per-locale "What's New"
// text shown to testers when a new build is released; the locale is
// Apple-format (e.g. "en-US", "fr-FR").
type BetaBuildLocalizationAttributes struct {
	WhatsNew string `json:"whatsNew,omitempty"`
	Locale   string `json:"locale,omitempty"`
}
