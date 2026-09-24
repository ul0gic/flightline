package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// applyPricingField applies one complete base-territory/price-point operation.
// G2 changes the dispatch registration to match /spec/pricing exactly.
func applyPricingField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if ch.Path != "/spec/pricing" || (ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate) {
		return fmt.Errorf("apply pricing: unsupported change %s %s", ch.Op, ch.Path)
	}
	pair, err := pricingChangePair(ch.To)
	if err != nil {
		return fmt.Errorf("apply pricing: target: %w", err)
	}
	if pair.BaseTerritory == nil || *pair.BaseTerritory == "" || pair.AppPricePointID == nil || *pair.AppPricePointID == "" {
		return errors.New("apply pricing: complete baseTerritory and appPricePointId pair required")
	}
	var expected *asc.ActiveBasePrice
	if ch.From != nil {
		before, err := pricingChangePair(ch.From)
		if err != nil {
			return fmt.Errorf("apply pricing: current pair: %w", err)
		}
		expected = &asc.ActiveBasePrice{}
		if before.BaseTerritory != nil {
			expected.TerritoryID = *before.BaseTerritory
		}
		if before.AppPricePointID != nil {
			expected.PricePointID = *before.AppPricePointID
		}
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	if err := asc.CreatePreservingPriceSchedule(ctx, c, appID, *pair.BaseTerritory, *pair.AppPricePointID, expected, time.Now()); err != nil {
		return fmt.Errorf("apply pricing: %w", err)
	}
	return nil
}

func pricingChangePair(value any) (config.PricingSpec, error) {
	buf, err := json.Marshal(value)
	if err != nil {
		return config.PricingSpec{}, err
	}
	var pair config.PricingSpec
	if err := json.Unmarshal(buf, &pair); err != nil {
		return config.PricingSpec{}, err
	}
	return pair, nil
}
