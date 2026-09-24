package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF5B_DiffAppAvailabilityEmitsOnlyManagedWritableFields(t *testing.T) {
	trueValue, falseValue := true, false
	oldDate, newDate := "2026-12-01", "2027-02-03"
	live := &config.AppAvailabilitySpec{Territories: map[string]config.TerritoryAvailabilitySpec{
		"GBR": {Available: &falseValue, ReleaseDate: &oldDate, PreOrderEnabled: &trueValue, ContentStatuses: []string{"AVAILABLE_FOR_PREORDER"}},
	}}
	desired := &config.AppAvailabilitySpec{Territories: map[string]config.TerritoryAvailabilitySpec{
		"GBR": {Available: &trueValue, ReleaseDate: &newDate, PreOrderEnabled: &falseValue, ContentStatuses: []string{"MISSING_RATING"}},
	}}
	var changes []Change
	diffAppAvailability(desired, live, &changes)
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	if changes[0].Op != OpUpdate || changes[0].Path != "/spec/appAvailability/territories/GBR/available" || changes[1].Path != "/spec/appAvailability/territories/GBR/releaseDate" {
		t.Fatalf("changes = %#v", changes)
	}
	var omitted []Change
	diffAppAvailability(&config.AppAvailabilitySpec{Territories: map[string]config.TerritoryAvailabilitySpec{"GBR": {}}}, live, &omitted)
	if len(omitted) != 0 {
		t.Fatalf("omitted fields changed live state: %#v", omitted)
	}
}
