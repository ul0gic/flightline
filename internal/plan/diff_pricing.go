package plan

import (
	"fmt"
	"github.com/ul0gic/flightline/internal/config"
	"reflect"
)

func diffPricing(d, l *config.PricingSpec, out *[]Change) {
	if d == nil {
		return
	}
	pair, complete := config.CompletePricingPair(d, l)
	if !complete {
		return
	} // ValidatePricingIntent reports the missing field.
	if l != nil && reflect.DeepEqual(pair, *l) {
		return
	}
	op := OpUpdate
	if l == nil {
		op = OpCreate
	}
	var from any
	if l != nil {
		from = *l
	}
	*out = append(*out, Change{
		Op: op, Resource: "pricing", Path: "/spec/pricing", From: from, To: pair,
		Hint: fmt.Sprintf("base price: %s / %s", *pair.BaseTerritory, *pair.AppPricePointID),
	})
}
