package asc

import (
	"context"
	"fmt"
	"net/url"
)

// AppTagAttributes is the API 4.5 subset used to inspect assigned app tags and their visibility.
type AppTagAttributes struct {
	Name              string `json:"name,omitempty"`
	VisibleInAppStore *bool  `json:"visibleInAppStore,omitempty"`
}

// ListAppTags returns every tag assigned to an app, following Apple's pagination links.
func ListAppTags(ctx context.Context, c *Client, appID string) ([]Resource[AppTagAttributes], error) {
	query := url.Values{
		"fields[appTags]": {"name,visibleInAppStore"},
		"limit":           {"200"},
	}
	tags := make([]Resource[AppTagAttributes], 0)
	for page, err := range Pages[AppTagAttributes](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appTags", query) {
		if err != nil {
			return nil, fmt.Errorf("asc: list app tags for app %q: %w", appID, err)
		}
		tags = append(tags, page.Data...)
	}
	return tags, nil
}

// PatchAppTagVisibility updates only the current AppTag visibility attribute.
func PatchAppTagVisibility(ctx context.Context, c *Client, tagID string, visible bool) (Resource[AppTagAttributes], error) {
	request := struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				VisibleInAppStore bool `json:"visibleInAppStore"`
			} `json:"attributes"`
		} `json:"data"`
	}{}
	request.Data.Type = "appTags"
	request.Data.ID = tagID
	request.Data.Attributes.VisibleInAppStore = visible
	response, err := Patch[Single[AppTagAttributes]](ctx, c, "/v1/appTags/"+url.PathEscape(tagID), nil, request)
	if err != nil {
		return Resource[AppTagAttributes]{}, fmt.Errorf("asc: update visibility for app tag %q: %w", tagID, err)
	}
	return response.Data, nil
}
