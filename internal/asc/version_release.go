package asc

import (
	"context"
	"errors"
	"fmt"
)

// AppStoreVersionReleaseRequest is the accepted request to release a manual version.
type AppStoreVersionReleaseRequest struct {
	ID string `json:"id"`
}

// CreateAppStoreVersionReleaseRequest requests release of an eligible manual version.
func CreateAppStoreVersionReleaseRequest(ctx context.Context, c *Client, versionID string) (*AppStoreVersionReleaseRequest, error) {
	if versionID == "" {
		return nil, errors.New("asc: app store version ID is required")
	}
	request := map[string]any{"data": map[string]any{
		"type":          "appStoreVersionReleaseRequests",
		"relationships": map[string]any{"appStoreVersion": map[string]any{"data": map[string]string{"type": "appStoreVersions", "id": versionID}}},
	}}
	response, err := Post[Single[struct{}]](ctx, c, "/v1/appStoreVersionReleaseRequests", nil, request)
	if err != nil {
		return nil, fmt.Errorf("asc: create app store version release request: %w", err)
	}
	if response.Data.ID == "" || response.Data.Type != "appStoreVersionReleaseRequests" {
		return nil, errors.New("asc: version release request response is incomplete or inconsistent")
	}
	return &AppStoreVersionReleaseRequest{ID: response.Data.ID}, nil
}
