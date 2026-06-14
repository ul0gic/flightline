package asc

// Apple-defined enum values surfaced as named constants so command code can
// filter and compare without typos.
const (
	CustomerReviewResponseStatePublished      = "PUBLISHED"
	CustomerReviewResponseStatePendingPublish = "PENDING_PUBLISH"
)

// CustomerReviewAttributes is the subset of Apple's CustomerReview.attributes Flightline reads.
// Rating is 1..5; Territory is an ISO 3166-1 alpha-3 code (e.g. "USA", "GBR").
type CustomerReviewAttributes struct {
	Rating           int    `json:"rating,omitempty"`
	Title            string `json:"title,omitempty"`
	Body             string `json:"body,omitempty"`
	ReviewerNickname string `json:"reviewerNickname,omitempty"`
	CreatedDate      string `json:"createdDate,omitempty"`
	Territory        string `json:"territory,omitempty"`
}

// CustomerReviewResponseAttributes is the subset of Apple's CustomerReviewResponse.attributes Flightline reads.
type CustomerReviewResponseAttributes struct {
	ResponseBody     string `json:"responseBody,omitempty"`
	LastModifiedDate string `json:"lastModifiedDate,omitempty"`
	State            string `json:"state,omitempty"`
}

// CustomerReviewSummarizationAttributes is the subset of Apple's CustomerReviewSummarization.attributes Flightline reads.
// Apple authors one per (app, locale, platform); Text is the AI-generated summary in that locale.
type CustomerReviewSummarizationAttributes struct {
	CreatedDate string `json:"createdDate,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Text        string `json:"text,omitempty"`
}
