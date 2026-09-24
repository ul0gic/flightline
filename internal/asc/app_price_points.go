package asc

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// AppPricePoint is a price point with its owning territory.
type AppPricePoint struct {
	ID string
	AppPricePointAttributes
	TerritoryID string
}

// ListAppPricePoints reads every price point available to an app, optionally for one territory.
func ListAppPricePoints(ctx context.Context, c *Client, appID, territoryID string) ([]AppPricePoint, error) {
	resourcePath := "/v1/apps/" + url.PathEscape(appID) + "/appPricePoints"
	return listAppPricePoints(ctx, c, resourcePath, territoryID)
}

// ListAppPricePointEqualizations reads every equalized price point for a source price point.
func ListAppPricePointEqualizations(ctx context.Context, c *Client, pricePointID, territoryID string) ([]AppPricePoint, error) {
	resourcePath := "/v3/appPricePoints/" + url.PathEscape(pricePointID) + "/equalizations"
	return listAppPricePoints(ctx, c, resourcePath, territoryID)
}

func listAppPricePoints(ctx context.Context, c *Client, resourcePath, territoryID string) ([]AppPricePoint, error) {
	query := url.Values{
		"fields[appPricePoints]": {"customerPrice,proceeds,territory"},
		"limit":                  {"200"},
	}
	if territoryID = strings.TrimSpace(territoryID); territoryID != "" {
		query.Set("filter[territory]", territoryID)
	}
	points := make([]AppPricePoint, 0)
	for page, err := range Pages[AppPricePointAttributes](ctx, c, resourcePath, query) {
		if err != nil {
			return nil, fmt.Errorf("asc: list app price points: %w", err)
		}
		for _, resource := range page.Data {
			territoryID, err := appPricePointTerritoryID(resource)
			if err != nil {
				return nil, err
			}
			points = append(points, AppPricePoint{
				ID:                      resource.ID,
				AppPricePointAttributes: resource.Attributes,
				TerritoryID:             territoryID,
			})
		}
	}
	return points, nil
}

func appPricePointTerritoryID(resource Resource[AppPricePointAttributes]) (string, error) {
	return territoryRelationshipID(resource.ID, resource.Type, "appPricePoints", resource.Relationships)
}
