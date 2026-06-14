package asc

// AppCategoryAttributes is the subset of Apple's AppCategory.attributes Flightline reads.
// The category id (e.g. "GAMES") is on Resource.ID, not in attributes; hierarchy is in relationships.
type AppCategoryAttributes struct {
	Platforms []string `json:"platforms,omitempty"`
}
