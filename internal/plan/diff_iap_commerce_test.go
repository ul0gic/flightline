package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF5A_IAPCommerceDiffAtomicPriceAndPartialAvailability(t *testing.T) {
	territories := []string{"CAN", "USA"}
	live := &config.IAPCommerceSpec{
		Pricing:      &config.IAPPriceSpec{BaseTerritory: "USA", PricePointID: "OLD"},
		Availability: &config.IAPAvailabilitySpec{AvailableInNewTerritories: new(true), AvailableTerritories: &territories},
	}
	desired := &config.IAPCommerceSpec{
		Pricing:      &config.IAPPriceSpec{BaseTerritory: "USA", PricePointID: "NEW"},
		Availability: &config.IAPAvailabilitySpec{AvailableInNewTerritories: new(false)},
	}
	var changes []Change
	diffIAPCommerce("com.example.unlock", desired, live, &changes)
	if len(changes) != 2 || changes[0].Path != "/spec/iap/products/com.example.unlock/commerce/pricing" || changes[1].Path != "/spec/iap/products/com.example.unlock/commerce/availability" {
		t.Fatalf("changes=%+v", changes)
	}
	got, ok := changes[1].To.(config.IAPAvailabilitySpec)
	if !ok {
		t.Fatalf("availability target type %T", changes[1].To)
	}
	if got.AvailableTerritories == nil || len(*got.AvailableTerritories) != 2 || *got.AvailableInNewTerritories {
		t.Fatalf("merged availability=%+v", got)
	}
	changes = nil
	diffIAPCommerce("com.example.unlock", &config.IAPCommerceSpec{Availability: &config.IAPAvailabilitySpec{AvailableTerritories: &territories}}, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("same territory set should be no-op: %+v", changes)
	}
}

func TestF5A_IAPCommerceDiffExplicitEmptyTerritories(t *testing.T) {
	empty := []string{}
	desired := &config.IAPCommerceSpec{Availability: &config.IAPAvailabilitySpec{AvailableInNewTerritories: new(false), AvailableTerritories: &empty}}
	var changes []Change
	diffIAPCommerce("com.example.unlock", desired, nil, &changes)
	if len(changes) != 1 || changes[0].Op != OpCreate {
		t.Fatalf("changes=%+v", changes)
	}
	to, ok := changes[0].To.(config.IAPAvailabilitySpec)
	if !ok {
		t.Fatalf("availability target type %T", changes[0].To)
	}
	if to.AvailableTerritories == nil || len(*to.AvailableTerritories) != 0 {
		t.Fatalf("explicit empty lost: %+v", to)
	}
}
