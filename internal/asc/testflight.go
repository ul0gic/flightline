package asc

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

// BetaGroupAttributes is the subset of Apple's BetaGroup.attributes Flightline reads.
// IsInternalGroup distinguishes internal/external; HasAccessToAllBuilds is the auto-promote flag that causes rejections.
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

// BetaTesterAttributes is the subset of Apple's BetaTester.attributes Flightline reads.
// AppDevices is omitted (only relevant on the detail endpoint; would pollute the list view).
type BetaTesterAttributes struct {
	FirstName  string `json:"firstName,omitempty"`
	LastName   string `json:"lastName,omitempty"`
	Email      string `json:"email,omitempty"`
	InviteType string `json:"inviteType,omitempty"`
	State      string `json:"state,omitempty"`
}

// BetaAppReviewSubmissionAttributes is the subset of Apple's BetaAppReviewSubmission.attributes Flightline reads.
// Apple does not expose review notes via the API for beta submissions.
type BetaAppReviewSubmissionAttributes struct {
	BetaReviewState string `json:"betaReviewState,omitempty"`
	SubmittedDate   string `json:"submittedDate,omitempty"`
}

// BetaBuildLocalizationAttributes is the subset of Apple's BetaBuildLocalization.attributes Flightline reads.
// Per-locale "What's New" text for testers; locale is Apple-format (e.g. "en-US", "fr-FR").
type BetaBuildLocalizationAttributes struct {
	WhatsNew string `json:"whatsNew,omitempty"`
	Locale   string `json:"locale,omitempty"`
}
