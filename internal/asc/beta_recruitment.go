package asc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

type BetaRecruitmentFilter struct {
	DeviceFamily       string `json:"deviceFamily,omitempty"`
	MinimumOSInclusive string `json:"minimumOsInclusive,omitempty"`
	MaximumOSInclusive string `json:"maximumOsInclusive,omitempty"`
}

type BetaRecruitmentCriterionAttributes struct {
	LastModifiedDate             string                  `json:"lastModifiedDate,omitempty"`
	DeviceFamilyOSVersionFilters []BetaRecruitmentFilter `json:"deviceFamilyOsVersionFilters,omitempty"`
}

type BetaRecruitmentOptionDevice struct {
	DeviceFamily string   `json:"deviceFamily,omitempty"`
	OSVersions   []string `json:"osVersions,omitempty"`
}

type BetaRecruitmentOptionAttributes struct {
	DeviceFamilyOSVersions []BetaRecruitmentOptionDevice `json:"deviceFamilyOsVersions,omitempty"`
}

func GetBetaRecruitmentCriterion(ctx context.Context, c *Client, groupID string) (*Resource[BetaRecruitmentCriterionAttributes], error) {
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/betaRecruitmentCriteria"
	resp, err := Get[Single[BetaRecruitmentCriterionAttributes]](ctx, c, path, nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get beta recruitment criteria for group %s: %w", groupID, err)
	}
	if resp.Data.ID == "" || resp.Data.Type != "betaRecruitmentCriteria" {
		return nil, fmt.Errorf("group %s returned incomplete beta recruitment criteria", groupID)
	}
	return &resp.Data, nil
}

func ListBetaRecruitmentOptions(ctx context.Context, c *Client) ([]Resource[BetaRecruitmentOptionAttributes], error) {
	var out []Resource[BetaRecruitmentOptionAttributes]
	for page, err := range Pages[BetaRecruitmentOptionAttributes](ctx, c, "/v1/betaRecruitmentCriterionOptions", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list beta recruitment options: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "betaRecruitmentCriterionOptions" {
				return nil, errors.New("beta recruitment options response contains incomplete row")
			}
		}
		out = append(out, page.Data...)
	}
	return out, nil
}

func UpdateBuildAutoNotify(ctx context.Context, c *Client, detailID string, enabled bool) (Resource[BuildBetaDetailAttributes], error) {
	path := "/v1/buildBetaDetails/" + url.PathEscape(detailID)
	body := map[string]any{"data": map[string]any{
		"type": "buildBetaDetails", "id": detailID,
		"attributes": map[string]any{"autoNotifyEnabled": enabled},
	}}
	resp, err := Patch[Single[BuildBetaDetailAttributes]](ctx, c, path, nil, body)
	if err != nil {
		return Resource[BuildBetaDetailAttributes]{}, fmt.Errorf("set build auto-notify: %w", err)
	}
	if resp.Data.ID != detailID || resp.Data.Type != "buildBetaDetails" || resp.Data.Attributes.AutoNotifyEnabled == nil || *resp.Data.Attributes.AutoNotifyEnabled != enabled {
		return Resource[BuildBetaDetailAttributes]{}, fmt.Errorf("build beta detail %s did not confirm auto-notify=%t; inspect live state", detailID, enabled)
	}
	return resp.Data, nil
}

func NotifyBetaBuild(ctx context.Context, c *Client, buildID string) (Resource[EmptyAttributes], error) {
	body := map[string]any{"data": map[string]any{
		"type":          "buildBetaNotifications",
		"relationships": map[string]any{"build": map[string]any{"data": map[string]string{"type": "builds", "id": buildID}}},
	}}
	resp, err := Post[Single[EmptyAttributes]](ctx, c, "/v1/buildBetaNotifications", nil, body)
	if err != nil {
		return Resource[EmptyAttributes]{}, fmt.Errorf("send beta build notification for %s: %w; outcome may be uncertain, inspect App Store Connect before retrying", buildID, err)
	}
	if resp.Data.ID == "" || resp.Data.Type != "buildBetaNotifications" {
		return Resource[EmptyAttributes]{}, fmt.Errorf("beta notification for build %s returned incomplete confirmation; inspect App Store Connect before retrying", buildID)
	}
	return resp.Data, nil
}
