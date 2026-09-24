package config

// IAPCommerceSpec preserves explicit managed fields; nil pointers mean omitted intent.
type IAPCommerceSpec struct {
	Pricing      *IAPPriceSpec        `yaml:"pricing,omitempty" json:"pricing,omitempty"`
	Availability *IAPAvailabilitySpec `yaml:"availability,omitempty" json:"availability,omitempty"`
}

// IAPPriceSpec preserves explicit managed fields; nil pointers mean omitted intent.
type IAPPriceSpec struct {
	BaseTerritory string `yaml:"baseTerritory" json:"baseTerritory"`
	PricePointID  string `yaml:"pricePointId" json:"pricePointId"`
}

// IAPAvailabilitySpec preserves explicit managed fields; nil pointers mean omitted intent.
type IAPAvailabilitySpec struct {
	AvailableInNewTerritories *bool     `yaml:"availableInNewTerritories,omitempty" json:"availableInNewTerritories,omitempty"`
	AvailableTerritories      *[]string `yaml:"availableTerritories,omitempty" json:"availableTerritories,omitempty"`
}

// AppAvailabilitySpec preserves explicit managed fields; nil pointers mean omitted intent.
type AppAvailabilitySpec struct {
	AvailableInNewTerritories *bool                                `yaml:"availableInNewTerritories,omitempty" json:"availableInNewTerritories,omitempty"`
	Territories               map[string]TerritoryAvailabilitySpec `yaml:"territories,omitempty" json:"territories,omitempty"`
}

// TerritoryAvailabilitySpec preserves explicit managed fields; nil pointers mean omitted intent.
type TerritoryAvailabilitySpec struct {
	Available           *bool    `yaml:"available,omitempty" json:"available,omitempty"`
	ReleaseDate         *string  `yaml:"releaseDate,omitempty" json:"releaseDate,omitempty"`
	PreOrderEnabled     *bool    `yaml:"preOrderEnabled,omitempty" json:"preOrderEnabled,omitempty"`
	PreOrderPublishDate *string  `yaml:"preOrderPublishDate,omitempty" json:"preOrderPublishDate,omitempty"`
	ContentStatuses     []string `yaml:"contentStatuses,omitempty" json:"contentStatuses,omitempty"`
}

// BetaMetadataSpec preserves explicit managed fields; nil pointers mean omitted intent.
type BetaMetadataSpec struct {
	AppLocalizations map[string]BetaAppLocalizationSpec `yaml:"appLocalizations,omitempty" json:"appLocalizations,omitempty"`
	ReviewDetails    *BetaReviewDetailsSpec             `yaml:"reviewDetails,omitempty" json:"reviewDetails,omitempty"`
	Builds           []BetaBuildMetadataSpec            `yaml:"builds,omitempty" json:"builds,omitempty"`
}

// BetaAppLocalizationSpec preserves explicit managed fields; nil pointers mean omitted intent.
type BetaAppLocalizationSpec struct {
	Description       *string `yaml:"description,omitempty" json:"description,omitempty"`
	FeedbackEmail     *string `yaml:"feedbackEmail,omitempty" json:"feedbackEmail,omitempty"`
	MarketingURL      *string `yaml:"marketingUrl,omitempty" json:"marketingUrl,omitempty"`
	PrivacyPolicyURL  *string `yaml:"privacyPolicyUrl,omitempty" json:"privacyPolicyUrl,omitempty"`
	TVOSPrivacyPolicy *string `yaml:"tvOsPrivacyPolicy,omitempty" json:"tvOsPrivacyPolicy,omitempty"`
}

// BetaBuildLocalizationSpec preserves explicit managed fields; nil pointers mean omitted intent.
type BetaBuildLocalizationSpec struct {
	WhatsNew *string `yaml:"whatsNew,omitempty" json:"whatsNew,omitempty"`
}

// BetaBuildSelector preserves explicit managed fields; nil pointers mean omitted intent.
type BetaBuildSelector struct {
	Number   string `yaml:"number" json:"number"`
	Version  string `yaml:"version" json:"version"`
	Platform string `yaml:"platform" json:"platform"`
}

// BetaBuildMetadataSpec preserves explicit managed fields; nil pointers mean omitted intent.
type BetaBuildMetadataSpec struct {
	Build         BetaBuildSelector                    `yaml:"build" json:"build"`
	Localizations map[string]BetaBuildLocalizationSpec `yaml:"localizations,omitempty" json:"localizations,omitempty"`
}

// BetaReviewDetailsSpec preserves explicit managed fields; nil pointers mean omitted intent.
type BetaReviewDetailsSpec struct {
	ContactFirstName    *string `yaml:"contactFirstName,omitempty" json:"contactFirstName,omitempty"`
	ContactLastName     *string `yaml:"contactLastName,omitempty" json:"contactLastName,omitempty"`
	ContactEmail        *string `yaml:"contactEmail,omitempty" json:"contactEmail,omitempty"`
	ContactPhone        *string `yaml:"contactPhone,omitempty" json:"contactPhone,omitempty"`
	DemoAccountName     *string `yaml:"demoAccountName,omitempty" json:"demoAccountName,omitempty"`
	Notes               *string `yaml:"notes,omitempty" json:"notes,omitempty"`
	DemoAccountRequired *bool   `yaml:"demoAccountRequired,omitempty" json:"demoAccountRequired,omitempty"`
}
