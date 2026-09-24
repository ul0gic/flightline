package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF7C_WebhookDeliveryReadsNeverPingOrRedeliver(t *testing.T) {
	actions := 0
	srv := webhookCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			actions++
		}
		switch r.URL.Path {
		case "/v1/apps/A1/webhooks":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"Builds","enabled":true,"eventTypes":["BUILD_UPLOAD_STATE_UPDATED"],"url":"https://hooks.example.test/build"}}]}`)
		case "/v1/webhooks/W1/deliveries":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhookDeliveries","id":"D1","attributes":{"deliveryState":"FAILED","errorMessage":"poison-secret","request":{"url":"https://user:poison-secret@hooks.example.test/private?token=poison-secret"},"response":{"httpStatusCode":500,"body":"poison-secret"}}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	cmd := newWebhooksCommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runWebhookDeliveriesList(cmd, fixtureASCClient(t, srv), "com.example.app", "W1", "json"); err != nil {
		t.Fatal(err)
	}
	if actions != 0 || strings.Contains(output.String(), "poison-secret") || strings.Contains(output.String(), "private") {
		t.Fatalf("actions=%d output=%s", actions, output.String())
	}
}

func TestF7C_WebhookRedeliveryRejectsForeignDeliveryBeforePost(t *testing.T) {
	actions := 0
	srv := webhookCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			actions++
		}
		switch r.URL.Path {
		case "/v1/apps/A1/webhooks":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhooks","id":"W1","attributes":{"name":"Builds","enabled":true,"eventTypes":[],"url":"https://hooks.example.test"}}]}`)
		case "/v1/webhooks/W1/deliveries":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"webhookDeliveries","id":"D1","attributes":{"deliveryState":"FAILED"}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	cmd := newWebhooksCommand()
	cmd.SetContext(context.Background())
	err := runWebhookRedelivery(cmd, fixtureASCClient(t, srv), "com.example.app", "W1", "FOREIGN", true, "json")
	if err == nil || actions != 0 {
		t.Fatalf("err=%v actions=%d", err, actions)
	}
}

func TestF7C_WebhookPingRequiresConfirmationBeforeNetwork(t *testing.T) {
	actions := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { actions++ }))
	defer srv.Close()
	cmd := newWebhooksCommand()
	cmd.SetContext(context.Background())
	err := runWebhookPing(cmd, fixtureASCClient(t, srv), "com.example.app", "W1", false, "json")
	if err == nil || actions != 0 {
		t.Fatalf("err=%v actions=%d", err, actions)
	}
}
