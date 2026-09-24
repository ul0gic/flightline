package state

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

type betaPreReleaseVersionAttributes struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

func FetchBetaGroupBuilds(ctx context.Context, c *asc.Client, groupID string) ([]config.BetaBuildSelector, error) {
	builds, err := asc.ListBetaGroupBuilds(ctx, c, groupID)
	if err != nil {
		return nil, err
	}
	result := make([]config.BetaBuildSelector, 0, len(builds))
	seen := make(map[config.BetaBuildSelector]bool, len(builds))
	for _, build := range builds {
		if build.ID == "" || build.Attributes.Version == "" {
			return nil, fmt.Errorf("beta group %s returned build without ID or number", groupID)
		}
		path := "/v1/builds/" + url.PathEscape(build.ID) + "/preReleaseVersion"
		pre, err := asc.Get[asc.Single[betaPreReleaseVersionAttributes]](ctx, c, path, nil)
		if err != nil {
			return nil, fmt.Errorf("beta group %s build %s prerelease version: %w", groupID, build.ID, err)
		}
		if pre.Data.Attributes.Version == "" || pre.Data.Attributes.Platform == "" {
			return nil, fmt.Errorf("beta group %s build %s lacks version or platform", groupID, build.ID)
		}
		selector := config.BetaBuildSelector{
			Number: build.Attributes.Version, Version: pre.Data.Attributes.Version, Platform: pre.Data.Attributes.Platform,
		}
		if seen[selector] {
			return nil, fmt.Errorf("beta group %s has duplicate build selector %+v", groupID, selector)
		}
		seen[selector] = true
		result = append(result, selector)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Version != result[j].Version {
			return result[i].Version < result[j].Version
		}
		if result[i].Platform != result[j].Platform {
			return result[i].Platform < result[j].Platform
		}
		return result[i].Number < result[j].Number
	})
	return result, nil
}
