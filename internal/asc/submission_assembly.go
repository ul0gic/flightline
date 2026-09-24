package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// SubmissionAssembly is the minimal, verified review-submission projection
// used by the assembly workflow.
type SubmissionAssembly struct {
	ID       string
	AppID    string
	Platform string
	State    string
}

// SubmissionAssemblyItem is an item whose reviewed resource was identified
// from exactly one documented relationship.
type SubmissionAssemblyItem struct {
	ID       string
	State    string
	Proposal SubmissionItemProposal
}

// ListSubmissionAssemblies lists every review submission belonging to appID.
func ListSubmissionAssemblies(ctx context.Context, c *Client, appID string) ([]SubmissionAssembly, error) {
	if appID == "" {
		return nil, errors.New("asc: app ID is required")
	}
	q := url.Values{"filter[app]": {appID}, "limit": {"200"}}
	out := make([]SubmissionAssembly, 0)
	for page, err := range Pages[ReviewSubmissionAttributes](ctx, c, "/v1/reviewSubmissions", q) {
		if err != nil {
			return nil, fmt.Errorf("asc: list review submissions: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "reviewSubmissions" || row.ID == "" || !validReviewSubmissionState(row.Attributes.State) {
				return nil, errors.New("asc: review submission response has incomplete identity or state")
			}
			out = append(out, SubmissionAssembly{ID: row.ID, AppID: appID, Platform: row.Attributes.Platform, State: row.Attributes.State})
		}
	}
	return out, nil
}

// CreateSubmissionAssembly creates a new modern review submission for one app.
func CreateSubmissionAssembly(ctx context.Context, c *Client, appID, platform string) (SubmissionAssembly, error) {
	if appID == "" {
		return SubmissionAssembly{}, errors.New("asc: app ID is required")
	}
	data := map[string]any{
		"type":          "reviewSubmissions",
		"relationships": map[string]any{"app": map[string]any{"data": map[string]string{"type": "apps", "id": appID}}},
	}
	if platform != "" {
		data["attributes"] = map[string]string{"platform": platform}
	}
	body := map[string]any{"data": data}
	response, err := Post[Single[ReviewSubmissionAttributes]](ctx, c, "/v1/reviewSubmissions", nil, body)
	if err != nil {
		return SubmissionAssembly{}, fmt.Errorf("asc: create review submission: %w", err)
	}
	row := response.Data
	if row.Type != "reviewSubmissions" || row.ID == "" || !validReviewSubmissionState(row.Attributes.State) {
		return SubmissionAssembly{}, errors.New("asc: created review submission response is incomplete or inconsistent")
	}
	return SubmissionAssembly{ID: row.ID, AppID: appID, Platform: row.Attributes.Platform, State: row.Attributes.State}, nil
}

// ListSubmissionAssemblyItems lists every item and refuses an item whose
// reviewed resource is absent, ambiguous, malformed, or unsupported.
func ListSubmissionAssemblyItems(ctx context.Context, c *Client, submissionID string) ([]SubmissionAssemblyItem, error) {
	if submissionID == "" {
		return nil, errors.New("asc: review submission ID is required")
	}
	q := url.Values{
		"limit":                         {"200"},
		"fields[reviewSubmissionItems]": {"state,appStoreVersion,inAppPurchaseVersion,appEvent,appStoreVersionExperimentV2"},
		"include":                       {"appStoreVersion,inAppPurchaseVersion,appEvent,appStoreVersionExperimentV2"},
	}
	path := "/v1/reviewSubmissions/" + url.PathEscape(submissionID) + "/items"
	out := make([]SubmissionAssemblyItem, 0)
	for page, err := range Pages[ReviewSubmissionItemAttributes](ctx, c, path, q) {
		if err != nil {
			return nil, fmt.Errorf("asc: list review submission items: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "reviewSubmissionItems" || row.ID == "" || !validReviewSubmissionItemState(row.Attributes.State) {
				return nil, errors.New("asc: review submission item response has incomplete identity or state")
			}
			proposal, err := submissionItemProposalFromRelationships(row.Relationships)
			if err != nil {
				return nil, fmt.Errorf("asc: review submission item %q: %w", row.ID, err)
			}
			out = append(out, SubmissionAssemblyItem{ID: row.ID, State: row.Attributes.State, Proposal: proposal})
		}
	}
	return out, nil
}

