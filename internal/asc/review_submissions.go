package asc

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

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

// ReviewSubmissionItemReference identifies the resource represented by a review item.
type ReviewSubmissionItemReference struct {
	Type          string
	ID            string
	Discriminator int
	Canonical     bool
	Opaque        bool
}

// ResolveReviewSubmissionItemReference prefers JSON:API relationship data and
// falls back to Apple's encoded review-item ID when the relationship is absent.
func ResolveReviewSubmissionItemReference(itemID, submissionID string, rels map[string]Relationship) ReviewSubmissionItemReference {
	for _, rel := range rels {
		if len(rel.Data) == 0 || string(rel.Data) == "null" {
			continue
		}
		var ref struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(rel.Data, &ref); err == nil && ref.Type != "" && ref.ID != "" {
			return ReviewSubmissionItemReference{Type: ref.Type, ID: ref.ID, Canonical: true}
		}
	}

	parts, ok := decodeReviewSubmissionItemID(itemID)
	if !ok || (submissionID != "" && parts[0] != submissionID) {
		return ReviewSubmissionItemReference{Type: "UNKNOWN(MALFORMED)", Opaque: true}
	}
	discriminator, err := strconv.Atoi(parts[1])
	if err != nil || discriminator < 0 || parts[2] == "" {
		return ReviewSubmissionItemReference{Type: "UNKNOWN(MALFORMED)", Opaque: true}
	}

	ref := ReviewSubmissionItemReference{ID: parts[2], Discriminator: discriminator}
	switch discriminator {
	case 6:
		ref.Type = "appStoreVersions"
	case 17:
		ref.Type = "inAppPurchaseVersions"
		ref.Canonical = true
	default:
		ref.Type = "UNKNOWN(" + strconv.Itoa(discriminator) + ")"
		ref.Opaque = true
	}
	return ref
}

func decodeReviewSubmissionItemID(itemID string) ([3]string, bool) {
	var empty [3]string
	encodings := []*base64.Encoding{
		base64.RawStdEncoding,
		base64.StdEncoding,
		base64.RawURLEncoding,
		base64.URLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(itemID)
		if err != nil {
			continue
		}
		parts := strings.Split(string(decoded), "|")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
			continue
		}
		return [3]string{parts[0], parts[1], parts[2]}, true
	}
	return empty, false
}
