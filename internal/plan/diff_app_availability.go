package plan

import (
	"sort"

	"github.com/ul0gic/flightline/internal/config"
)

func diffAppAvailability(desired, live *config.AppAvailabilitySpec, out *[]Change) {
	if desired == nil {
		return
	}
	var observed map[string]config.TerritoryAvailabilitySpec
	if live != nil {
		observed = live.Territories
	}
	territories := make([]string, 0, len(desired.Territories))
	for territoryID := range desired.Territories {
		territories = append(territories, territoryID)
	}
	sort.Strings(territories)
	for _, territoryID := range territories {
		want := desired.Territories[territoryID]
		have := observed[territoryID]
		base := "/spec/appAvailability/territories/" + territoryID
		emitAppAvailabilityUpdate(out, territoryID, base+"/available", want.Available, have.Available)
		emitAppAvailabilityUpdate(out, territoryID, base+"/releaseDate", want.ReleaseDate, have.ReleaseDate)
	}
}

func emitAppAvailabilityUpdate(out *[]Change, territoryID, path string, desired, live any) {
	if desired == nil || derefAny(desired) == nil || derefAny(desired) == derefAny(live) {
		return
	}
	to := derefAny(desired)
	from := derefAny(live)
	*out = append(*out, Change{
		Op:       OpUpdate,
		Resource: "appAvailability." + territoryID,
		Path:     path,
		From:     from,
		To:       to,
		Hint:     "update pre-order availability for " + territoryID,
	})
}
