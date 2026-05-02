package asc

// Customer reviews read surface. Apple groups three resources here:
//
//   - CustomerReview: a star-rated user review (rating 1..5, title, body,
//     reviewerNickname, createdDate, territory).
//   - CustomerReviewResponse: the developer's response to a review (one per
//     review). Includes responseBody, lastModifiedDate, state.
//   - CustomerReviewSummarization: Apple's per-locale AI summary of recent
//     reviews. Per-app, per-locale, per-platform.
//
// Source:
//
//	jq '.components.schemas.CustomerReview.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.CustomerReviewResponse.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.CustomerReviewSummarization.properties.attributes.properties' openapi.oas.json
//
// Endpoints used:
//
//	GET /v1/apps/{id}/customerReviews                 — list, filterable
//	GET /v1/customerReviews/{id}                      — single, with response include
//	GET /v1/apps/{id}/customerReviewSummarizations    — Apple AI summary
//
// Reused by Phase 3 `reviews respond` write verbs; the response attribute
// shape stays the same on POST/PATCH against /v1/customerReviewResponses.

// Apple-defined enum values surfaced as named constants so command code can
// filter and compare without typos.
const (
	CustomerReviewResponseStatePublished      = "PUBLISHED"
	CustomerReviewResponseStatePendingPublish = "PENDING_PUBLISH"
)

// CustomerReviewAttributes is the subset of Apple's CustomerReview.attributes
// Skipper reads. Rating is 1..5 inclusive; territory is an ISO-3166 code via
// Apple's TerritoryCode enum (e.g. "USA", "GBR").
type CustomerReviewAttributes struct {
	Rating           int    `json:"rating,omitempty"`
	Title            string `json:"title,omitempty"`
	Body             string `json:"body,omitempty"`
	ReviewerNickname string `json:"reviewerNickname,omitempty"`
	CreatedDate      string `json:"createdDate,omitempty"`
	Territory        string `json:"territory,omitempty"`
}

// CustomerReviewResponseAttributes is the subset of Apple's
// CustomerReviewResponse.attributes Skipper reads. State is one of
// PUBLISHED | PENDING_PUBLISH.
type CustomerReviewResponseAttributes struct {
	ResponseBody     string `json:"responseBody,omitempty"`
	LastModifiedDate string `json:"lastModifiedDate,omitempty"`
	State            string `json:"state,omitempty"`
}

// CustomerReviewSummarizationAttributes is the subset of Apple's
// CustomerReviewSummarization.attributes Skipper reads. Apple authors one
// summarization per (app, locale, platform); Text is the AI-generated
// summary body in that locale.
type CustomerReviewSummarizationAttributes struct {
	CreatedDate string `json:"createdDate,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Text        string `json:"text,omitempty"`
}
