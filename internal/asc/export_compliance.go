package asc

const (
	EncryptionDeclarationStateCreated  = "CREATED"
	EncryptionDeclarationStateInReview = "IN_REVIEW"
	EncryptionDeclarationStateApproved = "APPROVED"
	EncryptionDeclarationStateRejected = "REJECTED"
	EncryptionDeclarationStateInvalid  = "INVALID"
	EncryptionDeclarationStateExpired  = "EXPIRED"
)

// AppEncryptionDeclarationAttributes is the subset of Apple's AppEncryptionDeclaration.attributes Flightline reads.
// Per-app/build ECCN declaration; UsesEncryption and Document* fields are deprecated by Apple in v4.x.
type AppEncryptionDeclarationAttributes struct {
	AppDescription                  string `json:"appDescription,omitempty"`
	CreatedDate                     string `json:"createdDate,omitempty"`
	UsesEncryption                  *bool  `json:"usesEncryption,omitempty"` // deprecated in favor of Exempt
	Exempt                          *bool  `json:"exempt,omitempty"`         // true = qualifies for export-compliance exemption
	ContainsProprietaryCryptography *bool  `json:"containsProprietaryCryptography,omitempty"`
	ContainsThirdPartyCryptography  *bool  `json:"containsThirdPartyCryptography,omitempty"`
	AvailableOnFrenchStore          *bool  `json:"availableOnFrenchStore,omitempty"` // historical France-specific question
	Platform                        string `json:"platform,omitempty"`
	UploadedDate                    string `json:"uploadedDate,omitempty"`
	DocumentURL                     string `json:"documentUrl,omitempty"`  // deprecated; use appEncryptionDeclarationDocument
	DocumentName                    string `json:"documentName,omitempty"` // deprecated
	DocumentType                    string `json:"documentType,omitempty"` // deprecated
	AppEncryptionDeclarationState   string `json:"appEncryptionDeclarationState,omitempty"`
	CodeValue                       string `json:"codeValue,omitempty"` // ECCN classification Apple stamps on approval
}

// BuildEncryptionView reflects per-build encryption status (nil UsesNonExemptEncryption = unanswered = submission rejected).
// In spec v4.3 this field moved from AppStoreVersion to Build; do not look for it on the version resource.
type BuildEncryptionView struct {
	BuildID                 string `json:"buildId,omitempty"`
	BuildVersion            string `json:"buildVersion,omitempty"`
	UsesNonExemptEncryption *bool  `json:"usesNonExemptEncryption,omitempty"`
}
