package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestC2B_PricingDiffAtomic(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name                     string
		desired, live            *config.PricingSpec
		wantOp                   Op
		wantTerritory, wantPoint string
	}{
		{"initial", &config.PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("P1")}, nil, OpCreate, "USA", "P1"},
		{"simultaneous", &config.PricingSpec{BaseTerritory: str("GBR"), AppPricePointID: str("P2")}, &config.PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("P1")}, OpUpdate, "GBR", "P2"},
		{"partial", &config.PricingSpec{AppPricePointID: str("P2")}, &config.PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("P1")}, OpUpdate, "USA", "P2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff(&config.State{Spec: config.StateSpec{Pricing: tc.desired}}, &config.State{Spec: config.StateSpec{Pricing: tc.live}})
			if len(got) != 1 || got[0].Op != tc.wantOp || got[0].Path != "/spec/pricing" {
				t.Fatalf("changes=%+v", got)
			}
			pair, ok := got[0].To.(config.PricingSpec)
			if !ok || pair.BaseTerritory == nil || pair.AppPricePointID == nil || *pair.BaseTerritory != tc.wantTerritory || *pair.AppPricePointID != tc.wantPoint {
				t.Fatalf("incomplete To=%+v", got[0].To)
			}
		})
	}
}
