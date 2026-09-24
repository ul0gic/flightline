package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CustomerReviewResponse is a response whose parent review was verified through
// the requested app's complete review collection.
type CustomerReviewResponse struct {
	ID       string `json:"id"`
	ReviewID string `json:"reviewId"`
	CustomerReviewResponseAttributes
}

type relatedReviewResponseEnvelope struct {
	Data *Resource[CustomerReviewResponseAttributes] `json:"data"`
}

// VerifyCustomerReviewOwnership reads every page because the direct review
// resource does not expose an app relationship.
func VerifyCustomerReviewOwnership(ctx context.Context, c *Client, appID, reviewID string) error {
	if appID == "" || reviewID == "" {
		return errors.New("app and customer review IDs are required")
	}
	found := false
	path := "/v1/apps/" + url.PathEscape(appID) + "/customerReviews"
	for page, err := range Pages[CustomerReviewAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return fmt.Errorf("verify customer review ownership: %w", err)
		}
		for index := range page.Data {
			row := &page.Data[index]
			if row.Type != "customerReviews" || row.ID == "" {
				return errors.New("app customer review collection contains invalid resource")
			}
			if row.ID != reviewID {
				continue
			}
			if found {
				return fmt.Errorf("app %s has duplicate customer review %s", appID, reviewID)
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("customer review %s does not belong to app %s", reviewID, appID)
	}
	return nil
}

func ReadCustomerReviewResponse(ctx context.Context, c *Client, appID, reviewID string) (*CustomerReviewResponse, error) {
	if err := VerifyCustomerReviewOwnership(ctx, c, appID, reviewID); err != nil {
		return nil, err
	}
	return readRelatedCustomerReviewResponse(ctx, c, reviewID)
}

func readRelatedCustomerReviewResponse(ctx context.Context, c *Client, reviewID string) (*CustomerReviewResponse, error) {
	response, err := Get[relatedReviewResponseEnvelope](ctx, c, "/v1/customerReviews/"+url.PathEscape(reviewID)+"/response", nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("read customer review response: %w", err)
	}
	if response.Data == nil {
		return nil, nil
	}
	return projectCustomerReviewResponse(*response.Data, reviewID)
}

func projectCustomerReviewResponse(row Resource[CustomerReviewResponseAttributes], reviewID string) (*CustomerReviewResponse, error) {
	if row.Type != "customerReviewResponses" || row.ID == "" || row.Attributes.State == "" {
		return nil, errors.New("customer review response has invalid type, ID, or state")
	}
	if row.Attributes.State != CustomerReviewResponseStatePublished && row.Attributes.State != CustomerReviewResponseStatePendingPublish {
		return nil, fmt.Errorf("customer review response %s has unknown state %q", row.ID, row.Attributes.State)
	}
	if err := verifyResponseReviewRelationship(row.Relationships, reviewID); err != nil {
		return nil, fmt.Errorf("customer review response %s: %w", row.ID, err)
	}
	return &CustomerReviewResponse{ID: row.ID, ReviewID: reviewID, CustomerReviewResponseAttributes: row.Attributes}, nil
}

func verifyResponseReviewRelationship(relationships map[string]Relationship, reviewID string) error {
	if rel, ok := relationships["review"]; ok && len(rel.Data) != 0 && string(rel.Data) != "null" {
		var ref struct{ Type, ID string }
		if err := json.Unmarshal(rel.Data, &ref); err != nil || ref.Type != "customerReviews" || ref.ID != reviewID {
			return errors.New("response belongs to a different review")
		}
	}
	return nil
}

func CreateCustomerReviewResponse(ctx context.Context, c *Client, appID, reviewID, body string) (CustomerReviewResponse, bool, error) {
	if strings.TrimSpace(body) == "" {
		return CustomerReviewResponse{}, false, errors.New("customer review response body is required")
	}
	current, err := ReadCustomerReviewResponse(ctx, c, appID, reviewID)
	if err != nil {
		return CustomerReviewResponse{}, false, err
	}
	if current != nil {
		if current.ResponseBody == body {
			return *current, false, nil
		}
		return CustomerReviewResponse{}, false, fmt.Errorf("customer review %s already has a different response %s; delete it explicitly before creating another", reviewID, current.ID)
	}
	request := map[string]any{"data": map[string]any{"type": "customerReviewResponses", "attributes": map[string]any{"responseBody": body}, "relationships": map[string]any{"review": map[string]any{"data": map[string]string{"type": "customerReviews", "id": reviewID}}}}}
	created, err := Post[Single[CustomerReviewResponseAttributes]](ctx, c, "/v1/customerReviewResponses", nil, request)
	if err != nil {
		return CustomerReviewResponse{}, false, confirmReviewResponseAfterPostError(ctx, c, reviewID, body, err)
	}
	if err := validateCreatedReviewResponse(created.Data, reviewID, body); err != nil {
		return CustomerReviewResponse{}, false, err
	}
	verified, err := readRelatedCustomerReviewResponse(ctx, c, reviewID)
	if err != nil || verified == nil || verified.ID != created.Data.ID || verified.ResponseBody != body {
		return CustomerReviewResponse{}, false, fmt.Errorf("customer review response POST outcome is uncertain; inspect review %s before retrying", reviewID)
	}
	return *verified, true, nil
}

func validateCreatedReviewResponse(row Resource[CustomerReviewResponseAttributes], reviewID, body string) error {
	if row.Type != "customerReviewResponses" || row.ID == "" {
		return errors.New("customer review response POST returned invalid identity; inspect before retrying")
	}
	if err := verifyResponseReviewRelationship(row.Relationships, reviewID); err != nil {
		return fmt.Errorf("customer review response POST returned wrong review; inspect before retrying: %w", err)
	}
	if row.Attributes.ResponseBody != "" && row.Attributes.ResponseBody != body {
		return errors.New("customer review response POST returned a different body; inspect before retrying")
	}
	if state := row.Attributes.State; state != "" && state != CustomerReviewResponseStatePendingPublish && state != CustomerReviewResponseStatePublished {
		return errors.New("customer review response POST returned unknown state; inspect before retrying")
	}
	return nil
}

func confirmReviewResponseAfterPostError(ctx context.Context, c *Client, reviewID, body string, postErr error) error {
	observed, readErr := readRelatedCustomerReviewResponse(ctx, c, reviewID)
	if readErr == nil && observed != nil && observed.ResponseBody == body {
		return fmt.Errorf("customer review response POST reported failure but response %s is now present; inspect before retrying: %w", observed.ID, postErr)
	}
	return fmt.Errorf("customer review response POST outcome is uncertain; inspect review %s before retrying: %w", reviewID, postErr)
}

func DeleteCustomerReviewResponse(ctx context.Context, c *Client, appID, reviewID, responseID string) (bool, error) {
	if responseID == "" {
		return false, errors.New("response ID is required for deletion")
	}
	current, err := ReadCustomerReviewResponse(ctx, c, appID, reviewID)
	if err != nil {
		return false, err
	}
	if current == nil {
		return false, nil
	}
	if current.ID != responseID {
		return false, fmt.Errorf("review %s has response %s, not %s; no deletion occurred", reviewID, current.ID, responseID)
	}
	if err := c.Delete(ctx, "/v1/customerReviewResponses/"+url.PathEscape(responseID), nil); err != nil {
		observed, readErr := readRelatedCustomerReviewResponse(ctx, c, reviewID)
		if readErr == nil && observed == nil {
			return false, fmt.Errorf("customer review response DELETE reported failure but response is now absent; inspect before retrying: %w", err)
		}
		return false, fmt.Errorf("customer review response DELETE outcome is uncertain; inspect before retrying: %w", err)
	}
	return true, nil
}
