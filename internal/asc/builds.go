package asc

// BuildAttributes is the subset of Apple's Build.attributes Flightline reads.
// Version is CFBundleVersion (build number), not CFBundleShortVersionString (marketing version).
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
