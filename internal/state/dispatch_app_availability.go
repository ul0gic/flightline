package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/plan"
)

// ValidateAppAvailabilityChange validates a single pre-order territory availability update.
func ValidateAppAvailabilityChange(ch plan.Change) error {
	territoryID, field, err := appAvailabilityChangePath(ch.Path)
	if err != nil {
		return err
	}
	if territoryID == "" || ch.Op != plan.OpUpdate {
		return errors.New("app availability requires an update to an observed territory")
	}
	switch field {
	case "available":
		if _, ok := ch.To.(bool); !ok {
			return fmt.Errorf("app availability available expects bool, got %T", ch.To)
		}
	case "releaseDate":
		value, ok := ch.To.(string)
		if !ok {
			return fmt.Errorf("app availability releaseDate expects string, got %T", ch.To)
		}
		if !validAvailabilityReleaseDate(value) {
			return errors.New("app availability releaseDate must be a nonempty YYYY-MM-DD date")
		}
	default:
		return fmt.Errorf("app availability field %q is observed and cannot be written", field)
	}
	return nil
}

func applyAppAvailabilityChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidateAppAvailabilityChange(ch); err != nil {
		return err
	}
	territoryID, field, _ := appAvailabilityChangePath(ch.Path)
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	availability, err := asc.ReadAppAvailability(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("apply app availability: read current state: %w", err)
	}
	current, found := findAvailabilityTerritory(availability.Territories, territoryID)
	if !found {
		return fmt.Errorf("apply app availability: territory %q is no longer observed; replan", territoryID)
	}
	if current.PreOrderEnabled == nil || !*current.PreOrderEnabled {
		return fmt.Errorf("apply app availability: territory %q is not an observed active pre-order; ordinary availability writes are unsupported", territoryID)
	}
	if !appAvailabilityCurrentMatches(field, current, ch.From) {
		return fmt.Errorf("apply app availability: territory %q changed since planning; replan", territoryID)
	}
	updated, err := asc.PatchTerritoryAvailability(ctx, c, current.ID, field, ch.To)
	if err != nil {
		return err
	}
	if !appAvailabilityResponseMatches(field, updated, ch.To) {
		return fmt.Errorf("apply app availability: update response for territory %q did not confirm %s; inspect current state", territoryID, field)
	}
	return nil
}

func appAvailabilityChangePath(path string) (territoryID, field string, err error) {
	const prefix = "/spec/appAvailability/territories/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", fmt.Errorf("app availability: unsupported path %q", path)
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("app availability: malformed path %q", path)
	}
	return parts[0], parts[1], nil
}

func findAvailabilityTerritory(territories []asc.TerritoryAvailability, territoryID string) (asc.TerritoryAvailability, bool) {
	for _, territory := range territories {
		if territory.TerritoryID == territoryID {
			return territory, true
		}
	}
	return asc.TerritoryAvailability{}, false
}

func appAvailabilityCurrentMatches(field string, current asc.TerritoryAvailability, expected any) bool {
	switch field {
	case "available":
		value, ok := expected.(bool)
		return ok && current.Available != nil && *current.Available == value
	case "releaseDate":
		if expected == nil {
			return current.ReleaseDate == ""
		}
		value, ok := expected.(string)
		return ok && current.ReleaseDate == value
	default:
		return false
	}
}

func validAvailabilityReleaseDate(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func appAvailabilityResponseMatches(field string, updated asc.Resource[asc.TerritoryAvailabilityAttributes], want any) bool {
	switch field {
	case "available":
		value, ok := want.(bool)
		return ok && updated.Attributes.Available != nil && *updated.Attributes.Available == value
	case "releaseDate":
		value, ok := want.(string)
		return ok && updated.Attributes.ReleaseDate == value
	default:
		return false
	}
}
