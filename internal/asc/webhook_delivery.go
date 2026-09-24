package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// WebhookDelivery is an allowlisted diagnostic without body, error text, or payload.
type WebhookDelivery struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	CreatedDate string `json:"createdDate,omitempty"`
	SentDate    string `json:"sentDate,omitempty"`
	Redelivery  *bool  `json:"redelivery,omitempty"`
	StatusCode  *int   `json:"statusCode,omitempty"`
	Endpoint    string `json:"endpoint"`
}

type webhookDeliveryAttributes struct {
	CreatedDate   string `json:"createdDate"`
	DeliveryState string `json:"deliveryState"`
	Redelivery    *bool  `json:"redelivery"`
	SentDate      string `json:"sentDate"`
	Request       struct {
		URL string `json:"url"`
	} `json:"request"`
	Response struct {
		HTTPStatusCode *int `json:"httpStatusCode"`
	} `json:"response"`
}

func webhookDeliveryFromResource(row Resource[webhookDeliveryAttributes]) (WebhookDelivery, error) {
	if row.Type != "webhookDeliveries" || row.ID == "" || row.Attributes.DeliveryState != "SUCCEEDED" && row.Attributes.DeliveryState != "FAILED" && row.Attributes.DeliveryState != "PENDING" {
		return WebhookDelivery{}, errors.New("asc: webhook delivery response is incomplete or has unexpected type/state")
	}
	attrs := row.Attributes
	return WebhookDelivery{ID: row.ID, State: attrs.DeliveryState, CreatedDate: attrs.CreatedDate, SentDate: attrs.SentDate,
		Redelivery: attrs.Redelivery, StatusCode: attrs.Response.HTTPStatusCode, Endpoint: SanitizedWebhookOrigin(attrs.Request.URL)}, nil
}

func ListWebhookDeliveries(ctx context.Context, c *Client, webhookID string) ([]WebhookDelivery, error) {
	if webhookID == "" {
		return nil, errors.New("asc: webhook id is required")
	}
	path := "/v1/webhooks/" + url.PathEscape(webhookID) + "/deliveries"
	query := url.Values{"limit": {"200"}, "fields[webhookDeliveries]": {"createdDate,deliveryState,redelivery,sentDate,request,response"}}
	out := make([]WebhookDelivery, 0)
	seen := map[string]bool{}
	for page, err := range Pages[webhookDeliveryAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, SafeWebhookReadError("list deliveries", err)
		}
		for _, row := range page.Data {
			delivery, err := webhookDeliveryFromResource(row)
			if err != nil || seen[row.ID] {
				return nil, fmt.Errorf("asc: webhook %q returned incomplete or duplicate delivery", webhookID)
			}
			seen[row.ID] = true
			out = append(out, delivery)
		}
	}
	return out, nil
}

func PingWebhook(ctx context.Context, c *Client, webhookID string) (string, error) {
	if webhookID == "" {
		return "", errors.New("asc: webhook id is required")
	}
	body := map[string]any{"data": map[string]any{"type": "webhookPings", "relationships": map[string]any{
		"webhook": map[string]any{"data": map[string]string{"type": "webhooks", "id": webhookID}},
	}}}
	response, err := Post[Single[EmptyAttributes]](ctx, c, "/v1/webhookPings", nil, body)
	if err != nil {
		return "", safeWebhookMutationError("ping", err)
	}
	if response.Data.ID == "" || response.Data.Type != "webhookPings" {
		return "", &WebhookMutationError{Action: "ping"}
	}
	return response.Data.ID, nil
}

func RedeliverWebhookDelivery(ctx context.Context, c *Client, templateID string) (WebhookDelivery, error) {
	if templateID == "" {
		return WebhookDelivery{}, errors.New("asc: delivery id is required")
	}
	body := map[string]any{"data": map[string]any{"type": "webhookDeliveries", "relationships": map[string]any{
		"template": map[string]any{"data": map[string]string{"type": "webhookDeliveries", "id": templateID}},
	}}}
	response, err := Post[struct {
		Data Resource[webhookDeliveryAttributes] `json:"data"`
	}](ctx, c, "/v1/webhookDeliveries", nil, body)
	if err != nil {
		return WebhookDelivery{}, safeWebhookMutationError("redeliver", err)
	}
	result, err := webhookDeliveryFromResource(response.Data)
	if err != nil || result.ID == templateID {
		return WebhookDelivery{}, &WebhookMutationError{Action: "redeliver"}
	}
	return result, nil
}
