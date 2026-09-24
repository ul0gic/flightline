package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

const (
	ContentRightsDoesNotUseThirdParty = "DOES_NOT_USE_THIRD_PARTY_CONTENT"
	ContentRightsUsesThirdParty       = "USES_THIRD_PARTY_CONTENT"
)

// ContentRightsDeclaration is Apple's observed declaration, if present.
type ContentRightsDeclaration struct {
	AppID       string  `json:"appId"`
	Declaration *string `json:"contentRightsDeclaration"`
}

type contentRightsAttributes struct {
	Declaration *string `json:"contentRightsDeclaration"`
}

// ReadContentRights returns only an explicitly observed legal declaration.
func ReadContentRights(ctx context.Context, c *Client, appID string) (ContentRightsDeclaration, error) {
	if appID == "" {
		return ContentRightsDeclaration{}, errors.New("asc: app id is required")
	}
	query := url.Values{"fields[apps]": {"contentRightsDeclaration"}}
	response, err := Get[Single[contentRightsAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(appID), query)
	if err != nil {
		return ContentRightsDeclaration{}, fmt.Errorf("asc: read content rights for app %q: %w", appID, err)
	}
	if response.Data.Type != "apps" || response.Data.ID != appID {
		return ContentRightsDeclaration{}, fmt.Errorf("asc: content rights response for app %q is incomplete or inconsistent", appID)
	}
	declaration := response.Data.Attributes.Declaration
	if declaration != nil && !validContentRightsDeclaration(*declaration) {
		return ContentRightsDeclaration{}, fmt.Errorf("asc: app %q returned an unknown content rights declaration", appID)
	}
	return ContentRightsDeclaration{AppID: appID, Declaration: declaration}, nil
}

func validContentRightsDeclaration(value string) bool {
	return value == ContentRightsDoesNotUseThirdParty || value == ContentRightsUsesThirdParty
}

// PatchContentRights sends only the caller's explicit declaration.
func PatchContentRights(ctx context.Context, c *Client, appID, declaration string) error {
	if appID == "" || !validContentRightsDeclaration(declaration) {
		return errors.New("asc: app id and an explicit supported content rights declaration are required")
	}
	body := map[string]any{"data": map[string]any{
		"type": "apps", "id": appID,
		"attributes": map[string]string{"contentRightsDeclaration": declaration},
	}}
	response, err := Patch[Single[contentRightsAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(appID), nil, body)
	if err != nil {
		return fmt.Errorf("asc: update content rights for app %q: %w", appID, err)
	}
	if response.Data.ID != appID || response.Data.Type != "apps" {
		return fmt.Errorf("asc: content rights update for app %q returned an inconsistent resource", appID)
	}
	return nil
}
