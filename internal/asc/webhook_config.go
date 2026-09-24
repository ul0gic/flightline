package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// WebhookConfig is an allowlisted app webhook; URL is internal and never serialized.
type WebhookConfig struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Enabled    *bool    `json:"enabled"`
	EventTypes []string `json:"eventTypes"`
	Endpoint   string   `json:"endpoint"`
	URL        string   `json:"-"`
}

type webhookAttributes struct {
	Name       string   `json:"name"`
	Enabled    *bool    `json:"enabled"`
	EventTypes []string `json:"eventTypes"`
	URL        string   `json:"url"`
}

// WebhookMutationError discards arbitrary Apple error text from secret-bearing writes.
type WebhookMutationError struct {
	Action string `json:"action"`
	Status int    `json:"status"`
}

// WebhookReadError omits arbitrary server text that could contain endpoint credentials.
type WebhookReadError struct {
	Action string `json:"action"`
	Status int    `json:"status"`
}

func (e *WebhookReadError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("webhook %s failed; inspect app scope and retry the read", e.Action)
	}
	return fmt.Sprintf("webhook %s failed with HTTP %d", e.Action, e.Status)
}

func (e *WebhookMutationError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("webhook %s outcome unconfirmed; inspect app webhooks before retrying", e.Action)
	}
	return fmt.Sprintf("webhook %s failed with HTTP %d; inspect app webhooks before retrying", e.Action, e.Status)
}

func safeWebhookMutationError(action string, err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return &WebhookMutationError{Action: action, Status: apiErr.HTTPStatus}
	}
	return &WebhookMutationError{Action: action}
}

func SafeWebhookReadError(action string, err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return &WebhookReadError{Action: action, Status: apiErr.HTTPStatus}
	}
	return &WebhookReadError{Action: action}
}

func webhookFromResource(row Resource[webhookAttributes]) (WebhookConfig, error) {
	if row.ID == "" || row.Type != "webhooks" || row.Attributes.Name == "" || row.Attributes.URL == "" || row.Attributes.Enabled == nil || row.Attributes.EventTypes == nil {
		return WebhookConfig{}, errors.New("asc: webhook response is incomplete or has unexpected type")
	}
	return WebhookConfig{
		ID: row.ID, Name: row.Attributes.Name, Enabled: row.Attributes.Enabled,
		EventTypes: slices.Clone(row.Attributes.EventTypes), URL: row.Attributes.URL,
		Endpoint: SanitizedWebhookOrigin(row.Attributes.URL),
	}, nil
}

// SanitizedWebhookOrigin exposes only scheme and host from an observed URL.
func SanitizedWebhookOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "[unavailable]"
	}
	return "https://" + parsed.Host
}

func ListAppWebhooks(ctx context.Context, c *Client, appID string) ([]WebhookConfig, error) {
	if appID == "" {
		return nil, errors.New("asc: app id is required")
	}
	path := "/v1/apps/" + url.PathEscape(appID) + "/webhooks"
	query := url.Values{"limit": {"200"}, "fields[webhooks]": {"enabled,eventTypes,name,url"}}
	out := make([]WebhookConfig, 0)
	seen := make(map[string]bool)
	for page, err := range Pages[webhookAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, SafeWebhookReadError("list", err)
		}
		for _, row := range page.Data {
			hook, err := webhookFromResource(row)
			if err != nil || seen[row.ID] {
				return nil, fmt.Errorf("asc: app %q returned incomplete or duplicate webhook", appID)
			}
			seen[row.ID] = true
			out = append(out, hook)
		}
	}
	return out, nil
}

func GetWebhook(ctx context.Context, c *Client, id string) (WebhookConfig, error) {
	if id == "" {
		return WebhookConfig{}, errors.New("asc: webhook id is required")
	}
	response, err := Get[Single[webhookAttributes]](ctx, c, "/v1/webhooks/"+url.PathEscape(id), nil)
	if err != nil {
		return WebhookConfig{}, SafeWebhookReadError("get", err)
	}
	if response.Data.ID != id {
		return WebhookConfig{}, fmt.Errorf("asc: webhook %q response has inconsistent id", id)
	}
	return webhookFromResource(response.Data)
}

type WebhookWriteAttributes struct {
	Name       *string
	Enabled    *bool
	EventTypes *[]string
	URL        *string
	Secret     *string `json:"-"`
}

