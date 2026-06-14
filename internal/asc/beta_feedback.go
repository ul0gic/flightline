package asc

// BetaFeedbackBaseAttributes is the shared envelope across crash and screenshot submissions.
// Apple's wire field names are preserved exactly: `appUptimeInMilliseconds`, `screenWidthInPoints`.
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

// BetaFeedbackCrashSubmissionAttributes holds the attributes for a crash submission.
// The crash log text lives at /v1/betaFeedbackCrashSubmissions/{id}/crashLog, fetched separately.
type BetaFeedbackCrashSubmissionAttributes struct {
	BetaFeedbackBaseAttributes
}

// BetaFeedbackScreenshotImage describes one screenshot attachment with a pre-signed download URL.
type BetaFeedbackScreenshotImage struct {
	URL            string `json:"url,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	ExpirationDate string `json:"expirationDate,omitempty"`
}

// BetaFeedbackScreenshotSubmissionAttributes holds the screenshot submission attributes.
// Screenshots are inline pre-signed URLs; there is no separate relationship endpoint for the bytes.
type BetaFeedbackScreenshotSubmissionAttributes struct {
	BetaFeedbackBaseAttributes
	Screenshots []BetaFeedbackScreenshotImage `json:"screenshots,omitempty"`
}

// BetaCrashLogAttributes holds the crash log text. Apple returns it as a single string.
type BetaCrashLogAttributes struct {
	LogText string `json:"logText,omitempty"`
}
