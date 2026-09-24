package config

import "testing"

func TestC2B_PricingIntent(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name                     string
		desired, live            *PricingSpec
		wantTerritory, wantPoint string
		wantComplete             bool
		wantDiagnostic           bool
	}{
		{"initial complete", &PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("P1")}, nil, "USA", "P1", true, false},
		{"territory change requires explicit point", &PricingSpec{BaseTerritory: str("GBR")}, &PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("P1")}, "GBR", "P1", true, true},
		{"initial incomplete", &PricingSpec{BaseTerritory: str("USA")}, nil, "", "", false, true},
		{"point change preserves territory", &PricingSpec{AppPricePointID: str("P1")}, &PricingSpec{BaseTerritory: str("USA")}, "USA", "P1", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair, complete := CompletePricingPair(tc.desired, tc.live)
			if complete != tc.wantComplete {
				t.Fatalf("complete=%v", complete)
			}
			desired := &State{Spec: StateSpec{Pricing: tc.desired}}
			live := &State{Spec: StateSpec{Pricing: tc.live}}
			if got := ValidatePricingIntent("state.yaml", desired, live); (len(got) > 0) != tc.wantDiagnostic {
				t.Fatalf("diagnostics=%v", got)
			}
			if complete && (*pair.BaseTerritory != tc.wantTerritory || *pair.AppPricePointID != tc.wantPoint) {
				t.Fatalf("pair=%+v", pair)
			}
		})
	}
}
