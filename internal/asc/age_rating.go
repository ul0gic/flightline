package asc

// Apple's age rating questionnaire enums. Three shapes:
//
//   - Frequency enums (NONE | INFREQUENT_OR_MILD | FREQUENT_OR_INTENSE |
//     INFREQUENT | FREQUENT) — applied to most content categories.
//   - Boolean fields — yes/no questions like advertising or messagingAndChat.
//   - Override enums — manual age-rating overrides (general, Korea, etc.).
//
// The frequency enum is the same across all categories so we surface it via
// shared constants rather than per-field enum types — Apple's spec encodes
// the same enum inline at every site.
//
// Source: jq '.components.schemas.AgeRatingDeclaration.properties.attributes.properties' openapi.oas.json
const (
	AgeRatingFrequencyNone              = "NONE"
	AgeRatingFrequencyInfrequentOrMild  = "INFREQUENT_OR_MILD"
	AgeRatingFrequencyFrequentOrIntense = "FREQUENT_OR_INTENSE"
	AgeRatingFrequencyInfrequent        = "INFREQUENT"
	AgeRatingFrequencyFrequent          = "FREQUENT"

	KidsAgeBandFiveAndUnder = "FIVE_AND_UNDER"
	KidsAgeBandSixToEight   = "SIX_TO_EIGHT"
	KidsAgeBandNineToEleven = "NINE_TO_ELEVEN"
)

// AgeRatingDeclarationAttributes is the full ASC age-rating questionnaire
// surface — every field Apple asks during App Store Connect's age-rating
// flow. Skipper keeps the entire shape because L3 preflight rules will
// flag missing answers (they are a frequent rejection cause).
//
// Source: jq '.components.schemas.AgeRatingDeclaration.properties.attributes.properties' openapi.oas.json
//
// Notes on field shapes:
//   - Frequency-enum fields are strings; pointer-bool fields use *bool because
//     nil ("not yet answered") and false ("explicitly no") have different
//     operational meaning during the L3 preflight check.
//   - AgeRatingOverride is deprecated by Apple in favor of AgeRatingOverrideV2;
//     we surface both so older app records still decode cleanly.
//   - DeveloperAgeRatingInfoURL is a free-form URL Apple shows the reviewer.
//
// Reused by Phase 3 write verbs (`age-rating set`); add fields conservatively.
type AgeRatingDeclarationAttributes struct {
	// Boolean fields (yes/no questions)
	Advertising            *bool `json:"advertising,omitempty"`
	AgeAssurance           *bool `json:"ageAssurance,omitempty"`
	Gambling               *bool `json:"gambling,omitempty"`
	HealthOrWellnessTopics *bool `json:"healthOrWellnessTopics,omitempty"`
	LootBox                *bool `json:"lootBox,omitempty"`
	MessagingAndChat       *bool `json:"messagingAndChat,omitempty"`
	ParentalControls       *bool `json:"parentalControls,omitempty"`
	UnrestrictedWebAccess  *bool `json:"unrestrictedWebAccess,omitempty"`
	UserGeneratedContent   *bool `json:"userGeneratedContent,omitempty"`

	// Frequency-enum fields (NONE | INFREQUENT_OR_MILD | FREQUENT_OR_INTENSE | INFREQUENT | FREQUENT)
	AlcoholTobaccoOrDrugUseOrReferences         string `json:"alcoholTobaccoOrDrugUseOrReferences,omitempty"`
	Contests                                    string `json:"contests,omitempty"`
	GamblingSimulated                           string `json:"gamblingSimulated,omitempty"`
	GunsOrOtherWeapons                          string `json:"gunsOrOtherWeapons,omitempty"`
	HorrorOrFearThemes                          string `json:"horrorOrFearThemes,omitempty"`
	MatureOrSuggestiveThemes                    string `json:"matureOrSuggestiveThemes,omitempty"`
	MedicalOrTreatmentInformation               string `json:"medicalOrTreatmentInformation,omitempty"`
	ProfanityOrCrudeHumor                       string `json:"profanityOrCrudeHumor,omitempty"`
	SexualContentGraphicAndNudity               string `json:"sexualContentGraphicAndNudity,omitempty"`
	SexualContentOrNudity                       string `json:"sexualContentOrNudity,omitempty"`
	ViolenceCartoonOrFantasy                    string `json:"violenceCartoonOrFantasy,omitempty"`
	ViolenceRealistic                           string `json:"violenceRealistic,omitempty"`
	ViolenceRealisticProlongedGraphicOrSadistic string `json:"violenceRealisticProlongedGraphicOrSadistic,omitempty"`

	// Kids-band, override, and reviewer-info fields.
	KidsAgeBand               string `json:"kidsAgeBand,omitempty"`
	AgeRatingOverride         string `json:"ageRatingOverride,omitempty"`
	AgeRatingOverrideV2       string `json:"ageRatingOverrideV2,omitempty"`
	KoreaAgeRatingOverride    string `json:"koreaAgeRatingOverride,omitempty"`
	DeveloperAgeRatingInfoURL string `json:"developerAgeRatingInfoUrl,omitempty"`
}

// AppInfoAttributes is the subset of Apple's AppInfo.attributes Skipper
// reads. AppInfo is the per-review-cycle metadata bag (state mirrors version
// state); for age-rating reads we use it to pick the right appInfo for a
// version (each version has a corresponding appInfo in matching lifecycle
// state).
//
// Source: jq '.components.schemas.AppInfo.properties.attributes.properties' openapi.oas.json
//
// Apple's older age-rating fields (australiaAgeRating, brazilAgeRating, …)
// are all marked deprecated; the source of truth is the related
// ageRatingDeclaration resource, not AppInfo.attributes. Skipper surfaces
// only the fields needed to pick the right appInfo for a version.
type AppInfoAttributes struct {
	State string `json:"state,omitempty"`
}
