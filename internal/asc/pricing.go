package asc

// AppPriceScheduleAttributes is the subset of Apple's AppPriceSchedule attribute block Flightline reads.
// Apple exposes no attributes here; all state is in relationships (baseTerritory, manualPrices, automaticPrices).
type AppPriceScheduleAttributes struct{}

// AppPriceAttributes is the subset of Apple's AppPriceV2.attributes Flightline reads.
// Manual=true means operator-set; false means equalized. Empty EndDate = runs indefinitely.
type AppPriceAttributes struct {
	Manual    *bool  `json:"manual,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

// AppPricePointAttributes is the subset of Apple's AppPricePointV3.attributes Flightline reads.
// CustomerPrice and Proceeds are decimal strings, not floats: Apple's wire choice to avoid currency precision drift.
type AppPricePointAttributes struct {
	CustomerPrice string `json:"customerPrice,omitempty"`
	Proceeds      string `json:"proceeds,omitempty"`
}

// AppAvailabilityAttributes is the subset of Apple's AppAvailabilityV2 attribute block Flightline reads.
// AvailableInNewTerritories controls auto-release in newly onboarded territories; per-territory state is in relationships.
type AppAvailabilityAttributes struct {
	AvailableInNewTerritories *bool `json:"availableInNewTerritories,omitempty"`
}

// TerritoryAvailabilityAttributes is the subset of Apple's TerritoryAvailability.attributes Flightline reads.
// ContentStatuses is an array because Apple can set multiple blocking reasons simultaneously (e.g. MISSING_RATING + CANNOT_SELL_NON_IOS_GAMES).
type TerritoryAvailabilityAttributes struct {
	Available           *bool    `json:"available,omitempty"`
	ReleaseDate         string   `json:"releaseDate,omitempty"`
	PreOrderEnabled     *bool    `json:"preOrderEnabled,omitempty"`
	PreOrderPublishDate string   `json:"preOrderPublishDate,omitempty"`
	ContentStatuses     []string `json:"contentStatuses,omitempty"`
}
