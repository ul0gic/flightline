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
	Version  string // e.g. "1.0.1"; empty = latest editable
	Platform string // e.g. "IOS"; empty = IOS
	// RequireEditable fails Fetch on non-writable versions, so a stale
	// metadata.version errors loudly instead of diffing against a released one.
	RequireEditable bool
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
			if _, ok := editableVersionStates[st]; !ok {
				return nil, fmt.Errorf(
					"state: version %s is %s and cannot be edited; update metadata.version to an editable version or run `flightline versions create`",
					versionAttrs.VersionString, st,
				)
			}
		}
	}

	out := &config.State{
		APIVersion: "flightline.dev/v1alpha1",
		Kind:       "AppState",
		Metadata: config.StateMetadata{
			BundleID: bundleID,
			Version:  versionAttrs.VersionString,
			Platform: platform,
		},
		Spec: config.StateSpec{Version: projectVersion(versionAttrs)},
	}
	fetchAppInfoSurfaces(ctx, c, appID, versionID, out)
	fetchVersionScopedSurfaces(ctx, c, versionID, out)
	fetchAppScopedSurfaces(ctx, c, appID, out)
	return out, nil
}

func fetchAppInfoSurfaces(ctx context.Context, c *asc.Client, appID, versionID string, out *State) {
	appInfoID, err := fetchEditableAppInfo(ctx, c, appID)
	if err != nil || appInfoID == "" {
		return
	}
	if ar, ferr := fetchAgeRating(ctx, c, appInfoID); ferr == nil {
		out.Spec.AgeRating = projectAgeRating(ar)
	}
	if cats := fetchCategories(ctx, c, appInfoID); cats != nil {
		out.Spec.Categories = cats
	}
	if md, ferr := fetchMetadataLocales(ctx, c, versionID, appInfoID); ferr == nil {
		out.Spec.Metadata = md
	}
}

func fetchVersionScopedSurfaces(ctx context.Context, c *asc.Client, versionID string, out *State) {
	if buildID, encryption, ferr := fetchVersionBuildEncryption(ctx, c, versionID); ferr == nil && buildID != "" {
		out.Spec.ExportCompliance = &config.ExportComplianceSpec{UsesNonExemptEncryption: encryption}
		if num, nerr := fetchBuildNumber(ctx, c, buildID); nerr == nil {
			out.Spec.Build = &config.BuildSpec{Number: num}
		}
	}
	if rd := fetchReviewerDemo(ctx, c, versionID); rd != nil {
		out.Spec.ReviewerDemo = rd
	}
	if ss, ferr := fetchScreenshots(ctx, c, versionID); ferr == nil && ss != nil {
		out.Spec.Screenshots = ss
	}
}

func fetchAppScopedSurfaces(ctx context.Context, c *asc.Client, appID string, out *State) {
	if pr := fetchPricing(ctx, c, appID); pr != nil {
		out.Spec.Pricing = pr
	}
	if iaps, ferr := fetchIAPs(ctx, c, appID); ferr == nil && iaps != nil && len(iaps.Products) > 0 {
		out.Spec.IAP = iaps
	}
	if tf, ferr := fetchTestFlightGroups(ctx, c, appID); ferr == nil && tf != nil && len(tf.Groups) > 0 {
		out.Spec.TestFlight = tf
	}
	if cpp, ferr := fetchCustomProductPages(ctx, c, appID); ferr == nil && len(cpp) > 0 {
		out.Spec.CustomProductPages = &cpp
	}
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
	if a.ViolenceRealisticProlongedGraphicOrSadistic != "" {
		// schema is *bool, Apple is enum string. Treat any non-NONE as true.
		v := a.ViolenceRealisticProlongedGraphicOrSadistic != "NONE"
		out.ProlongedGraphicSadisticRealisticViolence = &v
	}
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
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return asc.VersionAttributes{}, "", fmt.Errorf("state: list versions: %w", err)
	}
	if len(page.Data) == 0 {
		return asc.VersionAttributes{}, "", fmt.Errorf("state: no version %q on platform %s", versionStr, platform)
	}
	return page.Data[0].Attributes, page.Data[0].ID, nil
}

// fetchEditableAppInfo returns the appInfo ID in an editable state, falling back to the first.
func fetchEditableAppInfo(ctx context.Context, c *asc.Client, appID string) (string, error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[asc.AppInfoAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appInfos", q,
	)
	if err != nil {
		return "", fmt.Errorf("state: list appInfos: %w", err)
	}
	for _, r := range page.Data {
		switch r.Attributes.State {
		case "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED",
			"METADATA_REJECTED", "WAITING_FOR_REVIEW", "IN_REVIEW":
			return r.ID, nil
		}
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	return "", nil
}

func fetchAgeRating(ctx context.Context, c *asc.Client, appInfoID string) (asc.AgeRatingDeclarationAttributes, error) {
	resp, err := asc.Get[asc.Single[asc.AgeRatingDeclarationAttributes]](
		ctx, c, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil,
	)
	if err != nil {
		return asc.AgeRatingDeclarationAttributes{}, err
	}
	return resp.Data.Attributes, nil
}

func fetchVersionBuildEncryption(ctx context.Context, c *asc.Client, versionID string) (buildID string, usesNonExempt *bool, err error) {
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/build", nil,
	)
	if err != nil {
		return "", nil, err
	}
	return resp.Data.ID, resp.Data.Attributes.UsesNonExemptEncryption, nil
}
