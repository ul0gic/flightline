package state

import (
	"context"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func fetchPricing(ctx context.Context, c *asc.Client, appID string) (*config.PricingSpec, error) {
	terr, pp, err := fetchPricingPair(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	if terr == "" && pp == "" {
		return nil, nil
	}
	out := &config.PricingSpec{}
	if terr != "" {
		out.BaseTerritory = &terr
	}
	if pp != "" {
		out.AppPricePointID = &pp
	}
	return out, nil
}
