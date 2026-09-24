package asc

import (
	"context"
	"errors"
	"fmt"
)

// EndAppAvailabilityPreOrders ends active pre-orders for the selected territory availability resources.
func EndAppAvailabilityPreOrders(ctx context.Context, c *Client, territoryAvailabilityIDs []string) (string, error) {
	if len(territoryAvailabilityIDs) == 0 {
		return "", errors.New("asc: at least one territory availability id is required")
	}
	linkages := make([]map[string]string, 0, len(territoryAvailabilityIDs))
	for _, id := range territoryAvailabilityIDs {
		if id == "" {
			return "", errors.New("asc: territory availability id is required")
		}
		linkages = append(linkages, map[string]string{"type": "territoryAvailabilities", "id": id})
	}
	request := map[string]any{
		"data": map[string]any{
			"type": "endAppAvailabilityPreOrders",
			"relationships": map[string]any{
				"territoryAvailabilities": map[string]any{"data": linkages},
			},
		},
	}
	response, err := Post[Single[EmptyAttributes]](ctx, c, "/v1/endAppAvailabilityPreOrders", nil, request)
	if err != nil {
		return "", fmt.Errorf("asc: end app pre-orders: %w", err)
	}
	if response.Data.ID == "" || response.Data.Type != "endAppAvailabilityPreOrders" {
		return "", errors.New("asc: end app pre-orders response is incomplete or has unexpected type")
	}
	return response.Data.ID, nil
}
