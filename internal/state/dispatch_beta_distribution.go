package state

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func ValidateBetaDistributionChange(ch plan.Change) error {
	_, _, err := betaDistributionIntent(ch)
	return err
}

func betaDistributionIntent(ch plan.Change) (string, []config.BetaBuildSelector, error) {
	if ch.Op != plan.OpUpdate || !strings.HasPrefix(ch.Path, "/spec/testflight/groups/") || !strings.HasSuffix(ch.Path, "/builds") {
		return "", nil, fmt.Errorf("unsupported beta distribution change %s %s", ch.Op, ch.Path)
	}
	rest := strings.TrimPrefix(ch.Path, "/spec/testflight/groups/")
	groupPath := strings.TrimSuffix(rest, "/builds")
	if groupPath == "" || groupPath == rest || strings.Contains(groupPath, "/") {
		return "", nil, fmt.Errorf("invalid beta group build path %q", ch.Path)
	}
	groupName, err := unescapeTestFlightPathSegment(groupPath)
	if err != nil || groupName == "" {
		return "", nil, fmt.Errorf("invalid beta group build path %q", ch.Path)
	}
	var selectors []config.BetaBuildSelector
	if err := decodeBetaChange(ch.To, &selectors); err != nil {
		return "", nil, err
	}
	if selectors == nil {
		return "", nil, fmt.Errorf("beta group %s builds requires explicit array intent", groupName)
	}
	if err := validateBetaDistributionSelectors(groupName, selectors); err != nil {
		return "", nil, err
	}
	return groupName, selectors, nil
}

func validateBetaDistributionSelectors(groupName string, selectors []config.BetaBuildSelector) error {
	seen := make(map[config.BetaBuildSelector]bool, len(selectors))
	for _, selector := range selectors {
		if selector.Number == "" || selector.Version == "" || selector.Platform == "" {
			return fmt.Errorf("beta group %s build selector requires number, version, platform", groupName)
		}
		if seen[selector] {
			return fmt.Errorf("beta group %s has duplicate build selector", groupName)
		}
		seen[selector] = true
	}
	return nil
}

func applyBetaDistributionChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	groupName, desired, err := betaDistributionIntent(ch)
	if err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	groupID, err := resolveBetaGroupByName(ctx, c, appID, groupName)
	if err != nil {
		return err
	}
	wantIDs, err := resolveDesiredBetaBuildIDs(ctx, c, appID, groupName, desired)
	if err != nil {
		return err
	}
	current, err := asc.ListBetaGroupBuilds(ctx, c, groupID)
	if err != nil {
		return err
	}
	adds, removes := betaMembershipDelta(wantIDs, current)
	for _, id := range adds {
		if err := requireBetaNotificationDisabled(ctx, c, id); err != nil {
			return fmt.Errorf("group %s: %w", groupName, err)
		}
	}
	for _, id := range adds {
		if err := asc.AddBetaGroupBuild(ctx, c, groupID, id); err != nil {
			return err
		}
	}
	for _, id := range removes {
		if err := asc.RemoveBetaGroupBuild(ctx, c, groupID, id); err != nil {
			return err
		}
	}
	return nil
}

func resolveDesiredBetaBuildIDs(ctx context.Context, c *asc.Client, appID, groupName string, desired []config.BetaBuildSelector) (map[string]bool, error) {
	wantIDs := make(map[string]bool, len(desired))
	for _, selector := range desired {
		id, err := resolveBetaBuildSelector(ctx, c, appID, selector)
		if err != nil {
			return nil, err
		}
		if wantIDs[id] {
			return nil, fmt.Errorf("duplicate build %s (%s/%s) in group %s", selector.Number, selector.Version, selector.Platform, groupName)
		}
		wantIDs[id] = true
	}
	return wantIDs, nil
}

func betaMembershipDelta(wantIDs map[string]bool, current []asc.Resource[asc.BuildAttributes]) (adds, removes []string) {
	haveIDs := make(map[string]bool, len(current))
	for _, row := range current {
		haveIDs[row.ID] = true
	}
	for id := range wantIDs {
		if !haveIDs[id] {
			adds = append(adds, id)
		}
	}
	for id := range haveIDs {
		if !wantIDs[id] {
			removes = append(removes, id)
		}
	}
	sort.Strings(adds)
	sort.Strings(removes)
	return adds, removes
}

func requireBetaNotificationDisabled(ctx context.Context, c *asc.Client, buildID string) error {
	detail, err := asc.GetBuildBetaDetail(ctx, c, buildID)
	if err != nil {
		return fmt.Errorf("cannot verify auto-notify is disabled for build %s: %w", buildID, err)
	}
	if detail.Attributes.AutoNotifyEnabled == nil || *detail.Attributes.AutoNotifyEnabled {
		return fmt.Errorf("build %s auto-notify is enabled or unknown; disable it explicitly in App Store Connect before applying group membership", buildID)
	}
	return nil
}
