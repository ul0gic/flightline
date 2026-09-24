package state

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// FetchAppAvailability reads an app's observed availability without exposing ASC resource IDs in state.
func FetchAppAvailability(ctx context.Context, c *asc.Client, appID string) (*config.AppAvailabilitySpec, error) {
	availability, err := asc.ReadAppAvailability(ctx, c, appID)
	if err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	spec := &config.AppAvailabilitySpec{
		AvailableInNewTerritories: cloneBool(availability.AvailableInNewTerritories),
		Territories:               make(map[string]config.TerritoryAvailabilitySpec, len(availability.Territories)),
	}
	for _, territory := range availability.Territories {
		if _, exists := spec.Territories[territory.TerritoryID]; exists {
			return nil, fmt.Errorf("app availability has duplicate territory %q", territory.TerritoryID)
		}
		spec.Territories[territory.TerritoryID] = config.TerritoryAvailabilitySpec{
			Available:           cloneBool(territory.Available),
			ReleaseDate:         optionalAvailabilityDate(territory.ReleaseDate),
			PreOrderEnabled:     cloneBool(territory.PreOrderEnabled),
			PreOrderPublishDate: optionalAvailabilityDate(territory.PreOrderPublishDate),
			ContentStatuses:     slices.Clone(territory.ContentStatuses),
		}
	}
	return spec, nil
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func optionalAvailabilityDate(value string) *string {
	if value == "" {
		return nil
	}
	cloned := value
	return &cloned
}
