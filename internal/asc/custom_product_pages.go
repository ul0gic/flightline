package asc

// Custom Product Pages let developers ship alternate App Store listings
// (different screenshots / descriptions / promo text) targeted by ad URL.
// Apple groups them into three resources:
//
//   - AppCustomProductPage: the page itself (name + url + visible flag).
//   - AppCustomProductPageVersion: a versioned snapshot of the page
//     (state machine: PREPARE_FOR_SUBMISSION → … → APPROVED).
//   - AppCustomProductPageLocalization: per-locale promotional text + the
//     screenshot/preview sets (which reach into the App Store media graph).
//
// Source:
//
//	jq '.components.schemas.AppCustomProductPage.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.AppCustomProductPageVersion.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.AppCustomProductPageLocalization.properties.attributes.properties' openapi.oas.json
//
// Reused by Phase 3 write verbs (`custom-product-pages create`); the wire
// shape stays the same on POST/PATCH.

// CustomProductPageVersionState constants. Mirrors the AppStoreVersion
// state machine but with its own narrower enum (custom product pages don't
// have a TestFlight bucket).
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

// AppCustomProductPageAttributes is the subset of Apple's
// AppCustomProductPage.attributes Skipper reads. Name is the
// developer-friendly label; URL is the ad-targetable App Store URL Apple
// computes; Visible toggles whether the page is live.
type AppCustomProductPageAttributes struct {
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	Visible *bool  `json:"visible,omitempty"`
}

// AppCustomProductPageVersionAttributes is the subset of Apple's
// AppCustomProductPageVersion.attributes Skipper reads. Each page can have
// multiple versions; the "live" version is the one in the APPROVED state
// (or the last APPROVED before a REPLACED_WITH_NEW_VERSION).
//
// DeepLink is an optional URL that opens directly into a specific in-app
// destination when the user taps through from the custom page.
type AppCustomProductPageVersionAttributes struct {
	Version  string `json:"version,omitempty"`
	State    string `json:"state,omitempty"`
	DeepLink string `json:"deepLink,omitempty"`
}

// AppCustomProductPageLocalizationAttributes is the subset of Apple's
// AppCustomProductPageLocalization.attributes Skipper reads. Per-locale
// promotional text shown above the screenshot set on the custom page.
//
// Locale is Apple format (e.g. "en-US", "fr-FR"). The screenshot/preview
// sets live in separate relationships (not modeled in v1 — Phase 3
// `custom-product-pages screenshots` will reach for them).
type AppCustomProductPageLocalizationAttributes struct {
	Locale          string `json:"locale,omitempty"`
	PromotionalText string `json:"promotionalText,omitempty"`
}
