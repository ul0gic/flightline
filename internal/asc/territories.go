package asc

// TerritoryAttributes is the subset of Apple's Territory.attributes Flightline reads.
// The territory id (ISO 3166-1 alpha-3, e.g. "USA") is in Resource.ID; only Currency is in attributes.
type TerritoryAttributes struct {
	Currency string `json:"currency,omitempty"`
}
