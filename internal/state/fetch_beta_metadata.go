package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func FetchBetaMetadata(ctx context.Context, c *asc.Client, appID string, selectors []config.BetaBuildSelector) (*config.BetaMetadataSpec, error) {
	result := &config.BetaMetadataSpec{}
	localizations, err := fetchBetaAppLocalizationSpecs(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	result.AppLocalizations = localizations
	review, err := fetchBetaReviewDetailSpec(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	result.ReviewDetails = review
	builds, err := fetchBetaBuildMetadataSpecs(ctx, c, appID, selectors)
	if err != nil {
		return nil, err
	}
	result.Builds = builds
	if len(result.AppLocalizations) == 0 && result.ReviewDetails == nil && len(result.Builds) == 0 {
		return nil, nil
	}
	return result, nil
}

func fetchBetaAppLocalizationSpecs(ctx context.Context, c *asc.Client, appID string) (map[string]config.BetaAppLocalizationSpec, error) {
	appLocalizations, err := asc.ListBetaAppLocalizations(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	var result map[string]config.BetaAppLocalizationSpec
	if len(appLocalizations) != 0 {
		result = make(map[string]config.BetaAppLocalizationSpec, len(appLocalizations))
	}
	for _, row := range appLocalizations {
		locale := row.Attributes.Locale
		if locale == "" {
			return nil, fmt.Errorf("beta app localization %s has no locale", row.ID)
		}
		if _, exists := result[locale]; exists {
			return nil, fmt.Errorf("multiple beta app localizations for %s", locale)
		}
		a := row.Attributes
		result[locale] = config.BetaAppLocalizationSpec{
			Description: &a.Description, FeedbackEmail: &a.FeedbackEmail, MarketingURL: &a.MarketingURL,
			PrivacyPolicyURL: &a.PrivacyPolicyURL, TVOSPrivacyPolicy: &a.TVOSPrivacyPolicy,
		}
	}
	return result, nil
}

func fetchBetaReviewDetailSpec(ctx context.Context, c *asc.Client, appID string) (*config.BetaReviewDetailsSpec, error) {
	detail, err := asc.GetBetaAppReviewDetail(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, nil
	}
	a := detail.Attributes
	return &config.BetaReviewDetailsSpec{
		ContactFirstName: &a.ContactFirstName, ContactLastName: &a.ContactLastName,
		ContactEmail: &a.ContactEmail, ContactPhone: &a.ContactPhone,
		DemoAccountName: &a.DemoAccountName, Notes: &a.Notes,
		DemoAccountRequired: a.DemoAccountRequired,
	}, nil
}

func fetchBetaBuildMetadataSpecs(ctx context.Context, c *asc.Client, appID string, selectors []config.BetaBuildSelector) ([]config.BetaBuildMetadataSpec, error) {
	var result []config.BetaBuildMetadataSpec
	seen := make(map[string]bool, len(selectors))
	for _, selector := range selectors {
		buildID, err := resolveBetaBuildSelector(ctx, c, appID, selector)
		if err != nil {
			return nil, err
		}
		if seen[buildID] {
			continue
		}
		seen[buildID] = true
		rows, err := asc.ListBetaBuildLocalizations(ctx, c, buildID)
		if err != nil {
			return nil, err
		}
		build := config.BetaBuildMetadataSpec{Build: selector}
		if len(rows) != 0 {
			build.Localizations = make(map[string]config.BetaBuildLocalizationSpec, len(rows))
		}
		for _, row := range rows {
			locale := row.Attributes.Locale
			if locale == "" {
				return nil, fmt.Errorf("beta build localization %s has no locale", row.ID)
			}
			if _, exists := build.Localizations[locale]; exists {
				return nil, fmt.Errorf("multiple beta build localizations for build %s locale %s", selector.Number, locale)
			}
			value := row.Attributes.WhatsNew
			build.Localizations[locale] = config.BetaBuildLocalizationSpec{WhatsNew: &value}
		}
		result = append(result, build)
	}
	return result, nil
}

func resolveBetaBuildSelector(ctx context.Context, c *asc.Client, appID string, selector config.BetaBuildSelector) (string, error) {
	if selector.Number == "" || selector.Version == "" || selector.Platform == "" {
		return "", errors.New("beta build selector requires number, version, and platform")
	}
	query := url.Values{
		"filter[app]": {appID}, "filter[version]": {selector.Number},
		"filter[preReleaseVersion.version]":  {selector.Version},
		"filter[preReleaseVersion.platform]": {selector.Platform}, "limit": {"2"},
	}
	resp, err := asc.Get[asc.Collection[asc.BuildAttributes]](ctx, c, "/v1/builds", query)
	if err != nil {
		return "", fmt.Errorf("resolve beta build %s (%s/%s): %w", selector.Number, selector.Version, selector.Platform, err)
	}
	if len(resp.Data) != 1 || resp.Links.Next != "" {
		return "", fmt.Errorf("beta build %s (%s/%s) resolved to %d builds; choose an exact existing build", selector.Number, selector.Version, selector.Platform, len(resp.Data))
	}
	build := resp.Data[0]
	if build.ID == "" || build.Attributes.Version != selector.Number {
		return "", fmt.Errorf("beta build lookup returned an incomplete or mismatched build for %s", selector.Number)
	}
	path := "/v1/builds/" + url.PathEscape(build.ID) + "/preReleaseVersion"
	pre, err := asc.Get[asc.Single[betaPreReleaseVersionAttributes]](ctx, c, path, nil)
	if err != nil {
		return "", fmt.Errorf("verify beta build %s prerelease version: %w", build.ID, err)
	}
	if pre.Data.Attributes.Version != selector.Version || pre.Data.Attributes.Platform != selector.Platform {
		return "", fmt.Errorf("beta build %s identity mismatch: requested %s/%s, observed %s/%s", build.ID,
			selector.Version, selector.Platform, pre.Data.Attributes.Version, pre.Data.Attributes.Platform)
	}
	return build.ID, nil
}
