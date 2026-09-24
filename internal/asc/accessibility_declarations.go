package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type AccessibilityDeclarationAttributes struct {
	DeviceFamily                           string `json:"deviceFamily,omitempty"`
	State                                  string `json:"state,omitempty"`
	SupportsAudioDescriptions              *bool  `json:"supportsAudioDescriptions,omitempty"`
	SupportsCaptions                       *bool  `json:"supportsCaptions,omitempty"`
	SupportsDarkInterface                  *bool  `json:"supportsDarkInterface,omitempty"`
	SupportsDifferentiateWithoutColorAlone *bool  `json:"supportsDifferentiateWithoutColorAlone,omitempty"`
	SupportsLargerText                     *bool  `json:"supportsLargerText,omitempty"`
	SupportsReducedMotion                  *bool  `json:"supportsReducedMotion,omitempty"`
	SupportsSufficientContrast             *bool  `json:"supportsSufficientContrast,omitempty"`
	SupportsVoiceControl                   *bool  `json:"supportsVoiceControl,omitempty"`
	SupportsVoiceover                      *bool  `json:"supportsVoiceover,omitempty"`
}

type AccessibilityDeclaration struct {
	ID string `json:"id"`
	AccessibilityDeclarationAttributes
}

var accessibilityFamilies = map[string]bool{
	"IPHONE": true, "IPAD": true, "APPLE_TV": true, "APPLE_WATCH": true, "MAC": true, "VISION": true,
}

func ValidAccessibilityFamily(family string) bool { return accessibilityFamilies[family] }

func (a AccessibilityDeclarationAttributes) HasAnswers() bool {
	return a.SupportsAudioDescriptions != nil || a.SupportsCaptions != nil || a.SupportsDarkInterface != nil ||
		a.SupportsDifferentiateWithoutColorAlone != nil || a.SupportsLargerText != nil ||
		a.SupportsReducedMotion != nil || a.SupportsSufficientContrast != nil ||
		a.SupportsVoiceControl != nil || a.SupportsVoiceover != nil
}

func ListAccessibilityDeclarations(ctx context.Context, c *Client, appID string) ([]AccessibilityDeclaration, error) {
	if appID == "" {
		return nil, errors.New("accessibility declarations: app ID is required")
	}
	q := url.Values{"limit": {"200"}}
	var declarations []AccessibilityDeclaration
	for page, err := range Pages[AccessibilityDeclarationAttributes](ctx, c, "/v1/apps/"+appID+"/accessibilityDeclarations", q) {
		if err != nil {
			return nil, fmt.Errorf("list accessibility declarations: %w", err)
		}
		for _, row := range page.Data {
			declaration, err := projectAccessibilityDeclaration(row)
			if err != nil {
				return nil, err
			}
			declarations = append(declarations, declaration)
		}
	}
	return declarations, nil
}

func GetAccessibilityDeclaration(ctx context.Context, c *Client, id string) (AccessibilityDeclaration, error) {
	if id == "" {
		return AccessibilityDeclaration{}, errors.New("accessibility declaration ID is required")
	}
	resp, err := Get[Single[AccessibilityDeclarationAttributes]](ctx, c, "/v1/accessibilityDeclarations/"+url.PathEscape(id), nil)
	if err != nil {
		return AccessibilityDeclaration{}, fmt.Errorf("read accessibility declaration %s: %w", id, err)
	}
	declaration, err := projectAccessibilityDeclaration(resp.Data)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	if declaration.ID != id {
		return AccessibilityDeclaration{}, fmt.Errorf("accessibility declaration %s: response ID mismatch", id)
	}
	return declaration, nil
}

