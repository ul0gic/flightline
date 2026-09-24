package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF7C_WebhookDeliveryReadDropsUntrustedPayloadAndPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "next" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhookDeliveries","id":"D2","attributes":{"deliveryState":"PENDING","request":{"url":"https://second.example.test/path"}}}]}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"type":"webhookDeliveries","id":"D1","attributes":{"createdDate":"2026-09-23T00:00:00Z","deliveryState":"FAILED","errorMessage":"poison-secret","request":{"url":"https://user:poison-secret@hooks.example.test/path?token=poison-secret"},"response":{"httpStatusCode":500,"body":"poison-secret"}}}],"included":[{"type":"webhookEvents","id":"E1","attributes":{"payload":"poison-secret"}}],"links":{"next":%q}}`, srv.URL+"/v1/webhooks/W1/deliveries?cursor=next")
	}))
	defer srv.Close()
	rows, err := ListWebhookDeliveries(context.Background(), fixtureClient(t, srv), "W1")
	if err != nil || len(rows) != 2 || rows[0].State != "FAILED" || rows[0].StatusCode == nil || *rows[0].StatusCode != 500 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	encoded, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(encoded), "poison-secret") || strings.Contains(string(encoded), "path") {
		t.Fatalf("unsafe delivery JSON=%s err=%v", encoded, err)
	}
}

func TestF7C_WebhookPingAndRedeliveryCanonicalPayloads(t *testing.T) {
	var calls []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls = append(calls, body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/webhookPings" {
			_, _ = fmt.Fprint(w, `{"data":{"type":"webhookPings","id":"P1"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"type":"webhookDeliveries","id":"D2","attributes":{"deliveryState":"PENDING","errorMessage":"poison-secret","response":{"body":"poison-secret"}}}}`)
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if id, err := PingWebhook(context.Background(), c, "W1"); err != nil || id != "P1" {
		t.Fatalf("ping=%s err=%v", id, err)
	}
	delivery, err := RedeliverWebhookDelivery(context.Background(), c, "D1")
	if err != nil || delivery.ID != "D2" {
		t.Fatalf("delivery=%+v err=%v", delivery, err)
	}
	encoded, _ := json.Marshal(delivery)
	if strings.Contains(string(encoded), "poison-secret") {
		t.Fatalf("redelivery leaked: %s", encoded)
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
	pingData, _ := calls[0]["data"].(map[string]any)
	pingRels, _ := pingData["relationships"].(map[string]any)
	ping, _ := pingRels["webhook"].(map[string]any)
	pingRef, _ := ping["data"].(map[string]any)
	redeliverData, _ := calls[1]["data"].(map[string]any)
	redeliverRels, _ := redeliverData["relationships"].(map[string]any)
	template, _ := redeliverRels["template"].(map[string]any)
	templateRef, _ := template["data"].(map[string]any)
	if pingRef["id"] != "W1" || pingRef["type"] != "webhooks" || templateRef["id"] != "D1" || templateRef["type"] != "webhookDeliveries" {
		t.Fatalf("payloads=%+v", calls)
	}
}

func TestG7_RedeliveryRejectsUnconfirmedStateOrReusedIdentity(t *testing.T) {
	for _, body := range []string{
		`{"data":{"type":"webhookDeliveries","id":"NEW"}}`,
		`{"data":{"type":"webhookDeliveries","id":"OLD","attributes":{"deliveryState":"PENDING"}}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		_, err := RedeliverWebhookDelivery(context.Background(), fixtureClient(t, srv), "OLD")
		srv.Close()
		if err == nil {
			t.Fatal("unconfirmed redelivery reported as success")
		}
	}
}
