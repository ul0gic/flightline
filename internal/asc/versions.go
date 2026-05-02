package asc

// VersionAttributes is the subset of Apple's AppStoreVersion.attributes
// Skipper reads. The wire-shape contract: field names match Apple's casing
// (`versionString`, not `version_string`), enums match Apple's spec exactly.
//
// Source: jq '.components.schemas.AppStoreVersion.properties.attributes.properties' openapi.oas.json
//
// Notes on state fields:
//   - AppStoreState is deprecated by Apple in favor of the modern review
//     submission flow, but older versions still surface state through it.
//     Skipper reads BOTH so callers see whichever Apple populates.
//   - AppVersionState is the newer field; some endpoints emit one, some
//     emit the other depending on the version's lifecycle position.
//
// Reused by Phase 3 write verbs (`versions update`); add fields conservatively.
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
