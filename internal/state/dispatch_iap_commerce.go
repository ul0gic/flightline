package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func ValidateIAPCommerceChange(ch plan.Change) error {
	_, leaf, err := iapCommercePath(ch.Path)
	if err != nil {
		return errUnmapped(ch)
	}
	if ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate {
		return fmt.Errorf("IAP commerce %s does not support %s", leaf, ch.Op)
	}
	switch leaf {
	case "pricing":
		return validateIAPPriceChange(ch)
	case "availability":
		return validateIAPAvailabilityChange(ch)
	}
	return nil
}

func validateIAPPriceChange(ch plan.Change) error {
	price, ok := ch.To.(config.IAPPriceSpec)
	if !ok || price.BaseTerritory == "" || price.PricePointID == "" {
		return errors.New("IAP pricing requires a complete baseTerritory and pricePointId pair")
	}
	if ch.From != nil {
		if _, ok := ch.From.(*config.IAPPriceSpec); !ok {
			return errors.New("IAP pricing expected prior value has wrong type")
		}
	}
	return nil
}

func validateIAPAvailabilityChange(ch plan.Change) error {
	availability, ok := ch.To.(config.IAPAvailabilitySpec)
	if !ok || availability.AvailableInNewTerritories == nil || availability.AvailableTerritories == nil {
		return errors.New("IAP availability requires complete availableInNewTerritories and availableTerritories")
	}
	seen := make(map[string]bool)
	for _, territory := range *availability.AvailableTerritories {
		if territory == "" || seen[territory] {
			return fmt.Errorf("IAP availability invalid or duplicate territory %q", territory)
		}
		seen[territory] = true
	}
	if ch.From != nil {
		if _, ok := ch.From.(*config.IAPAvailabilitySpec); !ok {
			return errors.New("IAP availability expected prior value has wrong type")
		}
	}
	return nil
}

func applyIAPCommerceChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidateIAPCommerceChange(ch); err != nil {
		return err
	}
	productID, leaf, _ := iapCommercePath(ch.Path)
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	iapID, err := resolveIAPByProductID(ctx, c, appID, productID)
	if err != nil {
		return err
	}
	if leaf == "pricing" {
		price, ok := ch.To.(config.IAPPriceSpec)
		if !ok {
			return errors.New("IAP pricing change has invalid value type")
		}
		var expected *asc.IAPBasePrice
		if before, ok := ch.From.(*config.IAPPriceSpec); ok && before != nil {
			expected = &asc.IAPBasePrice{BaseTerritoryID: before.BaseTerritory, PricePointID: before.PricePointID}
		}
		_, err = asc.CreatePreservingIAPPriceSchedule(ctx, c, iapID, price.BaseTerritory, price.PricePointID, expected, time.Now().UTC())
		return err
	}
	availability, ok := ch.To.(config.IAPAvailabilitySpec)
	if !ok {
		return errors.New("IAP availability change has invalid value type")
	}
	var expected *asc.IAPAvailability
	if before, ok := ch.From.(*config.IAPAvailabilitySpec); ok && before != nil {
		expected = &asc.IAPAvailability{ID: "observed", AvailableInNewTerritories: before.AvailableInNewTerritories}
		if before.AvailableTerritories != nil {
			expected.TerritoryIDs = *before.AvailableTerritories
		}
	}
	_, err = asc.CreateIAPAvailability(ctx, c, iapID, *availability.AvailableInNewTerritories, *availability.AvailableTerritories, expected)
	return err
}

func iapCommercePath(path string) (productID, leaf string, err error) {
	const prefix = "/spec/iap/products/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", errors.New("not an IAP product path")
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "commerce" || (parts[2] != "pricing" && parts[2] != "availability") {
		return "", "", errors.New("unsupported IAP commerce path")
	}
	return parts[0], parts[2], nil
}
