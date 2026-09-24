// Package state implements Fetch (live ASC → typed *State) and Apply (change set → ASC writes).
// Privacy labels are absent: Apple's API doesn't expose them.
package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// FetchOpts narrows what Fetch pulls; an empty value resolves to the latest
// non-archived state on the app.
type FetchOpts struct {
	// BetaDesired selects beta build-localization and group-membership observations needed by a plan.
	BetaDesired *config.TestFlightSpec
	// IncludeBetaBuilds includes all current group memberships in a fetched state file.
	IncludeBetaBuilds bool
	Version           string // e.g. "1.0.1"; empty = latest editable
	Platform          string // e.g. "IOS"; empty = IOS
	// RequireEditable fails Fetch on non-writable versions, so a stale
	// metadata.version errors loudly instead of diffing against a released one.
	RequireEditable bool
	// AllowPhasedRelease permits observation only; callers must validate all resulting changes with ValidateVersionChanges.
	AllowPhasedRelease bool
}

// editableVersionStates are the appStoreVersion states Apple accepts writes in.
var editableVersionStates = map[string]struct{}{
	"PREPARE_FOR_SUBMISSION": {},
	"METADATA_REJECTED":      {},
	"DEVELOPER_REJECTED":     {},
	"REJECTED":               {},
	"INVALID_BINARY":         {},
}

func versionState(a asc.VersionAttributes) string {
	if a.AppVersionState != "" {
		return a.AppVersionState
	}
	return a.AppStoreState
}

// Fetch reads live ASC state for bundleID as a schema-round-trippable *State.
// Unsupported surfaces are left nil so the diff engine treats them as not-managed.
func Fetch(ctx context.Context, c *asc.Client, bundleID string, opts FetchOpts) (*State, error) {
	if c == nil {
		return nil, errors.New("state: Fetch: client is nil")
	}
	if bundleID == "" {
		return nil, errors.New("state: Fetch: bundleID is required")
	}
	platform := opts.Platform
	if platform == "" {
		platform = "IOS"
	}

	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return nil, err
	}
	versionAttrs, versionID, err := fetchVersion(ctx, c, appID, opts.Version, platform)
	if err != nil {
		return nil, err
	}
	if opts.RequireEditable {
		if st := versionState(versionAttrs); st != "" {
			if _, ok := editableVersionStates[st]; !ok && !opts.AllowPhasedRelease {
				return nil, fmt.Errorf(
					"state: version %s is %s and cannot be edited; update metadata.version to an editable version or run `flightline versions create`",
					versionAttrs.VersionString, st,
				)
			}
		}
	}

	out := &config.State{
		ObservedVersionState: versionState(versionAttrs),
		APIVersion:           "flightline.dev/v1alpha1",
		Kind:                 "AppState",
		Metadata: config.StateMetadata{
			BundleID: bundleID,
			Version:  versionAttrs.VersionString,
			Platform: platform,
		},
		Spec: config.StateSpec{Version: projectVersion(versionAttrs)},
	}
	if err := fetchAppInfoSurfaces(ctx, c, appID, versionID, out); err != nil {
		return nil, err
	}
	buildID, err := fetchVersionScopedSurfaces(ctx, c, versionID, out)
	if err != nil {
		return nil, err
	}
	if err := fetchAppScopedSurfaces(ctx, c, appID, out, opts); err != nil {
		return nil, err
	}
	if err := fetchBetaSurfaces(ctx, c, appID, buildID, opts, out); err != nil {
		return nil, err
	}
	return out, nil
}

// State is re-exported so callers need only one import alongside Fetch.
type State = config.State

func projectVersion(a asc.VersionAttributes) *config.VersionSpec {
	out := &config.VersionSpec{}
	if a.ReleaseType != "" {
		s := a.ReleaseType
		out.ReleaseType = &s
	}
	if a.EarliestReleaseDate != "" {
		s := a.EarliestReleaseDate
		out.EarliestReleaseDate = &s
	}
	if a.Copyright != "" {
		s := a.Copyright
		out.Copyright = &s
	}
	if a.Downloadable != nil {
		v := *a.Downloadable
		out.Downloadable = &v
	}
	return out
}

