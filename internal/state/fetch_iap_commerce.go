package state

import (
	"context"
	"slices"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// FetchIAPCommerce observes current IAP pricing and the complete availability
// territory set. Historical and future price windows stay in the L1 reader.
func FetchIAPCommerce(ctx context.Context, c *asc.Client, iapID string) (*config.IAPCommerceSpec, error) {
	schedule, err := asc.ReadIAPPriceSchedule(ctx, c, iapID)
	if err != nil {
		return nil, err
	}
	availability, err := asc.ReadIAPAvailability(ctx, c, iapID)
	if err != nil {
		return nil, err
	}
	commerce := &config.IAPCommerceSpec{}
	if schedule.ID != "" {
		commerce.Pricing, err = currentIAPPrice(schedule, time.Now().UTC())
		if err != nil {
			return nil, err
		}
	}
	if availability.ID != "" {
		territories := slices.Clone(availability.TerritoryIDs)
		slices.Sort(territories)
		commerce.Availability = &config.IAPAvailabilitySpec{
			AvailableInNewTerritories: availability.AvailableInNewTerritories,
			AvailableTerritories:      &territories,
		}
	}
	if commerce.Pricing == nil && commerce.Availability == nil {
		return nil, nil
	}
	return commerce, nil
}

func currentIAPPrice(schedule asc.IAPPriceSchedule, at time.Time) (*config.IAPPriceSpec, error) {
	selected, err := schedule.ActiveBasePrice(at)
	if err != nil {
		return nil, err
	}
	if selected.PricePointID == "" {
		return nil, nil
	}
	return &config.IAPPriceSpec{BaseTerritory: selected.BaseTerritoryID, PricePointID: selected.PricePointID}, nil
}
