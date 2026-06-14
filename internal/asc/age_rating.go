package asc

// Frequency values are shared across all age-rating categories; Apple encodes the same enum at every site.
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

// AgeRatingDeclarationAttributes is the full age-rating questionnaire. *bool fields distinguish
// nil ("unanswered") from false ("explicitly no"), which matters for L3 preflight checks.
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

// AppInfoAttributes is the subset of AppInfo.attributes needed to match an appInfo to a version lifecycle state.
type AppInfoAttributes struct {
	State string `json:"state,omitempty"`
}
