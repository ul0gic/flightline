package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// ValidateRightsChange is pure so plan validation can reject unsupported writes
// before any earlier change in the plan is applied.
func ValidateRightsChange(ch plan.Change) error {
	switch ch.Path {
	case "/spec/contentRights":
		return validateContentRightsChange(ch)
	case "/spec/appEula":
		return validateAppEULAChange(ch)
	default:
		return fmt.Errorf("unsupported rights change path %q", ch.Path)
	}
}

func validateContentRightsChange(ch plan.Change) error {
	if ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate {
		return errors.New("content rights requires create or update")
	}
	value, ok := ch.To.(string)
	if !ok || !validRightsValue(value) {
		return errors.New("content rights requires an explicit supported declaration")
	}
	if ch.From != nil {
		if _, ok := ch.From.(string); !ok {
			return errors.New("content rights prior value must be a string or null")
		}
	}
	return nil
}

func validateAppEULAChange(ch plan.Change) error {
	if ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate {
		return errors.New("custom EULA requires create or update")
	}
	want, err := decodeRightsEULA(ch.To)
	if err != nil {
		return err
	}
	if want.AgreementText == nil || want.Territories == nil {
		return errors.New("custom EULA change requires complete agreementText and territories")
	}
	if ch.Op == plan.OpCreate && (*want.AgreementText == "" || len(*want.Territories) == 0) {
		return errors.New("custom EULA create requires nonempty text and territories")
	}
	if err := validateRightsTerritorySet(*want.Territories); err != nil {
		return err
	}
	var prior config.AppEULASpec
	if ch.From != nil {
		prior, err = decodeRightsEULA(ch.From)
		if err != nil {
			return fmt.Errorf("custom EULA prior state: %w", err)
		}
	}
	return validateAppEULANonClearing(want, prior)
}

func validateAppEULANonClearing(want, prior config.AppEULASpec) error {
	if strings.TrimSpace(*want.AgreementText) == "" && !sameRightsText(want.AgreementText, prior.AgreementText) {
		return errors.New("custom EULA cannot clear agreement text")
	}
	if len(*want.Territories) == 0 && !sameRightsTerritoryPointers(want.Territories, prior.Territories) {
		return errors.New("custom EULA cannot clear all territories")
	}
	return nil
}

func applyRightsChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidateRightsChange(ch); err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	if ch.Path == "/spec/contentRights" {
		return applyContentRightsChange(ctx, c, appID, ch)
	}
	return applyAppEULAChange(ctx, c, appID, ch)
}

func applyContentRightsChange(ctx context.Context, c *asc.Client, appID string, ch plan.Change) error {
	want, ok := ch.To.(string)
	if !ok {
		return errors.New("content rights change requires a string")
	}
	current, err := FetchContentRights(ctx, c, appID)
	if err != nil {
		return err
	}
	if current != nil && *current == want {
		return nil
	}
	if !rightsPriorMatches(current, ch.From) {
		return errors.New("content rights changed since planning; refresh the plan")
	}
	writeErr := asc.PatchContentRights(ctx, c, appID, want)
	after, readErr := FetchContentRights(ctx, c, appID)
	if readErr == nil && after != nil && *after == want {
		return nil
	}
	if writeErr != nil {
		return fmt.Errorf("content rights update outcome is unconfirmed; inspect current state before retrying: %w", writeErr)
	}
	if readErr != nil {
		return fmt.Errorf("content rights update outcome is unconfirmed; inspect current state before retrying: %w", readErr)
	}
	return errors.New("content rights update did not converge; refresh the plan")
}

func applyAppEULAChange(ctx context.Context, c *asc.Client, appID string, ch plan.Change) error {
	want, _ := decodeRightsEULA(ch.To)
	observed, err := asc.ReadAppEULA(ctx, c, appID)
	if err != nil {
		return err
	}
	var current *config.AppEULASpec
	if observed != nil {
		current = appEULASpecFromObserved(observed)
	}
	if rightsEULAEqual(current, &want) {
		return nil
	}
	if !rightsEULAPriorMatches(current, ch.From) {
		return errors.New("custom EULA changed since planning; refresh the plan")
	}
	if current == nil {
		_, err = asc.CreateAppEULA(ctx, c, appID, *want.AgreementText, *want.Territories)
	} else {
		var text *string
		var territories *[]string
		if !sameRightsText(current.AgreementText, want.AgreementText) {
			if strings.TrimSpace(*want.AgreementText) == "" {
				return errors.New("custom EULA cannot clear agreement text")
			}
			text = want.AgreementText
		}
		if !sameRightsTerritoryPointers(current.Territories, want.Territories) {
			if len(*want.Territories) == 0 {
				return errors.New("custom EULA cannot clear all territories")
			}
			territories = want.Territories
		}
		err = asc.PatchAppEULA(ctx, c, observed.ID, text, territories)
	}
	after, readErr := FetchAppEULA(ctx, c, appID)
	if readErr == nil && rightsEULAEqual(after, &want) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("custom EULA write outcome is unconfirmed; inspect current state before retrying: %w", err)
	}
	if readErr != nil {
		return fmt.Errorf("custom EULA write outcome is unconfirmed; inspect current state before retrying: %w", readErr)
	}
	return errors.New("custom EULA write did not converge; refresh the plan")
}

func rightsPriorMatches(current *string, prior any) bool {
	if prior == nil {
		return current == nil
	}
	value, ok := prior.(string)
	return ok && current != nil && *current == value
}

func rightsEULAPriorMatches(current *config.AppEULASpec, prior any) bool {
	if prior == nil {
		return current == nil
	}
	value, err := decodeRightsEULA(prior)
	return err == nil && rightsEULAEqual(current, &value)
}

func rightsEULAEqual(a, b *config.AppEULASpec) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameRightsText(a.AgreementText, b.AgreementText) && sameRightsTerritoryPointers(a.Territories, b.Territories)
}

func sameRightsText(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func sameRightsTerritoryPointers(a, b *[]string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aa, bb := slices.Clone(*a), slices.Clone(*b)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}

func decodeRightsEULA(value any) (config.AppEULASpec, error) {
	if value == nil {
		return config.AppEULASpec{}, errors.New("custom EULA change cannot be null")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return config.AppEULASpec{}, fmt.Errorf("encode custom EULA change: %w", err)
	}
	var out config.AppEULASpec
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return config.AppEULASpec{}, fmt.Errorf("decode custom EULA change: %w", err)
	}
	return out, nil
}

func validRightsValue(value string) bool {
	return value == asc.ContentRightsDoesNotUseThirdParty || value == asc.ContentRightsUsesThirdParty
}

func validateRightsTerritorySet(territories []string) error {
	seen := map[string]bool{}
	for _, territory := range territories {
		if strings.TrimSpace(territory) == "" || seen[territory] {
			return errors.New("custom EULA territory IDs must be nonempty and unique")
		}
		seen[territory] = true
	}
	return nil
}
