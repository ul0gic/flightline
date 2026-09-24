package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// AppEncryptionDeclarationCreateAttributes are the four attributes ASC requires together on creation.
type AppEncryptionDeclarationCreateAttributes struct {
	AppDescription                  string `json:"appDescription"`
	ContainsProprietaryCryptography bool   `json:"containsProprietaryCryptography"`
	ContainsThirdPartyCryptography  bool   `json:"containsThirdPartyCryptography"`
	AvailableOnFrenchStore          bool   `json:"availableOnFrenchStore"`
}

// GetBuildEncryptionDeclaration reads the declaration explicitly associated with one build.
func GetBuildEncryptionDeclaration(ctx context.Context, c *Client, buildID string) (*Resource[AppEncryptionDeclarationAttributes], error) {
	if strings.TrimSpace(buildID) == "" {
		return nil, errors.New("asc: build ID is required")
	}
	resp, err := Get[struct {
		Data *Resource[AppEncryptionDeclarationAttributes] `json:"data"`
	}](ctx, c, "/v1/builds/"+buildID+"/appEncryptionDeclaration", nil)
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, nil
	}
	if resp.Data.ID == "" {
		return nil, fmt.Errorf("asc: build %s declaration response missing resource id", buildID)
	}
	return resp.Data, nil
}

// ListAppEncryptionDeclarations lists every declaration belonging to one app through the live top-level route.
func ListAppEncryptionDeclarations(ctx context.Context, c *Client, appID string) ([]Resource[AppEncryptionDeclarationAttributes], error) {
	if strings.TrimSpace(appID) == "" {
		return nil, errors.New("asc: app ID is required")
	}
	q := url.Values{"filter[app]": {appID}, "limit": {"200"}}
	var declarations []Resource[AppEncryptionDeclarationAttributes]
	for page, err := range Pages[AppEncryptionDeclarationAttributes](ctx, c, "/v1/appEncryptionDeclarations", q) {
		if err != nil {
			return nil, fmt.Errorf("asc: list app encryption declarations: %w", err)
		}
		declarations = append(declarations, page.Data...)
	}
	return declarations, nil
}

// CreateAppEncryptionDeclaration creates one declaration with ASC's required attributes and app relationship.
func CreateAppEncryptionDeclaration(ctx context.Context, c *Client, appID string, attrs AppEncryptionDeclarationCreateAttributes) (Resource[AppEncryptionDeclarationAttributes], error) {
	if strings.TrimSpace(appID) == "" {
		return Resource[AppEncryptionDeclarationAttributes]{}, errors.New("asc: app ID is required")
	}
	body := struct {
		Data struct {
			Type          string                                   `json:"type"`
			Attributes    AppEncryptionDeclarationCreateAttributes `json:"attributes"`
			Relationships struct {
				App struct {
					Data struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"data"`
				} `json:"app"`
			} `json:"relationships"`
		} `json:"data"`
	}{}
	body.Data.Type = "appEncryptionDeclarations"
	body.Data.Attributes = attrs
	body.Data.Relationships.App.Data.Type = "apps"
	body.Data.Relationships.App.Data.ID = appID

	resp, err := Post[Single[AppEncryptionDeclarationAttributes]](ctx, c, "/v1/appEncryptionDeclarations", nil, body)
	if err != nil {
		return Resource[AppEncryptionDeclarationAttributes]{}, err
	}
	if resp.Data.ID == "" {
		return Resource[AppEncryptionDeclarationAttributes]{}, errors.New("asc: create app encryption declaration response missing resource id")
	}
	return resp.Data, nil
}

// SetBuildEncryptionDeclaration explicitly associates a declaration with a build.
func SetBuildEncryptionDeclaration(ctx context.Context, c *Client, buildID, declarationID string) error {
	if strings.TrimSpace(buildID) == "" {
		return errors.New("asc: build ID is required")
	}
	if strings.TrimSpace(declarationID) == "" {
		return errors.New("asc: declaration ID is required")
	}
	body := map[string]any{
		"data": map[string]string{"type": "appEncryptionDeclarations", "id": declarationID},
	}
	if _, err := Patch[json.RawMessage](ctx, c, "/v1/builds/"+buildID+"/relationships/appEncryptionDeclaration", nil, body); err != nil {
		return err
	}
	return nil
}
