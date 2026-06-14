package asc

const (
	ReviewSubmissionStateReadyForReview   = "READY_FOR_REVIEW"
	ReviewSubmissionStateWaitingForReview = "WAITING_FOR_REVIEW"
	ReviewSubmissionStateInReview         = "IN_REVIEW"
	ReviewSubmissionStateUnresolvedIssues = "UNRESOLVED_ISSUES"
	ReviewSubmissionStateCanceling        = "CANCELING"
	ReviewSubmissionStateCompleting       = "COMPLETING"
	ReviewSubmissionStateComplete         = "COMPLETE"

	// ReviewSubmissionItemState*: see ReviewSubmissionItemAttributes.State.
	// Source: jq '.components.schemas.ReviewSubmissionItem.properties.attributes.properties.state.enum' openapi.oas.json
	ReviewSubmissionItemStateReadyForReview = "READY_FOR_REVIEW"
	ReviewSubmissionItemStateAccepted       = "ACCEPTED"
	ReviewSubmissionItemStateApproved       = "APPROVED"
	ReviewSubmissionItemStateRejected       = "REJECTED"
	ReviewSubmissionItemStateRemoved        = "REMOVED"
)

// ReviewSubmissionAttributes is the subset of Apple's ReviewSubmission.attributes Flightline reads.
// /v1/reviewSubmissions is the modern flow; /v1/appStoreVersionSubmissions is deprecated and unused here.
type ReviewSubmissionAttributes struct {
	Platform      string `json:"platform,omitempty"`
	SubmittedDate string `json:"submittedDate,omitempty"`
	State         string `json:"state,omitempty"`
}

// ReviewSubmissionItemAttributes is the subset of Apple's ReviewSubmissionItem.attributes Flightline reads.
// What is being reviewed (version, custom page, experiment) is in the relationships block, not here.
type ReviewSubmissionItemAttributes struct {
	State string `json:"state,omitempty"`
}
