package asc

import (
	"context"
	"fmt"
	"net/url"
)

// BetaAppLocalizationAttributes carries TestFlight app copy, separate from App Store version copy.
type BetaAppLocalizationAttributes struct {
	Locale            string `json:"locale,omitempty"`
	Description       string `json:"description,omitempty"`
	FeedbackEmail     string `json:"feedbackEmail,omitempty"`
	MarketingURL      string `json:"marketingUrl,omitempty"`
	PrivacyPolicyURL  string `json:"privacyPolicyUrl,omitempty"`
	TVOSPrivacyPolicy string `json:"tvOsPrivacyPolicy,omitempty"`
}

// BetaAppReviewDetailAttributes excludes the password because Apple may return it on reads.
type BetaAppReviewDetailAttributes struct {
	ContactFirstName    string `json:"contactFirstName,omitempty"`
	ContactLastName     string `json:"contactLastName,omitempty"`
	ContactPhone        string `json:"contactPhone,omitempty"`
	ContactEmail        string `json:"contactEmail,omitempty"`
	DemoAccountName     string `json:"demoAccountName,omitempty"`
	DemoAccountRequired *bool  `json:"demoAccountRequired,omitempty"`
	Notes               string `json:"notes,omitempty"`
}

func ListBetaAppLocalizations(ctx context.Context, c *Client, appID string) ([]Resource[BetaAppLocalizationAttributes], error) {
	var out []Resource[BetaAppLocalizationAttributes]
	query := url.Values{"limit": {"200"}}
	path := "/v1/apps/" + url.PathEscape(appID) + "/betaAppLocalizations"
	for page, err := range Pages[BetaAppLocalizationAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, fmt.Errorf("list beta app localizations: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "betaAppLocalizations" || row.Attributes.Locale == "" {
				return nil, fmt.Errorf("app %s returned incomplete beta app localization", appID)
			}
		}
		out = append(out, page.Data...)
	}
	return out, nil
}

func ListBetaBuildLocalizations(ctx context.Context, c *Client, buildID string) ([]Resource[BetaBuildLocalizationAttributes], error) {
	var out []Resource[BetaBuildLocalizationAttributes]
	query := url.Values{"limit": {"200"}}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/betaBuildLocalizations"
	for page, err := range Pages[BetaBuildLocalizationAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, fmt.Errorf("list beta build localizations: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "betaBuildLocalizations" || row.Attributes.Locale == "" {
				return nil, fmt.Errorf("build %s returned incomplete beta build localization", buildID)
			}
		}
		out = append(out, page.Data...)
	}
	return out, nil
}

func GetBetaAppReviewDetail(ctx context.Context, c *Client, appID string) (*Resource[BetaAppReviewDetailAttributes], error) {
	query := url.Values{"filter[app]": {appID}, "limit": {"200"}}
	var detail *Resource[BetaAppReviewDetailAttributes]
	for page, err := range Pages[BetaAppReviewDetailAttributes](ctx, c, "/v1/betaAppReviewDetails", query) {
		if err != nil {
			return nil, fmt.Errorf("get beta app review detail: %w", err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "betaAppReviewDetails" {
				return nil, fmt.Errorf("app %s returned incomplete beta app review detail", appID)
			}
			if detail != nil {
				return nil, fmt.Errorf("app %q has multiple beta app review details", appID)
			}
			selected := row
			detail = &selected
		}
	}
	return detail, nil
}

func CreateBetaAppLocalization(ctx context.Context, c *Client, appID, locale string, attributes map[string]any) (Resource[BetaAppLocalizationAttributes], error) {
	attributes["locale"] = locale
	body := map[string]any{"data": map[string]any{
		"type": "betaAppLocalizations", "attributes": attributes,
		"relationships": map[string]any{"app": map[string]any{"data": map[string]string{"type": "apps", "id": appID}}},
	}}
	resp, err := Post[Single[BetaAppLocalizationAttributes]](ctx, c, "/v1/betaAppLocalizations", nil, body)
	if err != nil {
		return Resource[BetaAppLocalizationAttributes]{}, fmt.Errorf("create beta app localization %s: %w", locale, err)
	}
	return resp.Data, nil
}

func UpdateBetaAppLocalization(ctx context.Context, c *Client, id string, attributes map[string]any) (Resource[BetaAppLocalizationAttributes], error) {
	body := map[string]any{"data": map[string]any{"type": "betaAppLocalizations", "id": id, "attributes": attributes}}
	resp, err := Patch[Single[BetaAppLocalizationAttributes]](ctx, c, "/v1/betaAppLocalizations/"+url.PathEscape(id), nil, body)
	if err != nil {
		return Resource[BetaAppLocalizationAttributes]{}, fmt.Errorf("update beta app localization %s: %w", id, err)
	}
	return resp.Data, nil
}

func CreateBetaBuildLocalization(ctx context.Context, c *Client, buildID, locale string, attributes map[string]any) (Resource[BetaBuildLocalizationAttributes], error) {
	attributes["locale"] = locale
	body := map[string]any{"data": map[string]any{
		"type": "betaBuildLocalizations", "attributes": attributes,
		"relationships": map[string]any{"build": map[string]any{"data": map[string]string{"type": "builds", "id": buildID}}},
	}}
	resp, err := Post[Single[BetaBuildLocalizationAttributes]](ctx, c, "/v1/betaBuildLocalizations", nil, body)
	if err != nil {
		return Resource[BetaBuildLocalizationAttributes]{}, fmt.Errorf("create beta build localization %s: %w", locale, err)
	}
	return resp.Data, nil
}

func UpdateBetaBuildLocalization(ctx context.Context, c *Client, id string, attributes map[string]any) (Resource[BetaBuildLocalizationAttributes], error) {
	body := map[string]any{"data": map[string]any{"type": "betaBuildLocalizations", "id": id, "attributes": attributes}}
	resp, err := Patch[Single[BetaBuildLocalizationAttributes]](ctx, c, "/v1/betaBuildLocalizations/"+url.PathEscape(id), nil, body)
	if err != nil {
		return Resource[BetaBuildLocalizationAttributes]{}, fmt.Errorf("update beta build localization %s: %w", id, err)
	}
	return resp.Data, nil
}

func UpdateBetaAppReviewDetail(ctx context.Context, c *Client, id string, attributes map[string]any) (Resource[BetaAppReviewDetailAttributes], error) {
	body := map[string]any{"data": map[string]any{"type": "betaAppReviewDetails", "id": id, "attributes": attributes}}
	resp, err := Patch[Single[BetaAppReviewDetailAttributes]](ctx, c, "/v1/betaAppReviewDetails/"+url.PathEscape(id), nil, body)
	if err != nil {
		return Resource[BetaAppReviewDetailAttributes]{}, fmt.Errorf("update beta app review detail %s: %w", id, err)
	}
	return resp.Data, nil
}