func CreateAccessibilityDeclaration(ctx context.Context, c *Client, appID string, attributes AccessibilityDeclarationAttributes) (AccessibilityDeclaration, error) {
	if appID == "" || !ValidAccessibilityFamily(attributes.DeviceFamily) || !attributes.HasAnswers() {
		return AccessibilityDeclaration{}, errors.New("accessibility declaration create requires app ID, valid device family, and explicit support answers")
	}
	if attributes.State != "" {
		return AccessibilityDeclaration{}, errors.New("accessibility declaration state is read-only; publish explicitly")
	}
	body := map[string]any{"data": map[string]any{
		"type": "accessibilityDeclarations", "attributes": attributes,
		"relationships": map[string]any{"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}}},
	}}
	resp, err := Post[Single[AccessibilityDeclarationAttributes]](ctx, c, "/v1/accessibilityDeclarations", nil, body)
	if err != nil {
		return AccessibilityDeclaration{}, fmt.Errorf("create accessibility declaration: %w", err)
	}
	created, err := projectAccessibilityDeclaration(resp.Data)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	if created.DeviceFamily != attributes.DeviceFamily || created.State != "DRAFT" {
		return AccessibilityDeclaration{}, errors.New("created accessibility declaration response has unexpected family or state")
	}
	return created, nil
}

func UpdateAccessibilityDeclaration(ctx context.Context, c *Client, id string, answers AccessibilityDeclarationAttributes) (AccessibilityDeclaration, error) {
	if !answers.HasAnswers() {
		return AccessibilityDeclaration{}, errors.New("accessibility declaration update requires explicit support answers")
	}
	if answers.DeviceFamily != "" || answers.State != "" {
		return AccessibilityDeclaration{}, errors.New("device family and state cannot be changed")
	}
	current, err := requireDraftAccessibilityDeclaration(ctx, c, id)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	if accessibilityAnswersMatch(answers, current.AccessibilityDeclarationAttributes) {
		return current, nil
	}
	body := map[string]any{"data": map[string]any{"type": "accessibilityDeclarations", "id": id, "attributes": answers}}
	resp, err := Patch[Single[AccessibilityDeclarationAttributes]](ctx, c, "/v1/accessibilityDeclarations/"+url.PathEscape(id), nil, body)
	if err != nil {
		return AccessibilityDeclaration{}, fmt.Errorf("update accessibility declaration %s: %w", id, err)
	}
	updated, err := projectAccessibilityDeclaration(resp.Data)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	if updated.ID != current.ID || updated.DeviceFamily != current.DeviceFamily {
		return AccessibilityDeclaration{}, errors.New("updated accessibility declaration identity mismatch")
	}
	return updated, nil
}

func PublishAccessibilityDeclaration(ctx context.Context, c *Client, id string) (AccessibilityDeclaration, error) {
	current, err := requireDraftAccessibilityDeclaration(ctx, c, id)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	body := map[string]any{"data": map[string]any{
		"type": "accessibilityDeclarations", "id": id, "attributes": map[string]any{"publish": true},
	}}
	resp, err := Patch[Single[AccessibilityDeclarationAttributes]](ctx, c, "/v1/accessibilityDeclarations/"+url.PathEscape(id), nil, body)
	if err != nil {
		return AccessibilityDeclaration{}, fmt.Errorf("publish accessibility declaration %s: %w", id, err)
	}
	published, err := projectAccessibilityDeclaration(resp.Data)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	if published.ID != current.ID || published.DeviceFamily != current.DeviceFamily || published.State != "PUBLISHED" {
		return AccessibilityDeclaration{}, errors.New("publish response did not confirm the expected declaration as PUBLISHED; inspect live state before retrying")
	}
	return published, nil
}

func DeleteAccessibilityDeclaration(ctx context.Context, c *Client, id string) error {
	if _, err := requireDraftAccessibilityDeclaration(ctx, c, id); err != nil {
		return err
	}
	if err := c.Delete(ctx, "/v1/accessibilityDeclarations/"+url.PathEscape(id), nil); err != nil {
		return fmt.Errorf("delete accessibility declaration %s: %w", id, err)
	}
	return nil
}

func requireDraftAccessibilityDeclaration(ctx context.Context, c *Client, id string) (AccessibilityDeclaration, error) {
	current, err := GetAccessibilityDeclaration(ctx, c, id)
	if err != nil {
		return AccessibilityDeclaration{}, err
	}
	if current.State != "DRAFT" {
		return AccessibilityDeclaration{}, fmt.Errorf("accessibility declaration %s is %s; only DRAFT declarations can be modified or deleted", id, current.State)
	}
	return current, nil
}

func projectAccessibilityDeclaration(row Resource[AccessibilityDeclarationAttributes]) (AccessibilityDeclaration, error) {
	if row.Type != "accessibilityDeclarations" || row.ID == "" || !ValidAccessibilityFamily(row.Attributes.DeviceFamily) || !validAccessibilityState(row.Attributes.State) {
		return AccessibilityDeclaration{}, fmt.Errorf("accessibility declaration %q has missing or invalid type, ID, deviceFamily, or state", row.ID)
	}
	return AccessibilityDeclaration{ID: row.ID, AccessibilityDeclarationAttributes: row.Attributes}, nil
}

func validAccessibilityState(state string) bool {
	return state == "DRAFT" || state == "PUBLISHED" || state == "REPLACED"
}

func accessibilityAnswersMatch(desired, current AccessibilityDeclarationAttributes) bool {
	want := []*bool{
		desired.SupportsAudioDescriptions, desired.SupportsCaptions, desired.SupportsDarkInterface,
		desired.SupportsDifferentiateWithoutColorAlone, desired.SupportsLargerText, desired.SupportsReducedMotion,
		desired.SupportsSufficientContrast, desired.SupportsVoiceControl, desired.SupportsVoiceover,
	}
	have := []*bool{
		current.SupportsAudioDescriptions, current.SupportsCaptions, current.SupportsDarkInterface,
		current.SupportsDifferentiateWithoutColorAlone, current.SupportsLargerText, current.SupportsReducedMotion,
		current.SupportsSufficientContrast, current.SupportsVoiceControl, current.SupportsVoiceover,
	}
	for index := range want {
		if want[index] != nil && (have[index] == nil || *want[index] != *have[index]) {
			return false
		}
	}
	return true
}
