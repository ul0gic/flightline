package asc

// TerritoryAttributes is the subset of Apple's Territory.attributes Flightline
// reads. Territories are App Store regions (USA, GBR, JPN, …); the API
// exposes only the ISO 4217 currency code per territory.
//
// Source: jq '.components.schemas.Territory.properties.attributes.properties' openapi.oas.json
//
// Apple's `id` on territory resources is the ISO 3166-1 alpha-3 country code
// (e.g. "USA", "GBR") — surfaced via the JSON:API resource envelope's id
// field, not as an attribute. Stable across all apps and rarely changes;
// command-side caches are sound.
//
// Reused by Phase 3 write verbs (pricing/availability writes carry a
// territory id; the typed shape stays the same).
type TerritoryAttributes struct {
	Currency string `json:"currency,omitempty"`
}
