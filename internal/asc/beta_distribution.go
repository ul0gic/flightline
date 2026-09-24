package asc

import (
	"context"
	"fmt"
	"net/url"
)

type BuildBetaDetailAttributes struct {
	AutoNotifyEnabled  *bool  `json:"autoNotifyEnabled,omitempty"`
	InternalBuildState string `json:"internalBuildState,omitempty"`
	ExternalBuildState string `json:"externalBuildState,omitempty"`
}

func ListBetaGroupBuilds(ctx context.Context, c *Client, groupID string) ([]Resource[BuildAttributes], error) {
	var out []Resource[BuildAttributes]
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/builds"
	for page, err := range Pages[BuildAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list beta group builds %s: %w", groupID, err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "builds" || row.Attributes.Version == "" {
				return nil, fmt.Errorf("beta group %s returned incomplete build", groupID)
			}
		}
		out = append(out, page.Data...)
	}
	return out, nil
}

func GetBuildBetaDetail(ctx context.Context, c *Client, buildID string) (Resource[BuildBetaDetailAttributes], error) {
	path := "/v1/builds/" + url.PathEscape(buildID) + "/buildBetaDetail"
	resp, err := Get[Single[BuildBetaDetailAttributes]](ctx, c, path, nil)
	if err != nil {
		return Resource[BuildBetaDetailAttributes]{}, fmt.Errorf("get build beta detail %s: %w", buildID, err)
	}
	if resp.Data.ID == "" || resp.Data.Type != "buildBetaDetails" {
		return Resource[BuildBetaDetailAttributes]{}, fmt.Errorf("build %s has incomplete beta detail", buildID)
	}
	return resp.Data, nil
}

func AddBetaGroupBuild(ctx context.Context, c *Client, groupID, buildID string) error {
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/relationships/builds"
	body := map[string]any{"data": []map[string]string{{"type": "builds", "id": buildID}}}
	if _, err := Post[struct{}](ctx, c, path, nil, body); err != nil {
		return fmt.Errorf("add build %s to beta group %s: %w", buildID, groupID, err)
	}
	return nil
}

func RemoveBetaGroupBuild(ctx context.Context, c *Client, groupID, buildID string) error {
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/relationships/builds"
	body := map[string]any{"data": []map[string]string{{"type": "builds", "id": buildID}}}
	if err := c.DeleteWithBody(ctx, path, nil, body); err != nil {
		return fmt.Errorf("remove build %s from beta group %s: %w", buildID, groupID, err)
	}
	return nil
}
