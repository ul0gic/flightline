package state

import (
	"context"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
)

// fetchPricingPair preserves read errors for callers that need the coupled price fields.
func fetchPricingPair(ctx context.Context, c *asc.Client, appID string) (territory, pricePoint string, err error) {
	pair, err := asc.ReadActiveBasePrice(ctx, c, appID, time.Now())
	if err != nil {
		return "", "", err
	}
	return pair.TerritoryID, pair.PricePointID, nil
}
