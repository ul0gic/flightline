package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// AppStoreVersionPhasedRelease is the observed rollout state for one app version.
type AppStoreVersionPhasedRelease struct {
	ID string `json:"id"`
	AppStoreVersionPhasedReleaseAttributes
}

// AppStoreVersionPhasedReleaseAttributes contains the documented rollout fields.
type AppStoreVersionPhasedReleaseAttributes struct {
	PhasedReleaseState string `json:"phasedReleaseState,omitempty"`
	StartDate          string `json:"startDate,omitempty"`
	TotalPauseDuration *int   `json:"totalPauseDuration,omitempty"`
	CurrentDayNumber   *int   `json:"currentDayNumber,omitempty"`
}

// ReadAppStoreVersionPhasedRelease reads the rollout configuration for an app version.
func ReadAppStoreVersionPhasedRelease(ctx context.Context, c *Client, versionID string) (*AppStoreVersionPhasedRelease, error) {
	if versionID == "" {
		return nil, errors.New("asc: app store version ID is required")
	}
	query := url.Values{"fields[appStoreVersionPhasedReleases]": {"phasedReleaseState,startDate,totalPauseDuration,currentDayNumber"}}
	response, err := Get[Single[AppStoreVersionPhasedReleaseAttributes]](ctx, c, "/v1/appStoreVersions/"+url.PathEscape(versionID)+"/appStoreVersionPhasedRelease", query)
	if err != nil {
		return nil, fmt.Errorf("asc: read phased release for version %q: %w", versionID, err)
	}
	if response.Data.ID == "" || response.Data.Type != "appStoreVersionPhasedReleases" {
		return nil, fmt.Errorf("asc: phased release response for version %q is incomplete or inconsistent", versionID)
	}
	if !validPhasedReleaseState(response.Data.Attributes.PhasedReleaseState) {
		return nil, fmt.Errorf("asc: phased release response for version %q has unknown state %q", versionID, response.Data.Attributes.PhasedReleaseState)
	}
	return &AppStoreVersionPhasedRelease{ID: response.Data.ID, AppStoreVersionPhasedReleaseAttributes: response.Data.Attributes}, nil
}

func validPhasedReleaseState(state string) bool {
	switch state {
	case "INACTIVE", "ACTIVE", "PAUSED", "COMPLETE":
		return true
	default:
		return false
	}
}

// EnableAppStoreVersionPhasedRelease creates an inactive phased-release plan for a version.
func EnableAppStoreVersionPhasedRelease(ctx context.Context, c *Client, versionID string) (*AppStoreVersionPhasedRelease, error) {
	if versionID == "" {
		return nil, errors.New("asc: app store version ID is required")
	}
	request := map[string]any{"data": map[string]any{
		"type":          "appStoreVersionPhasedReleases",
		"attributes":    map[string]any{"phasedReleaseState": "INACTIVE"},
		"relationships": map[string]any{"appStoreVersion": map[string]any{"data": map[string]string{"type": "appStoreVersions", "id": versionID}}},
	}}
	return createOrUpdatePhasedRelease(ctx, c, "/v1/appStoreVersionPhasedReleases", request, versionID, "create")
}

// PauseAppStoreVersionPhasedRelease pauses an active rollout.
func PauseAppStoreVersionPhasedRelease(ctx context.Context, c *Client, releaseID string) (*AppStoreVersionPhasedRelease, error) {
	return updatePhasedReleaseState(ctx, c, releaseID, "PAUSED")
}

// ResumeAppStoreVersionPhasedRelease resumes a paused rollout.
func ResumeAppStoreVersionPhasedRelease(ctx context.Context, c *Client, releaseID string) (*AppStoreVersionPhasedRelease, error) {
	return updatePhasedReleaseState(ctx, c, releaseID, "ACTIVE")
}

func updatePhasedReleaseState(ctx context.Context, c *Client, releaseID, state string) (*AppStoreVersionPhasedRelease, error) {
	if releaseID == "" {
		return nil, errors.New("asc: phased release ID is required")
	}
	request := map[string]any{"data": map[string]any{
		"type": "appStoreVersionPhasedReleases", "id": releaseID,
		"attributes": map[string]any{"phasedReleaseState": state},
	}}
	return createOrUpdatePhasedRelease(ctx, c, "/v1/appStoreVersionPhasedReleases/"+url.PathEscape(releaseID), request, releaseID, "update")
}

func createOrUpdatePhasedRelease(ctx context.Context, c *Client, path string, request any, expectedID, action string) (*AppStoreVersionPhasedRelease, error) {
	var response Single[AppStoreVersionPhasedReleaseAttributes]
	var err error
	if action == "create" {
		response, err = Post[Single[AppStoreVersionPhasedReleaseAttributes]](ctx, c, path, nil, request)
	} else {
		response, err = Patch[Single[AppStoreVersionPhasedReleaseAttributes]](ctx, c, path, nil, request)
	}
	if err != nil {
		return nil, fmt.Errorf("asc: %s phased release: %w", action, err)
	}
	if response.Data.ID == "" || response.Data.Type != "appStoreVersionPhasedReleases" || action == "update" && response.Data.ID != expectedID {
		return nil, fmt.Errorf("asc: %s phased release response is incomplete or inconsistent", action)
	}
	return &AppStoreVersionPhasedRelease{ID: response.Data.ID, AppStoreVersionPhasedReleaseAttributes: response.Data.Attributes}, nil
}
