package state

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// ValidatePhasedReleaseChange validates the atomic rollout change emitted by the planner.
func ValidatePhasedReleaseChange(ch plan.Change) error {
	if ch.Path != "/spec/version/phasedRelease" || (ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate) {
		return errors.New("phased release requires an atomic create or update")
	}
	to, ok := phasedReleaseChangeSpec(ch.To)
	if !ok {
		return fmt.Errorf("phased release expects config.PhasedReleaseSpec, got %T", ch.To)
	}
	if ch.Op == plan.OpCreate {
		return validatePhasedReleaseCreate(ch, to)
	}
	return validatePhasedReleaseUpdate(ch, to)
}

func validatePhasedReleaseCreate(ch plan.Change, to config.PhasedReleaseSpec) error {
	if ch.From != nil || to.Enabled == nil || !*to.Enabled || to.State != nil || to.StartDate != nil || to.TotalPauseDuration != nil || to.CurrentDayNumber != nil {
		return errors.New("phased release creation requires enabled true and omitted state")
	}
	return nil
}

func validatePhasedReleaseUpdate(ch plan.Change, to config.PhasedReleaseSpec) error {
	from, ok := phasedReleaseChangeSpec(ch.From)
	if !ok || from.Enabled == nil || !*from.Enabled || to.Enabled == nil || !*to.Enabled || from.State == nil || to.State == nil || !phasedTransitionAllowed(*from.State, *to.State) {
		return errors.New("phased release update requires ACTIVE to PAUSED or PAUSED to ACTIVE")
	}
	if !phasedReleaseReadOnlyMatches(from, to) {
		return errors.New("phased release startDate, totalPauseDuration, and currentDayNumber are observed and cannot be changed")
	}
	return nil
}

func applyPhasedReleaseChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidatePhasedReleaseChange(ch); err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	version, err := resolveExactPhasedReleaseVersion(ctx, c, appID, actx.Version, actx.Platform)
	if err != nil {
		return err
	}
	current, err := readCurrentPhasedRelease(ctx, c, version.ID)
	if err != nil {
		return err
	}
	to, _ := phasedReleaseChangeSpec(ch.To)
	if ch.Op == plan.OpCreate {
		return applyPhasedReleaseCreate(ctx, c, version, current)
	}
	return applyPhasedReleaseUpdate(ctx, c, version, current, ch, to)
}

func applyPhasedReleaseCreate(ctx context.Context, c *asc.Client, version phasedReleaseVersion, current *asc.AppStoreVersionPhasedRelease) error {
	if current != nil {
		if current.PhasedReleaseState == "INACTIVE" {
			return nil
		}
		return errors.New("apply phased release: rollout already exists; replan")
	}
	if !phasedReleaseEnableVersionState(versionState(version.Attributes)) {
		return fmt.Errorf("apply phased release: version state %q is not eligible to enable a rollout", versionState(version.Attributes))
	}
	created, err := asc.EnableAppStoreVersionPhasedRelease(ctx, c, version.ID)
	if err != nil {
		return err
	}
	if created.PhasedReleaseState != "INACTIVE" {
		return errors.New("apply phased release: creation did not confirm INACTIVE state; inspect current rollout")
	}
	return nil
}

func applyPhasedReleaseUpdate(ctx context.Context, c *asc.Client, version phasedReleaseVersion, current *asc.AppStoreVersionPhasedRelease, ch plan.Change, to config.PhasedReleaseSpec) error {
	if current == nil {
		return errors.New("apply phased release: rollout is absent; replan")
	}
	from, _ := phasedReleaseChangeSpec(ch.From)
	if current.PhasedReleaseState == *to.State {
		return nil
	}
	if current.PhasedReleaseState != *from.State {
		return errors.New("apply phased release: rollout state changed since planning; replan")
	}
	if versionState(version.Attributes) != "READY_FOR_DISTRIBUTION" {
		return fmt.Errorf("apply phased release: version state %q cannot pause or resume a rollout", versionState(version.Attributes))
	}
	var updated *asc.AppStoreVersionPhasedRelease
	var err error
	if *to.State == "PAUSED" {
		updated, err = asc.PauseAppStoreVersionPhasedRelease(ctx, c, current.ID)
	} else {
		updated, err = asc.ResumeAppStoreVersionPhasedRelease(ctx, c, current.ID)
	}
	if err != nil {
		return err
	}
	if updated.PhasedReleaseState != *to.State {
		return errors.New("apply phased release: update did not confirm requested state; inspect current rollout")
	}
	return nil
}

type phasedReleaseVersion struct {
	ID         string
	Attributes asc.VersionAttributes
}

func resolveExactPhasedReleaseVersion(ctx context.Context, c *asc.Client, appID, versionString, platform string) (phasedReleaseVersion, error) {
	if strings.TrimSpace(versionString) == "" {
		return phasedReleaseVersion{}, errors.New("apply phased release: version is required")
	}
	if platform == "" {
		platform = "IOS"
	}
	query := url.Values{"filter[versionString]": {versionString}, "filter[platform]": {platform}, "limit": {"200"}}
	var found *phasedReleaseVersion
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appStoreVersions", query) {
		if err != nil {
			return phasedReleaseVersion{}, fmt.Errorf("apply phased release: list versions: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "appStoreVersions" || row.Attributes.VersionString != versionString || row.Attributes.Platform != platform {
				continue
			}
			if found != nil {
				return phasedReleaseVersion{}, fmt.Errorf("apply phased release: version %q on %s is ambiguous", versionString, platform)
			}
			candidate := phasedReleaseVersion{ID: row.ID, Attributes: row.Attributes}
			found = &candidate
		}
	}
	if found == nil {
		return phasedReleaseVersion{}, fmt.Errorf("apply phased release: version %q on %s was not found", versionString, platform)
	}
	return *found, nil
}

func readCurrentPhasedRelease(ctx context.Context, c *asc.Client, versionID string) (*asc.AppStoreVersionPhasedRelease, error) {
	release, err := asc.ReadAppStoreVersionPhasedRelease(ctx, c, versionID)
	if err == nil {
		return release, nil
	}
	var apiErr *asc.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound {
		return nil, nil
	}
	return nil, fmt.Errorf("apply phased release: read current rollout: %w", err)
}

func phasedReleaseChangeSpec(value any) (config.PhasedReleaseSpec, bool) {
	switch typed := value.(type) {
	case config.PhasedReleaseSpec:
		return typed, true
	case *config.PhasedReleaseSpec:
		if typed != nil {
			return *typed, true
		}
	}
	return config.PhasedReleaseSpec{}, false
}

func phasedTransitionAllowed(from, to string) bool {
	return from == "ACTIVE" && to == "PAUSED" || from == "PAUSED" && to == "ACTIVE"
}

func phasedReleaseReadOnlyMatches(from, to config.PhasedReleaseSpec) bool {
	return equalPhasedReleaseString(from.StartDate, to.StartDate) && equalPhasedReleaseInt(from.TotalPauseDuration, to.TotalPauseDuration) && equalPhasedReleaseInt(from.CurrentDayNumber, to.CurrentDayNumber)
}

func equalPhasedReleaseString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalPhasedReleaseInt(left, right *int) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func phasedReleaseEnableVersionState(state string) bool {
	switch state {
	case "PREPARE_FOR_SUBMISSION", "WAITING_FOR_REVIEW", "IN_REVIEW", "WAITING_FOR_EXPORT_COMPLIANCE", "PENDING_DEVELOPER_RELEASE", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED":
		return true
	default:
		return false
	}
}

func clonePhasedInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
