package asc

// BuildAttributes is the subset of Apple's Build.attributes Skipper reads.
//
// Source: jq '.components.schemas.Build.properties.attributes.properties' openapi.oas.json
//
// Field notes:
//   - Version is the build number (CFBundleVersion). The marketing version
//     (CFBundleShortVersionString) lives on the AppStoreVersion (preRelease
//     too), not on the Build resource.
//   - ProcessingState enum: PROCESSING | FAILED | INVALID | VALID. Apple's
//     "Processing" stage from upload completion to TestFlight availability.
//   - Expired and ExpirationDate are paired: Expired is the boolean snapshot;
//     ExpirationDate is the timestamp Apple revoked the build's TestFlight
//     install eligibility (90 days after upload by default).
//   - UsesNonExemptEncryption is the export-compliance answer captured at
//     upload time. nil = not yet declared.
//
// Reused by Phase 3 write verbs; add fields conservatively.
type BuildAttributes struct {
	Version                 string `json:"version,omitempty"`
	UploadedDate            string `json:"uploadedDate,omitempty"`
	ExpirationDate          string `json:"expirationDate,omitempty"`
	Expired                 *bool  `json:"expired,omitempty"`
	MinOsVersion            string `json:"minOsVersion,omitempty"`
	ProcessingState         string `json:"processingState,omitempty"`
	UsesNonExemptEncryption *bool  `json:"usesNonExemptEncryption,omitempty"`
	BuildAudienceType       string `json:"buildAudienceType,omitempty"`
}
