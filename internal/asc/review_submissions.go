package asc

// Review submission state enums (Apple-defined, not exhaustive in the
// language sense — values arrive as strings and we surface them verbatim).
//
// Listed here as named constants so command code can reference state values
// without typos and so readers can grep for the canonical set.
const (
	// ReviewSubmissionState* — see ReviewSubmissionAttributes.State.
	// Source: jq '.components.schemas.ReviewSubmission.properties.attributes.properties.state.enum' openapi.oas.json
	ReviewSubmissionStateReadyForReview   = "READY_FOR_REVIEW"
	ReviewSubmissionStateWaitingForReview = "WAITING_FOR_REVIEW"
	ReviewSubmissionStateInReview         = "IN_REVIEW"
	ReviewSubmissionStateUnresolvedIssues = "UNRESOLVED_ISSUES"
	ReviewSubmissionStateCanceling        = "CANCELING"
	ReviewSubmissionStateCompleting       = "COMPLETING"
	ReviewSubmissionStateComplete         = "COMPLETE"

	// ReviewSubmissionItemState* — see ReviewSubmissionItemAttributes.State.
	// Source: jq '.components.schemas.ReviewSubmissionItem.properties.attributes.properties.state.enum' openapi.oas.json
	ReviewSubmissionItemStateReadyForReview = "READY_FOR_REVIEW"
	ReviewSubmissionItemStateAccepted       = "ACCEPTED"
	ReviewSubmissionItemStateApproved       = "APPROVED"
	ReviewSubmissionItemStateRejected       = "REJECTED"
	ReviewSubmissionItemStateRemoved        = "REMOVED"
)

// ReviewSubmissionAttributes is the subset of Apple's
// ReviewSubmission.attributes Skipper reads.
//
// Source: jq '.components.schemas.ReviewSubmission.properties.attributes.properties' openapi.oas.json
//
// /v1/reviewSubmissions is the modern flow. /v1/appStoreVersionSubmissions
// is deprecated by Apple; Skipper uses the modern endpoint exclusively.
type ReviewSubmissionAttributes struct {
	Platform      string `json:"platform,omitempty"`
	SubmittedDate string `json:"submittedDate,omitempty"`
	State         string `json:"state,omitempty"`
}

// ReviewSubmissionItemAttributes is the subset of Apple's
// ReviewSubmissionItem.attributes Skipper reads.
//
// Source: jq '.components.schemas.ReviewSubmissionItem.properties.attributes.properties' openapi.oas.json
//
// Items reference the actual review surface (a version, a custom product
// page, an experiment, etc.) via the relationships block. Skipper surfaces
// the relationship type + id so consumers know what's being reviewed.
type ReviewSubmissionItemAttributes struct {
	State string `json:"state,omitempty"`
}
