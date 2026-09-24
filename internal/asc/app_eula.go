package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
)

// AppEULA is the complete custom agreement observed for an app.
type AppEULA struct {
	ID            string   `json:"id"`
	AgreementText string   `json:"agreementText"`
	Territories   []string `json:"territories"`
}

type appEULAAttributes struct {
	AgreementText *string `json:"agreementText"`
}

type appEULAResponse struct {
	Data *Resource[appEULAAttributes] `json:"data"`
}

// ReadAppEULA returns nil when the app has no custom agreement. Territory reads
// follow all pages so callers never mistake a partial relationship for the set.
func ReadAppEULA(ctx context.Context, c *Client, appID string) (*AppEULA, error) {
	if appID == "" {
		return nil, errors.New("asc: app id is required")
	}
	path := "/v1/apps/" + url.PathEscape(appID) + "/endUserLicenseAgreement"
	response, err := Get[appEULAResponse](ctx, c, path, nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return nil, nil
		}
		return nil, fmt.Errorf("asc: read EULA for app %q: %w", appID, err)
	}
	if response.Data == nil {
		return nil, nil
	}
	if response.Data.ID == "" || response.Data.Type != "endUserLicenseAgreements" || response.Data.Attributes.AgreementText == nil {
		return nil, fmt.Errorf("asc: EULA response for app %q is incomplete or has unexpected type", appID)
	}
	out := &AppEULA{
		ID:            response.Data.ID,
		AgreementText: *response.Data.Attributes.AgreementText,
		Territories:   make([]string, 0),
	}
	territoryPath := "/v1/endUserLicenseAgreements/" + url.PathEscape(out.ID) + "/territories"
	query := url.Values{"limit": {"200"}}
	seen := make(map[string]bool)
	for page, err := range Pages[EmptyAttributes](ctx, c, territoryPath, query) {
		if err != nil {
			return nil, fmt.Errorf("asc: read territories for EULA %q: %w", out.ID, err)
		}
		for _, row := range page.Data {
			if row.ID == "" || row.Type != "territories" {
				return nil, fmt.Errorf("asc: EULA %q returned an incomplete territory", out.ID)
			}
			if seen[row.ID] {
				return nil, fmt.Errorf("asc: EULA %q returned duplicate territory %q", out.ID, row.ID)
			}
			seen[row.ID] = true
			out.Territories = append(out.Territories, row.ID)
		}
	}
	slices.Sort(out.Territories)
	return out, nil
}

// CreateAppEULA creates one custom agreement with an exact territory set.
func CreateAppEULA(ctx context.Context, c *Client, appID, text string, territories []string) (string, error) {
	if appID == "" || text == "" || len(territories) == 0 {
		return "", errors.New("asc: EULA create requires app, agreement text, and territories")
	}
	body := map[string]any{"data": map[string]any{
		"type":       "endUserLicenseAgreements",
		"attributes": map[string]string{"agreementText": text},
		"relationships": map[string]any{
			"app":         map[string]any{"data": map[string]string{"type": "apps", "id": appID}},
			"territories": map[string]any{"data": eulaTerritoryRefs(territories)},
		},
	}}
	response, err := Post[Single[appEULAAttributes]](ctx, c, "/v1/endUserLicenseAgreements", nil, body)
	if err != nil {
		return "", fmt.Errorf("asc: create EULA for app %q: %w", appID, err)
	}
	if response.Data.ID == "" || response.Data.Type != "endUserLicenseAgreements" {
		return "", fmt.Errorf("asc: create EULA for app %q returned an incomplete resource", appID)
	}
	return response.Data.ID, nil
}

// PatchAppEULA updates only supplied fields. Nil means preserve.
func PatchAppEULA(ctx context.Context, c *Client, id string, text *string, territories *[]string) error {
	if id == "" || text == nil && territories == nil {
		return errors.New("asc: EULA update requires id and at least one explicit field")
	}
	data := map[string]any{"type": "endUserLicenseAgreements", "id": id}
	if text != nil {
		if *text == "" {
			return errors.New("asc: EULA agreement text must be nonempty")
		}
		data["attributes"] = map[string]string{"agreementText": *text}
	}
	if territories != nil {
		if len(*territories) == 0 {
			return errors.New("asc: EULA territory set must be nonempty")
		}
		data["relationships"] = map[string]any{"territories": map[string]any{"data": eulaTerritoryRefs(*territories)}}
	}
	response, err := Patch[Single[appEULAAttributes]](ctx, c, "/v1/endUserLicenseAgreements/"+url.PathEscape(id), nil, map[string]any{"data": data})
	if err != nil {
		return fmt.Errorf("asc: update EULA %q: %w", id, err)
	}
	if response.Data.ID != id || response.Data.Type != "endUserLicenseAgreements" {
		return fmt.Errorf("asc: EULA update %q returned an inconsistent resource", id)
	}
	return nil
}

func DeleteAppEULA(ctx context.Context, c *Client, id string) error {
	if id == "" {
		return errors.New("asc: EULA id is required")
	}
	if err := c.Delete(ctx, "/v1/endUserLicenseAgreements/"+url.PathEscape(id), nil); err != nil {
		return fmt.Errorf("asc: delete EULA %q: %w", id, err)
	}
	return nil
}

func eulaTerritoryRefs(territories []string) []map[string]string {
	refs := make([]map[string]string, 0, len(territories))
	for _, territory := range territories {
		refs = append(refs, map[string]string{"type": "territories", "id": territory})
	}
	return refs
}
