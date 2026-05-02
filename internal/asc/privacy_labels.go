package asc

// Privacy nutrition labels are NOT in App Store Connect API v4.3.
//
// Verified absent via:
//
//	jq '.paths | keys[] | select(test("[Pp]rivacy"))' openapi.oas.json   # (empty)
//	jq '.components.schemas | keys[] | select(test("[Pp]rivacy"))' openapi.oas.json   # (empty)
//
// Apple's privacy nutrition labels (the authoring surface they call
// "App Privacy Details") live in App Store Connect's web UI only — there
// is no public REST endpoint to read or write them in the v4.3 spec.
//
// The types in this file capture the wire shape Skipper would consume IF
// Apple ever ships an `appPrivacyDetails` resource. They mirror the public
// data model documented at:
//
//	https://developer.apple.com/app-store/app-privacy-details/
//
// — categories of collected data ("Contact Info", "Location", "Health &
// Fitness", …), each with linked-to-user / used-for-tracking flags, and a
// per-purpose breakdown ("Analytics", "Advertising", "App Functionality",
// "Product Personalization", "Other Purposes").
//
// When Apple ships the API, switch the cmd from the stub diagnostic to a
// typed Get[T] call against the new endpoint. The struct names here are
// stable predictions; cmd-side consumers must not depend on them being
// authoritative until ISSUE-002 closes.
//
// See .project/issues/open/ISSUE-002-privacy-labels-not-in-asc-api.md.

// AppPrivacyDetailAttributes is the speculative wire shape for Apple's
// `appPrivacyDetails` resource. Empty struct in v4.3 because Apple does
// not expose any fields; reserved here so when the API ships, only the
// field set changes and not the import path.
type AppPrivacyDetailAttributes struct {
	// Reserved for the categories array Apple's web UI captures (contact
	// info, health & fitness, financial info, location, etc.). Until the
	// API exposes these, this struct stays empty by design — adding
	// speculative fields would freeze a contract Apple hasn't published.
}
