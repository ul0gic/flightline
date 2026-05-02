package asc

// Beta feedback read surface. Apple ships TestFlight feedback in two
// resource families that share most attributes:
//
//   - BetaFeedbackCrashSubmission: a tester's crash report (auto-collected
//     when an app traps; tester optionally adds an email and comment).
//   - BetaFeedbackScreenshotSubmission: a tester's manual screenshot +
//     comment captured via the TestFlight share-sheet.
//
// Both share the device-environment quad (deviceModel, osVersion, locale,
// timeZone, architecture, connectionType, pairedAppleWatch) plus disk /
// battery / screen telemetry. Screenshot submissions additionally carry a
// `screenshots[]` array of pre-signed image URLs.
//
// Source:
//
//	jq '.components.schemas.BetaFeedbackCrashSubmission.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.BetaFeedbackScreenshotSubmission.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.BetaCrashLog.properties.attributes.properties' openapi.oas.json
//
// Endpoints:
//
//	GET /v1/apps/{id}/betaFeedbackCrashSubmissions       — list crashes
//	GET /v1/apps/{id}/betaFeedbackScreenshotSubmissions  — list screenshots
//	GET /v1/betaFeedbackCrashSubmissions/{id}            — single crash
//	GET /v1/betaFeedbackScreenshotSubmissions/{id}       — single screenshot
//	GET /v1/betaFeedbackCrashSubmissions/{id}/crashLog   — log text
//
// Reused by Phase 3 download verbs (no Apple writes here — feedback is
// tester-authored). Adding fields conservatively.

// BetaFeedbackBaseAttributes is the shared envelope across crash and
// screenshot submissions. Pulled out so both resource types embed the same
// shape and JSON consumers see consistent field names.
//
// Apple's wire field names are preserved exactly: `appUptimeInMilliseconds`
// (not `appUptimeMillis`), `screenWidthInPoints` (not `screenWidthInPx`).
type BetaFeedbackBaseAttributes struct {
	CreatedDate             string `json:"createdDate,omitempty"`
	Comment                 string `json:"comment,omitempty"`
	Email                   string `json:"email,omitempty"`
	DeviceModel             string `json:"deviceModel,omitempty"`
	OsVersion               string `json:"osVersion,omitempty"`
	Locale                  string `json:"locale,omitempty"`
	TimeZone                string `json:"timeZone,omitempty"`
	Architecture            string `json:"architecture,omitempty"`
	ConnectionType          string `json:"connectionType,omitempty"`
	PairedAppleWatch        string `json:"pairedAppleWatch,omitempty"`
	AppUptimeInMilliseconds int64  `json:"appUptimeInMilliseconds,omitempty"`
	DiskBytesAvailable      int64  `json:"diskBytesAvailable,omitempty"`
	DiskBytesTotal          int64  `json:"diskBytesTotal,omitempty"`
	BatteryPercentage       int    `json:"batteryPercentage,omitempty"`
	ScreenWidthInPoints     int    `json:"screenWidthInPoints,omitempty"`
	ScreenHeightInPoints    int    `json:"screenHeightInPoints,omitempty"`
	AppPlatform             string `json:"appPlatform,omitempty"`
	DevicePlatform          string `json:"devicePlatform,omitempty"`
	DeviceFamily            string `json:"deviceFamily,omitempty"`
	BuildBundleID           string `json:"buildBundleId,omitempty"`
}

// BetaFeedbackCrashSubmissionAttributes is the subset of Apple's
// BetaFeedbackCrashSubmission.attributes Skipper reads. The actual crash
// log text lives at the relationship endpoint
// /v1/betaFeedbackCrashSubmissions/{id}/crashLog and is fetched separately
// when the user runs `beta-feedback download`.
type BetaFeedbackCrashSubmissionAttributes struct {
	BetaFeedbackBaseAttributes
}

// BetaFeedbackScreenshotImage describes one screenshot attachment with a
// pre-signed download URL. ExpirationDate is the URL's expiry, after which
// it must be re-fetched via the parent submission.
type BetaFeedbackScreenshotImage struct {
	URL            string `json:"url,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	ExpirationDate string `json:"expirationDate,omitempty"`
}

// BetaFeedbackScreenshotSubmissionAttributes is the subset of Apple's
// BetaFeedbackScreenshotSubmission.attributes Skipper reads. Screenshots
// holds the pre-signed image URLs inline — there's no separate
// relationships endpoint for screenshot bytes.
type BetaFeedbackScreenshotSubmissionAttributes struct {
	BetaFeedbackBaseAttributes
	Screenshots []BetaFeedbackScreenshotImage `json:"screenshots,omitempty"`
}

// BetaCrashLogAttributes is the subset of Apple's BetaCrashLog.attributes
// Skipper reads. LogText is the symbolicated crash log body; Apple returns
// it as a single string, not a structured object.
type BetaCrashLogAttributes struct {
	LogText string `json:"logText,omitempty"`
}
