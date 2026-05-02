package asc

// AppCategoryAttributes is the subset of Apple's AppCategory.attributes
// Skipper reads. AppCategory resources are hierarchical: top-level categories
// (e.g. "Games", "Productivity") plus per-platform subcategories. The
// `platforms` array lists the platforms a category is valid on.
//
// Source: jq '.components.schemas.AppCategory.properties.attributes.properties' openapi.oas.json
//
// The id on a category resource is Apple's stable category key (e.g. "GAMES",
// "BUSINESS", "PHOTO_AND_VIDEO"); surfaced via Resource.ID, not as an
// attribute. Hierarchy lives in the `parent` and `subcategories`
// relationships, not on attributes.
//
// Reused by Phase 3 write verbs (`categories set`); the wire shape stays
// the same when assigning a primary/secondary category to an appInfo.
type AppCategoryAttributes struct {
	Platforms []string `json:"platforms,omitempty"`
}
