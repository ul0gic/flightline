package asc

// AppEncryptionDeclarationState enumerates Apple's review states for
// AppEncryptionDeclaration resources. Listed as constants so command code
// can surface them without typos.
//
// Source: jq '.components.schemas.AppEncryptionDeclarationState' openapi.oas.json
const (
	EncryptionDeclarationStateCreated  = "CREATED"
	EncryptionDeclarationStateInReview = "IN_REVIEW"
	EncryptionDeclarationStateApproved = "APPROVED"
	EncryptionDeclarationStateRejected = "REJECTED"
	EncryptionDeclarationStateInvalid  = "INVALID"
	EncryptionDeclarationStateExpired  = "EXPIRED"
)

// AppEncryptionDeclarationAttributes is the subset of Apple's
// AppEncryptionDeclaration.attributes Skipper reads. This is the
// per-app/per-build declaration submitted to Apple for full ECCN
// classification when an app's encryption requires more than the simple
// `usesNonExemptEncryption` boolean answer.
//
// Source: jq '.components.schemas.AppEncryptionDeclaration.properties.attributes.properties' openapi.oas.json
//
// Field notes:
//   - Exempt is the consolidated answer Apple wants when an app qualifies
//     for one of the published export-compliance exemptions (mass-market,
//     standard encryption only, etc.). When true, the app is exempt from
//     filing further documentation.
//   - ContainsProprietaryCryptography / ContainsThirdPartyCryptography are
//     the two per-source-of-crypto flags. AvailableOnFrenchStore is the
//     historical France-specific question Apple still asks.
//   - UsesEncryption is deprecated by Apple in favor of the more granular
//     fields above; surfaced for read parity with older app records.
//   - DocumentURL/DocumentName/DocumentType are deprecated in favor of the
//     separate appEncryptionDeclarationDocument resource.
//   - CodeValue is the ECCN classification number Apple stamps on approved
//     declarations.
//   - AppDescription is the developer's free-form summary of the
//     cryptography used.
//
// Reused by Phase 3 write verbs (`export-compliance create/update`); kept to
// fields Apple actually exposes on the wire.
type AppEncryptionDeclarationAttributes struct {
	AppDescription                  string `json:"appDescription,omitempty"`
	CreatedDate                     string `json:"createdDate,omitempty"`
	UsesEncryption                  *bool  `json:"usesEncryption,omitempty"`
	Exempt                          *bool  `json:"exempt,omitempty"`
	ContainsProprietaryCryptography *bool  `json:"containsProprietaryCryptography,omitempty"`
	ContainsThirdPartyCryptography  *bool  `json:"containsThirdPartyCryptography,omitempty"`
	AvailableOnFrenchStore          *bool  `json:"availableOnFrenchStore,omitempty"`
	Platform                        string `json:"platform,omitempty"`
	UploadedDate                    string `json:"uploadedDate,omitempty"`
	DocumentURL                     string `json:"documentUrl,omitempty"`
	DocumentName                    string `json:"documentName,omitempty"`
	DocumentType                    string `json:"documentType,omitempty"`
	AppEncryptionDeclarationState   string `json:"appEncryptionDeclarationState,omitempty"`
	CodeValue                       string `json:"codeValue,omitempty"`
}

// BuildEncryptionView reflects the per-build encryption answer, which lives
// on Build.attributes.usesNonExemptEncryption rather than on a separate
// AppStoreVersion field. This is the simpler "did I answer the question"
// signal that L3 preflight checks; a missing answer (nil) is the failure
// mode that gets a release rejected at submission time.
//
// Apple's older schema put this field on AppStoreVersion; current spec
// (v4.3) places it on Build. This file calls it out so future readers don't
// hunt for a non-existent version-level field.
type BuildEncryptionView struct {
	BuildID                 string `json:"buildId,omitempty"`
	BuildVersion            string `json:"buildVersion,omitempty"`
	UsesNonExemptEncryption *bool  `json:"usesNonExemptEncryption,omitempty"`
}
