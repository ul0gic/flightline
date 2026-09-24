package state

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func wantsBetaGroupBuilds(opts FetchOpts, name string) bool {
	return opts.IncludeBetaBuilds || (opts.BetaDesired != nil && opts.BetaDesired.Groups[name].Builds != nil)
}

// Observe the attached build plus explicitly managed historical builds, never every build on the app.
func fetchBetaSurfaces(ctx context.Context, c *asc.Client, appID, buildID string, opts FetchOpts, out *State) error {
	selectors := []config.BetaBuildSelector{}
	if buildID != "" {
		pre, err := asc.Get[asc.Single[betaPreReleaseVersionAttributes]](ctx, c, "/v1/builds/"+url.PathEscape(buildID)+"/preReleaseVersion", nil)
		if err != nil {
			return fmt.Errorf("state: attached beta build identity: %w", err)
		}
		if pre.Data.ID == "" || pre.Data.Type != "preReleaseVersions" || pre.Data.Attributes.Version == "" || pre.Data.Attributes.Platform == "" || out.Spec.Build == nil || out.Spec.Build.Number == "" {
			return fmt.Errorf("state: attached beta build %s has incomplete identity", buildID)
		}
		selectors = append(selectors, config.BetaBuildSelector{Number: out.Spec.Build.Number, Version: pre.Data.Attributes.Version, Platform: pre.Data.Attributes.Platform})
	}
	if opts.BetaDesired != nil && opts.BetaDesired.Metadata != nil {
		for _, build := range opts.BetaDesired.Metadata.Builds {
			selectors = append(selectors, build.Build)
		}
	}
	selectors = uniqueBetaSelectors(selectors)
	metadata, err := FetchBetaMetadata(ctx, c, appID, selectors)
	if err != nil {
		return fmt.Errorf("state: beta metadata: %w", err)
	}
	if metadata != nil {
		if out.Spec.TestFlight == nil {
			out.Spec.TestFlight = &config.TestFlightSpec{}
		}
		out.Spec.TestFlight.Metadata = metadata
	}
	return nil
}

func uniqueBetaSelectors(selectors []config.BetaBuildSelector) []config.BetaBuildSelector {
	seen := make(map[config.BetaBuildSelector]bool, len(selectors))
	result := make([]config.BetaBuildSelector, 0, len(selectors))
	for _, selector := range selectors {
		if !seen[selector] {
			result = append(result, selector)
			seen[selector] = true
		}
	}
	return result
}
