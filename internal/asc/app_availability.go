package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// AppAvailability is an app's complete observed territory availability state.
type AppAvailability struct {
	ID                        string
	AvailableInNewTerritories *bool
	Territories               []TerritoryAvailability
}

// TerritoryAvailability is one territory's observed availability and release state.
type TerritoryAvailability struct {
	ID string
	TerritoryAvailabilityAttributes
	TerritoryID string
}

// ReadAppAvailability reads every territory availability associated with an app.
func ReadAppAvailability(ctx context.Context, c *Client, appID string) (AppAvailability, error) {
	query := url.Values{
		"fields[appAvailabilities]": {"availableInNewTerritories"},
	}
	availability, err := Get[Single[AppAvailabilityAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appAvailabilityV2", query)
	if err != nil {
		return AppAvailability{}, fmt.Errorf("asc: read app availability for app %q: %w", appID, err)
	}
	if availability.Data.ID == "" || availability.Data.Type != "appAvailabilities" {
		return AppAvailability{}, fmt.Errorf("asc: app availability response for app %q is incomplete or has unexpected type", appID)
	}

	out := AppAvailability{
		ID:                        availability.Data.ID,
		AvailableInNewTerritories: availability.Data.Attributes.AvailableInNewTerritories,
		Territories:               make([]TerritoryAvailability, 0),
	}
	territoryQuery := url.Values{
		"fields[territoryAvailabilities]": {"available,releaseDate,preOrderEnabled,preOrderPublishDate,contentStatuses,territory"},
		"limit":                           {"200"},
	}
	resourcePath := "/v2/appAvailabilities/" + url.PathEscape(out.ID) + "/territoryAvailabilities"
	seenTerritories := make(map[string]bool)
	for page, err := range Pages[TerritoryAvailabilityAttributes](ctx, c, resourcePath, territoryQuery) {
		if err != nil {
			return AppAvailability{}, fmt.Errorf("asc: list territory availabilities for app %q: %w", appID, err)
		}
		for _, resource := range page.Data {
			territoryID, err := territoryAvailabilityTerritoryID(resource)
			if err != nil {
				return AppAvailability{}, err
			}
			if seenTerritories[territoryID] {
				return AppAvailability{}, fmt.Errorf("asc: app availability for app %q has duplicate territory %q", appID, territoryID)
			}
			seenTerritories[territoryID] = true
			out.Territories = append(out.Territories, TerritoryAvailability{
				ID:                              resource.ID,
				TerritoryAvailabilityAttributes: resource.Attributes,
				TerritoryID:                     territoryID,
			})
		}
	}
	return out, nil
}

// PatchTerritoryAvailability updates a single documented territory availability attribute.
func PatchTerritoryAvailability(ctx context.Context, c *Client, territoryAvailabilityID, field string, value any) (Resource[TerritoryAvailabilityAttributes], error) {
	if territoryAvailabilityID == "" {
		return Resource[TerritoryAvailabilityAttributes]{}, errors.New("asc: territory availability id is required")
	}
	if field != "available" && field != "releaseDate" {
		return Resource[TerritoryAvailabilityAttributes]{}, fmt.Errorf("asc: unsupported territory availability field %q", field)
	}
	request := map[string]any{
		"data": map[string]any{
			"type":       "territoryAvailabilities",
			"id":         territoryAvailabilityID,
			"attributes": map[string]any{field: value},
		},
	}
	response, err := Patch[Single[TerritoryAvailabilityAttributes]](ctx, c, "/v1/territoryAvailabilities/"+url.PathEscape(territoryAvailabilityID), nil, request)
	if err != nil {
		return Resource[TerritoryAvailabilityAttributes]{}, fmt.Errorf("asc: update territory availability %q: %w", territoryAvailabilityID, err)
	}
	if response.Data.ID != territoryAvailabilityID || response.Data.Type != "territoryAvailabilities" {
		return Resource[TerritoryAvailabilityAttributes]{}, fmt.Errorf("asc: territory availability update response for %q is incomplete or inconsistent", territoryAvailabilityID)
	}
	return response.Data, nil
}

func territoryAvailabilityTerritoryID(resource Resource[TerritoryAvailabilityAttributes]) (string, error) {
	return territoryRelationshipID(resource.ID, resource.Type, "territoryAvailabilities", resource.Relationships)
}

func territoryRelationshipID(resourceID, resourceType, expectedResourceType string, relationships map[string]Relationship) (string, error) {
	if resourceID == "" || resourceType != expectedResourceType {
		return "", errors.New("asc: territory availability response is incomplete or has unexpected type")
	}
	relationship, ok := relationships["territory"]
	if !ok {
		return "", fmt.Errorf("asc: territory availability %q is missing territory relationship", resourceID)
	}
	var territory struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(relationship.Data, &territory); err != nil {
		return "", fmt.Errorf("asc: decode territory relationship for availability %q: %w", resourceID, err)
	}
	if territory.Type != "territories" || territory.ID == "" {
		return "", fmt.Errorf("asc: territory availability %q has incomplete territory relationship", resourceID)
	}
	return territory.ID, nil
}