func submissionItemProposalFromRelationships(rels map[string]Relationship) (SubmissionItemProposal, error) {
	var found []SubmissionItemProposal
	for relationship, rel := range rels {
		if len(rel.Data) == 0 || string(rel.Data) == "null" {
			continue
		}
		if relationship != "appStoreVersion" && relationship != "inAppPurchaseVersion" && relationship != "appEvent" && relationship != "appStoreVersionExperimentV2" {
			return SubmissionItemProposal{}, fmt.Errorf("unsupported non-null %s relationship", relationship)
		}
		var ref struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(rel.Data, &ref); err != nil {
			return SubmissionItemProposal{}, fmt.Errorf("invalid %s relationship", relationship)
		}
		proposal := SubmissionItemProposal{Relationship: relationship, Type: ref.Type, ID: ref.ID}
		if err := proposal.Validate(); err != nil {
			return SubmissionItemProposal{}, err
		}
		found = append(found, proposal)
	}
	if len(found) != 1 {
		return SubmissionItemProposal{}, errors.New("requires exactly one supported reviewed-resource relationship")
	}
	return found[0], nil
}

// AddSubmissionAssemblyItem attaches one verified resource to a review
// submission. The caller must inspect membership after a write failure.
func AddSubmissionAssemblyItem(ctx context.Context, c *Client, submissionID string, proposal SubmissionItemProposal) (SubmissionAssemblyItem, error) {
	if submissionID == "" {
		return SubmissionAssemblyItem{}, errors.New("asc: review submission ID is required")
	}
	if err := proposal.Validate(); err != nil {
		return SubmissionAssemblyItem{}, err
	}
	body := map[string]any{"data": map[string]any{
		"type": "reviewSubmissionItems",
		"relationships": map[string]any{
			"reviewSubmission":    map[string]any{"data": map[string]string{"type": "reviewSubmissions", "id": submissionID}},
			proposal.Relationship: map[string]any{"data": map[string]string{"type": proposal.Type, "id": proposal.ID}},
		},
	}}
	response, err := Post[Single[ReviewSubmissionItemAttributes]](ctx, c, "/v1/reviewSubmissionItems", nil, body)
	if err != nil {
		return SubmissionAssemblyItem{}, fmt.Errorf("asc: attach review submission item: %w", err)
	}
	row := response.Data
	if row.Type != "reviewSubmissionItems" || row.ID == "" || !validReviewSubmissionItemState(row.Attributes.State) {
		return SubmissionAssemblyItem{}, errors.New("asc: attached review submission item response is incomplete or inconsistent")
	}
	got, err := submissionItemProposalFromRelationships(row.Relationships)
	if err != nil || got != proposal {
		return SubmissionAssemblyItem{}, errors.New("asc: attached review submission item relationship was not confirmed")
	}
	return SubmissionAssemblyItem{ID: row.ID, State: row.Attributes.State, Proposal: got}, nil
}

// SubmitSubmissionAssembly is deliberately separate from assembly.
func SubmitSubmissionAssembly(ctx context.Context, c *Client, submissionID string) (SubmissionAssembly, error) {
	if submissionID == "" {
		return SubmissionAssembly{}, errors.New("asc: review submission ID is required")
	}
	body := map[string]any{"data": map[string]any{"type": "reviewSubmissions", "id": submissionID, "attributes": map[string]bool{"submitted": true}}}
	response, err := Patch[Single[ReviewSubmissionAttributes]](ctx, c, "/v1/reviewSubmissions/"+url.PathEscape(submissionID), nil, body)
	if err != nil {
		return SubmissionAssembly{}, fmt.Errorf("asc: submit review submission: %w", err)
	}
	row := response.Data
	if row.Type != "reviewSubmissions" || row.ID != submissionID || !submittedReviewSubmissionState(row.Attributes.State) {
		return SubmissionAssembly{}, errors.New("asc: submitted review submission response is incomplete or inconsistent")
	}
	return SubmissionAssembly{ID: row.ID, Platform: row.Attributes.Platform, State: row.Attributes.State}, nil
}

func validReviewSubmissionState(state string) bool {
	switch state {
	case ReviewSubmissionStateReadyForReview, ReviewSubmissionStateWaitingForReview, ReviewSubmissionStateInReview,
		ReviewSubmissionStateUnresolvedIssues, ReviewSubmissionStateCanceling, ReviewSubmissionStateCompleting,
		ReviewSubmissionStateComplete:
		return true
	default:
		return false
	}
}

func validReviewSubmissionItemState(state string) bool {
	switch state {
	case ReviewSubmissionItemStateReadyForReview, ReviewSubmissionItemStateAccepted, ReviewSubmissionItemStateApproved,
		ReviewSubmissionItemStateRejected, ReviewSubmissionItemStateRemoved:
		return true
	default:
		return false
	}
}

func submittedReviewSubmissionState(state string) bool {
	switch state {
	case ReviewSubmissionStateWaitingForReview, ReviewSubmissionStateInReview:
		return true
	default:
		return false
	}
}
