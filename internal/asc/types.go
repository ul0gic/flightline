package asc

import "encoding/json"

// Apple's App Store Connect API uses JSON:API-style envelopes. These generic
// containers cover every endpoint Skipper touches; per-resource files declare
// only the *Attributes struct (e.g. AppAttributes) carrying the fields Skipper
// actually reads or writes.
//
// Fields are tagged to match Apple's wire casing exactly (`bundleId`, not
// `bundle_id`). Output stability is a contract — see PRD § Output convention.

// Resource is one JSON:API resource object: the {type, id, attributes,
// relationships, links} quad. A is the per-resource attributes struct.
type Resource[A any] struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id"`
	Attributes    A                       `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
	Links         ResourceLinks           `json:"links,omitempty"`
}

// Single is the single-resource response envelope: GET /v1/<resource>/{id}.
// Included carries side-loaded resources of varying types; consumers decode
// individual entries on demand.
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

// Relationship is one entry in a resource's relationships map. Apple uses
// to-one and to-many shapes; both share the same envelope, with Data being
// either a single ref or an array of refs. We decode as RawMessage to defer
// shape detection until the caller knows what they expect.
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
