package state

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func optionalMissing(err error) bool {
	var apiErr *asc.APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound
}

func fetchAppInfoSurfaces(ctx context.Context, c *asc.Client, appID, versionID string, out *State) error {
	appInfoID, err := fetchEditableAppInfo(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: app info: %w", err)
	}
	if appInfoID != "" {
		if ar, err := fetchAgeRating(ctx, c, appInfoID); err == nil {
			if ar != nil {
				out.Spec.AgeRating = projectAgeRating(*ar)
			}
		} else if !optionalMissing(err) {
			return fmt.Errorf("state: age rating: %w", err)
		}
		cats, err := fetchCategories(ctx, c, appInfoID)
		if err != nil {
			return fmt.Errorf("state: categories: %w", err)
		}
		out.Spec.Categories = cats
	}
	md, err := fetchMetadataLocales(ctx, c, versionID, appInfoID)
	if err != nil {
		return fmt.Errorf("state: metadata: %w", err)
	}
	out.Spec.Metadata = md
	return nil
}

func fetchVersionScopedSurfaces(ctx context.Context, c *asc.Client, versionID string, out *State) (string, error) {
	phased, err := FetchPhasedRelease(ctx, c, versionID)
	if err != nil {
		return "", fmt.Errorf("state: phased release: %w", err)
	}
	if out.Spec.Version == nil {
		out.Spec.Version = &config.VersionSpec{}
	}
	out.Spec.Version.PhasedRelease = phased

	buildID, encryption, err := fetchVersionBuildEncryption(ctx, c, versionID)
	if err != nil && !optionalMissing(err) {
		return "", fmt.Errorf("state: version build: %w", err)
	}
	if err == nil && buildID != "" {
		out.Spec.ExportCompliance = &config.ExportComplianceSpec{UsesNonExemptEncryption: encryption}
		num, err := fetchBuildNumber(ctx, c, buildID)
		if err != nil {
			return "", fmt.Errorf("state: build %s: %w", buildID, err)
		}
		out.Spec.Build = &config.BuildSpec{Number: num}
		declaration, err := fetchExportDeclaration(ctx, c, buildID)
		if err != nil {
			return "", fmt.Errorf("state: build %s export declaration: %w", buildID, err)
		}
		out.Spec.ExportCompliance.Declaration = declaration
	}
	rd, err := fetchReviewerDemo(ctx, c, versionID)
	if err != nil {
		return "", fmt.Errorf("state: review detail: %w", err)
	}
	out.Spec.ReviewerDemo = rd
	ss, err := fetchScreenshots(ctx, c, versionID)
	if err != nil {
		return "", fmt.Errorf("state: screenshots: %w", err)
	}
	out.Spec.Screenshots = ss
	previews, err := FetchPreviews(ctx, c, versionID)
	if err != nil {
		return "", fmt.Errorf("state: previews: %w", err)
	}
	out.Spec.Previews = previews
	return buildID, nil
}

func fetchAppScopedSurfaces(ctx context.Context, c *asc.Client, appID string, out *State, opts FetchOpts) error {
	rights, err := FetchContentRights(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: content rights: %w", err)
	}
	out.Spec.ContentRights = rights
	eula, err := FetchAppEULA(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: app EULA: %w", err)
	}
	out.Spec.AppEULA = eula

	availability, err := FetchAppAvailability(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: app availability: %w", err)
	}
	out.Spec.AppAvailability = availability
	accessibility, err := FetchAccessibilityDeclarations(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: accessibility declarations: %w", err)
	}
	out.Spec.AccessibilityDeclarations = accessibility
	pr, err := fetchPricing(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: pricing: %w", err)
	}
	out.Spec.Pricing = pr
	iaps, err := fetchIAPs(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: IAPs: %w", err)
	}
	if iaps != nil && len(iaps.Products) > 0 {
		out.Spec.IAP = iaps
	}
	tf, err := fetchTestFlightGroupsWithOptions(ctx, c, appID, opts)
	if err != nil {
		return fmt.Errorf("state: TestFlight groups: %w", err)
	}
	if tf != nil && len(tf.Groups) > 0 {
		out.Spec.TestFlight = tf
	}
	cpp, err := fetchCustomProductPages(ctx, c, appID)
	if err != nil {
		return fmt.Errorf("state: custom product pages: %w", err)
	}
	if len(cpp) > 0 {
		out.Spec.CustomProductPages = &cpp
	}
	return nil
}
