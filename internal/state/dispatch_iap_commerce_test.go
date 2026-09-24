package state

import (
	"context"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestF5A_ValidateIAPCommerceChangeRejectsIncompleteOrUnsupported(t *testing.T) {
	cases := []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/iap/products/p1/commerce/pricing", To: config.IAPPriceSpec{BaseTerritory: "USA"}},
		{Op: plan.OpUpdate, Path: "/spec/iap/products/p1/commerce/availability", To: config.IAPAvailabilitySpec{AvailableInNewTerritories: new(true)}},
		{Op: plan.OpDelete, Path: "/spec/iap/products/p1/commerce/pricing", To: config.IAPPriceSpec{BaseTerritory: "USA", PricePointID: "P1"}},
		{Op: plan.OpUpdate, Path: "/spec/iap/products/p1/commerce/offerCodes", To: "unexpected"},
	}
	for _, change := range cases {
		if err := ValidateIAPCommerceChange(change); err == nil {
			t.Errorf("accepted %+v", change)
		}
	}
	if err := applyIAPCommerceChange(context.Background(), nil, ApplyContext{}, cases[0]); err == nil {
		t.Fatal("apply accepted invalid pricing before client use")
	}
	territories := []string{}
	valid := plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/p1/commerce/availability", To: config.IAPAvailabilitySpec{AvailableInNewTerritories: new(false), AvailableTerritories: &territories}}
	if err := ValidateIAPCommerceChange(valid); err != nil {
		t.Fatalf("explicit false and empty set rejected: %v", err)
	}
	valid.Path = "/spec/iap/products/p1/commerce/pricing"
	valid.To = config.IAPPriceSpec{BaseTerritory: "USA", PricePointID: "P1"}
	if err := ValidateIAPCommerceChange(valid); err != nil {
		t.Fatalf("paired pricing rejected: %v", err)
	}
}