func CreateAppWebhook(ctx context.Context, c *Client, appID string, attrs WebhookWriteAttributes) (string, error) {
	if appID == "" || attrs.Name == nil || attrs.Enabled == nil || attrs.EventTypes == nil || attrs.URL == nil || attrs.Secret == nil || *attrs.Secret == "" {
		return "", errors.New("asc: webhook create requires app, name, URL, events, enabled state, and nonempty secret")
	}
	attributes := webhookWriteMap(attrs)
	body := map[string]any{"data": map[string]any{
		"type": "webhooks", "attributes": attributes,
		"relationships": map[string]any{"app": map[string]any{"data": map[string]string{"type": "apps", "id": appID}}},
	}}
	response, err := Post[Single[webhookAttributes]](ctx, c, "/v1/webhooks", nil, body)
	if err != nil {
		return "", safeWebhookMutationError("create", err)
	}
	if response.Data.ID == "" || response.Data.Type != "webhooks" {
		return "", &WebhookMutationError{Action: "create"}
	}
	return response.Data.ID, nil
}

func PatchWebhook(ctx context.Context, c *Client, id string, attrs WebhookWriteAttributes) error {
	if id == "" {
		return errors.New("asc: webhook id is required")
	}
	attributes := webhookWriteMap(attrs)
	if attrs.Secret != nil && *attrs.Secret == "" {
		return errors.New("asc: webhook signing secret must be nonempty")
	}
	if len(attributes) == 0 {
		return errors.New("asc: webhook update has no explicit attributes")
	}
	body := map[string]any{"data": map[string]any{"type": "webhooks", "id": id, "attributes": attributes}}
	response, err := Patch[Single[webhookAttributes]](ctx, c, "/v1/webhooks/"+url.PathEscape(id), nil, body)
	if err != nil {
		return safeWebhookMutationError("update", err)
	}
	if response.Data.ID != id || response.Data.Type != "webhooks" {
		return &WebhookMutationError{Action: "update"}
	}
	return nil
}

func webhookWriteMap(attrs WebhookWriteAttributes) map[string]any {
	out := map[string]any{}
	if attrs.Name != nil {
		out["name"] = *attrs.Name
	}
	if attrs.Enabled != nil {
		out["enabled"] = *attrs.Enabled
	}
	if attrs.EventTypes != nil {
		out["eventTypes"] = *attrs.EventTypes
	}
	if attrs.URL != nil {
		out["url"] = *attrs.URL
	}
	if attrs.Secret != nil {
		out["secret"] = *attrs.Secret
	}
	return out
}

func DeleteWebhook(ctx context.Context, c *Client, id string) error {
	if id == "" {
		return errors.New("asc: webhook id is required")
	}
	if err := c.Delete(ctx, "/v1/webhooks/"+url.PathEscape(id), nil); err != nil {
		return safeWebhookMutationError("delete", err)
	}
	return nil
}

var webhookEventTypes = map[string]bool{
	"ALTERNATIVE_DISTRIBUTION_PACKAGE_AVAILABLE_UPDATED":           true,
	"ALTERNATIVE_DISTRIBUTION_PACKAGE_VERSION_CREATED":             true,
	"ALTERNATIVE_DISTRIBUTION_TERRITORY_AVAILABILITY_UPDATED":      true,
	"APP_STORE_VERSION_APP_VERSION_STATE_UPDATED":                  true,
	"BACKGROUND_ASSET_VERSION_APP_STORE_RELEASE_STATE_UPDATED":     true,
	"BACKGROUND_ASSET_VERSION_EXTERNAL_BETA_RELEASE_STATE_UPDATED": true,
	"BACKGROUND_ASSET_VERSION_INTERNAL_BETA_RELEASE_CREATED":       true,
	"BACKGROUND_ASSET_VERSION_STATE_UPDATED":                       true,
	"BETA_FEEDBACK_CRASH_SUBMISSION_CREATED":                       true,
	"BETA_FEEDBACK_SCREENSHOT_SUBMISSION_CREATED":                  true,
	"BUILD_BETA_DETAIL_EXTERNAL_BUILD_STATE_UPDATED":               true,
	"BUILD_UPLOAD_STATE_UPDATED":                                   true,
}

func ValidateWebhookEvents(events []string) error {
	if len(events) == 0 {
		return errors.New("webhook requires at least one event type")
	}
	seen := map[string]bool{}
	for _, event := range events {
		if !webhookEventTypes[event] || seen[event] {
			return fmt.Errorf("unsupported or duplicate webhook event type %q", event)
		}
		seen[event] = true
	}
	return nil
}

func ValidateWebhookURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") || parsed.Opaque != "" || strings.ContainsAny(raw, "\r\n") {
		return errors.New("webhook URL must be HTTPS without userinfo, query, or fragment")
	}
	return nil
}
