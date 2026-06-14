package asc

const (
	CustomProductPageVersionStatePrepareForSubmission = "PREPARE_FOR_SUBMISSION"
	CustomProductPageVersionStateReadyForReview       = "READY_FOR_REVIEW"
	CustomProductPageVersionStateWaitingForReview     = "WAITING_FOR_REVIEW"
	CustomProductPageVersionStateInReview             = "IN_REVIEW"
	CustomProductPageVersionStateAccepted             = "ACCEPTED"
	CustomProductPageVersionStateApproved             = "APPROVED"
	CustomProductPageVersionStateReplaced             = "REPLACED_WITH_NEW_VERSION"
	CustomProductPageVersionStateRejected             = "REJECTED"
)

// AppCustomProductPageAttributes is the subset of Apple's AppCustomProductPage.attributes Flightline reads.
// Name is developer-friendly; URL is Apple's ad-targetable App Store URL; Visible toggles live status.
type AppCustomProductPageAttributes struct {
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	Visible *bool  `json:"visible,omitempty"`
}

// AppCustomProductPageVersionAttributes is the subset of Apple's AppCustomProductPageVersion.attributes Flightline reads.
// The live version is the APPROVED one; DeepLink opens a specific in-app destination on tap-through.
type AppCustomProductPageVersionAttributes struct {
	Version  string `json:"version,omitempty"`
	State    string `json:"state,omitempty"`
	DeepLink string `json:"deepLink,omitempty"`
}

// AppCustomProductPageLocalizationAttributes is the subset of Apple's AppCustomProductPageLocalization.attributes.
// Per-locale promotional text; screenshot/preview sets live in relationships, not modeled here.
type AppCustomProductPageLocalizationAttributes struct {
	Locale          string `json:"locale,omitempty"`
	PromotionalText string `json:"promotionalText,omitempty"`
}