// projectAgeRating maps Apple's wire names to schema names; apply re-translates
// back via ageRatingSchemaToWire.
func projectAgeRating(a asc.AgeRatingDeclarationAttributes) *config.AgeRatingSpec {
	out := &config.AgeRatingSpec{
		AgeRatingOverrideV2:            optStr(a.AgeRatingOverrideV2),
		KoreaAgeRatingOverride:         optStr(a.KoreaAgeRatingOverride),
		GracRatingClassificationNumber: optStr(a.GracRatingClassificationNumber),

		CartoonOrFantasyViolence:            optStr(a.ViolenceCartoonOrFantasy),
		RealisticViolence:                   optStr(a.ViolenceRealistic),
		ProfanityOrCrudeHumor:               optStr(a.ProfanityOrCrudeHumor),
		MatureSuggestiveThemes:              optStr(a.MatureOrSuggestiveThemes),
		HorrorOrFearThemes:                  optStr(a.HorrorOrFearThemes),
		MedicalOrTreatmentInformation:       optStr(a.MedicalOrTreatmentInformation),
		AlcoholTobaccoOrDrugUseOrReferences: optStr(a.AlcoholTobaccoOrDrugUseOrReferences),
		ContestsAndGambling:                 optStr(a.Contests),
		SexualContentOrNudity:               optStr(a.SexualContentOrNudity),
		SexualContentGraphicAndNudity:       optStr(a.SexualContentGraphicAndNudity),
		GamblingSimulated:                   optStr(a.GamblingSimulated),
		GunsOrOtherWeapons:                  optStr(a.GunsOrOtherWeapons),
		Advertising:                         copyBool(a.Advertising),
		AgeAssurance:                        copyBool(a.AgeAssurance),
		HealthOrWellnessTopics:              copyBool(a.HealthOrWellnessTopics),
		LootBox:                             copyBool(a.LootBox),
		MessagingAndChat:                    copyBool(a.MessagingAndChat),
		ParentalControls:                    copyBool(a.ParentalControls),
		UserGeneratedContent:                copyBool(a.UserGeneratedContent),
		Gambling:                            copyBool(a.Gambling),
		SocialMedia:                         copyBool(a.SocialMedia),
		SocialMediaAgeRestricted:            copyBool(a.SocialMediaAgeRestricted),
		UnrestrictedWebAccess:               copyBool(a.UnrestrictedWebAccess),
		KidsAgeBand:                         optStr(a.KidsAgeBand),
	}
	out.ProlongedGraphicSadisticRealisticViolence = optStr(a.ViolenceRealisticProlongedGraphicOrSadistic)

	return out
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func copyBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

type appAttributes struct {
	BundleID string `json:"bundleId,omitempty"`
}

func resolveAppID(ctx context.Context, c *asc.Client, bundleID string) (string, error) {
	if isNumericAppID(bundleID) {
		if _, err := asc.Get[asc.Single[appAttributes]](ctx, c, "/v1/apps/"+bundleID, nil); err != nil {
			return "", fmt.Errorf("state: no app found with id %s: %w", bundleID, err)
		}
		return bundleID, nil
	}
	q := url.Values{
		"filter[bundleId]": {bundleID},
		"limit":            {"1"},
	}
	page, err := asc.Get[asc.Collection[appAttributes]](ctx, c, "/v1/apps", q)
	if err != nil {
		return "", fmt.Errorf("state: resolve appId for %s: %w", bundleID, err)
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("state: no app found with bundleId %q", bundleID)
	}
	return page.Data[0].ID, nil
}

// isNumericAppID reports whether the argument is an ASC app ID (pure digits) rather than a bundleId.
func isNumericAppID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// fetchVersion returns the matching version row, or the newest editable when versionStr is empty.
func fetchVersion(ctx context.Context, c *asc.Client, appID, versionStr, platform string) (asc.VersionAttributes, string, error) {
	q := url.Values{
		"filter[platform]": {platform},
		"limit":            {"50"},
	}
	if versionStr != "" {
		q.Set("filter[versionString]", versionStr)
	}
	var selected *asc.Resource[asc.VersionAttributes]
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, c, "/v1/apps/"+appID+"/appStoreVersions", q) {
		if err != nil {
			return asc.VersionAttributes{}, "", fmt.Errorf("state: list versions: %w", err)
		}
		for i := range page.Data {
			if selected == nil {
				row := page.Data[i]
				selected = &row
			}
		}
	}
	if selected == nil {
		return asc.VersionAttributes{}, "", fmt.Errorf("state: no version %q on platform %s", versionStr, platform)
	}
	return selected.Attributes, selected.ID, nil
}

// fetchEditableAppInfo returns the appInfo ID in an editable state, falling back to the first.
func fetchEditableAppInfo(ctx context.Context, c *asc.Client, appID string) (string, error) {
	q := url.Values{"limit": {"50"}}
	var firstID string
	for page, err := range asc.Pages[asc.AppInfoAttributes](ctx, c, "/v1/apps/"+appID+"/appInfos", q) {
		if err != nil {
			return "", fmt.Errorf("state: list appInfos: %w", err)
		}
		for _, r := range page.Data {
			if firstID == "" {
				firstID = r.ID
			}
			switch r.Attributes.State {
			case "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED",
				"METADATA_REJECTED", "WAITING_FOR_REVIEW", "IN_REVIEW":
				return r.ID, nil
			}
		}
	}
	return firstID, nil
}

func fetchAgeRating(ctx context.Context, c *asc.Client, appInfoID string) (*asc.AgeRatingDeclarationAttributes, error) {
	resp, err := asc.Get[struct {
		Data *asc.Resource[asc.AgeRatingDeclarationAttributes] `json:"data"`
	}](
		ctx, c, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil,
	)
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, nil
	}
	if resp.Data.ID == "" {
		return nil, errors.New("age rating response missing resource id")
	}
	return &resp.Data.Attributes, nil
}

func fetchVersionBuildEncryption(ctx context.Context, c *asc.Client, versionID string) (buildID string, usesNonExempt *bool, err error) {
	resp, err := asc.Get[struct {
		Data *asc.Resource[asc.BuildAttributes] `json:"data"`
	}](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/build", nil,
	)
	if err != nil {
		return "", nil, err
	}
	if resp.Data == nil {
		return "", nil, nil
	}
	if resp.Data.ID == "" {
		return "", nil, errors.New("version build response missing resource id")
	}
	return resp.Data.ID, resp.Data.Attributes.UsesNonExemptEncryption, nil
}
