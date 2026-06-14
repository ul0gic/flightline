package asc

// VersionAttributes is the subset of Apple's AppStoreVersion.attributes Flightline reads.
// AppStoreState (deprecated) and AppVersionState both surfaced; Apple populates one or the other depending on lifecycle position.
type VersionAttributes struct {
	Platform            string `json:"platform,omitempty"`
	VersionString       string `json:"versionString,omitempty"`
	AppStoreState       string `json:"appStoreState,omitempty"`
	AppVersionState     string `json:"appVersionState,omitempty"`
	Copyright           string `json:"copyright,omitempty"`
	ReviewType          string `json:"reviewType,omitempty"`
	ReleaseType         string `json:"releaseType,omitempty"`
	EarliestReleaseDate string `json:"earliestReleaseDate,omitempty"`
	Downloadable        *bool  `json:"downloadable,omitempty"`
	CreatedDate         string `json:"createdDate,omitempty"`
}
