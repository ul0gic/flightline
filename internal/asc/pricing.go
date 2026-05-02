package asc

// Apple's App Store pricing model has been reworked in v4.x. The relevant
// resources Skipper reads:
//
//   - AppPriceSchedule: the per-app schedule resource. Has a baseTerritory
//     (the territory that anchors price equalization) and two collections of
//     AppPriceV2 entries: manual (operator-set windows) and automatic
//     (equalized from the base territory).
//   - AppPriceV2: one entry on the schedule. Carries manual flag,
//     startDate/endDate window, and points at an AppPricePointV3 plus a
//     Territory.
//   - AppPricePointV3: the actual customer price + proceeds for a (territory,
//     tier) tuple. customerPrice and proceeds are decimal strings (Apple's
//     wire shape avoids float precision loss).
//   - AppAvailabilityV2: the per-app availability resource. Has
//     availableInNewTerritories plus a territoryAvailabilities relationship
//     (per-territory available/release-date/preorder data).
//
// AppPriceTier (v1 era) is deprecated in favor of AppPricePointV3; do not
// model it.
//
// Source:
//
//	jq '.components.schemas.AppPriceSchedule' openapi.oas.json
//	jq '.components.schemas.AppPriceV2.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.AppPricePointV3.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.AppAvailabilityV2.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.TerritoryAvailability.properties.attributes.properties' openapi.oas.json

// AppPriceScheduleAttributes is the subset of Apple's AppPriceSchedule
// attribute block Skipper reads. Apple's spec exposes no attributes on this
// resource — all interesting state lives in the relationships (baseTerritory,
// manualPrices, automaticPrices) — but we declare the struct so Get[T]
// decoding follows the rest of the codebase.
type AppPriceScheduleAttributes struct{}

// AppPriceAttributes is the subset of Apple's AppPriceV2.attributes Skipper
// reads. One entry on a price schedule's manual or automatic prices array.
//
// Source: jq '.components.schemas.AppPriceV2.properties.attributes.properties' openapi.oas.json
//
// Manual flag distinguishes operator-set windows (true) from equalized
// auto-prices (false). StartDate/EndDate define the window; an empty
// EndDate means the window runs indefinitely. The linked AppPricePoint
// (relationship) carries customerPrice and proceeds.
type AppPriceAttributes struct {
	Manual    *bool  `json:"manual,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

// AppPricePointAttributes is the subset of Apple's AppPricePointV3.attributes
// Skipper reads. The actual customer-facing price and developer proceeds for
// a (territory, tier) tuple.
//
// Source: jq '.components.schemas.AppPricePointV3.properties.attributes.properties' openapi.oas.json
//
// Both fields are decimal strings (e.g. "9.99"), not floats — Apple chose
// this on the wire to avoid float-precision drift across currencies. Treat
// as opaque tokens; do not parse as float64 in Skipper.
type AppPricePointAttributes struct {
	CustomerPrice string `json:"customerPrice,omitempty"`
	Proceeds      string `json:"proceeds,omitempty"`
}

// AppAvailabilityAttributes is the subset of Apple's AppAvailabilityV2
// attribute block Skipper reads. The single boolean controls whether Apple
// auto-releases the app in newly-onboarded territories; per-territory
// availability lives in the territoryAvailabilities relationship.
//
// Source: jq '.components.schemas.AppAvailabilityV2.properties.attributes.properties' openapi.oas.json
type AppAvailabilityAttributes struct {
	AvailableInNewTerritories *bool `json:"availableInNewTerritories,omitempty"`
}

// TerritoryAvailabilityAttributes is the subset of Apple's
// TerritoryAvailability.attributes Skipper reads. One entry per territory
// the app is configured for.
//
// Source: jq '.components.schemas.TerritoryAvailability.properties.attributes.properties' openapi.oas.json
//
// ContentStatuses is an array because Apple may flag multiple reasons a
// territory is in a non-AVAILABLE state (e.g. MISSING_RATING +
// CANNOT_SELL_NON_IOS_GAMES on the same row). The id of the resource is
// the territory id (USA, GBR, …) — surfaced via Resource.ID.
type TerritoryAvailabilityAttributes struct {
	Available           *bool    `json:"available,omitempty"`
	ReleaseDate         string   `json:"releaseDate,omitempty"`
	PreOrderEnabled     *bool    `json:"preOrderEnabled,omitempty"`
	PreOrderPublishDate string   `json:"preOrderPublishDate,omitempty"`
	ContentStatuses     []string `json:"contentStatuses,omitempty"`
}
