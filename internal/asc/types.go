package asc

import "encoding/json"

// Resource is one JSON:API resource object: {type, id, attributes, relationships, links}. A is the per-resource attributes struct.
// Fields are tagged to match Apple's wire casing exactly (`bundleId`, not `bundle_id`): output is a stable contract.
type Resource[A any] struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id"`
	Attributes    A                       `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
	Links         ResourceLinks           `json:"links,omitempty"`
}

// Single is the single-resource response envelope: GET /v1/<resource>/{id}. Included holds side-loaded resources.
type Single[A any] struct {
	Data     Resource[A]       `json:"data"`
	Included []json.RawMessage `json:"included,omitempty"`
}

// Collection is the list response envelope: GET /v1/<resource>?...
// Links.Next is the cursor URL for paging; empty means the iterator is done.
type Collection[A any] struct {
	Data     []Resource[A]     `json:"data"`
	Links    PagedLinks        `json:"links"`
	Meta     CollectionMeta    `json:"meta,omitempty"`
	Included []json.RawMessage `json:"included,omitempty"`
}

// Relationship is one entry in a resource's relationships map (to-one or to-many, same envelope).
// Data is RawMessage to defer shape detection until the caller knows whether it's a ref or array of refs.
type Relationship struct {
	Links RelationshipLinks  `json:"links,omitempty"`
	Meta  CollectionMetaInfo `json:"meta,omitempty"`
	Data  json.RawMessage    `json:"data,omitempty"`
}

// ResourceLinks is the per-resource self link.
type ResourceLinks struct {
	Self string `json:"self,omitempty"`
}

// RelationshipLinks holds the self / related URLs of a relationship.
type RelationshipLinks struct {
	Self    string `json:"self,omitempty"`
	Related string `json:"related,omitempty"`
}

// PagedLinks holds the navigation URLs of a paginated collection.
// Next is empty on the last page.
type PagedLinks struct {
	Self  string `json:"self,omitempty"`
	First string `json:"first,omitempty"`
	Next  string `json:"next,omitempty"`
}

// CollectionMeta is the top-level meta block on a list response.
type CollectionMeta struct {
	Paging Paging `json:"paging,omitempty"`
}

// CollectionMetaInfo is the meta block that may appear inside a Relationship.
// Apple uses the same Paging shape there.
type CollectionMetaInfo struct {
	Paging Paging `json:"paging,omitempty"`
}

// Paging is Apple's paging info: total + limit + cursor.
type Paging struct {
	Total      int    `json:"total,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// EmptyAttributes is the attribute type for endpoints that return resources
// with no attributes block (rare; some relationship-only payloads).
type EmptyAttributes struct{}
