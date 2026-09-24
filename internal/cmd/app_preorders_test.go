package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5B_EndAppPreordersRequiresActivePreOrderBeforePost(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1"}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[{"type":"territoryAvailabilities","id":"TA-USA","attributes":{"preOrderEnabled":false},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
		case "/v1/endAppAvailabilityPreOrders":
			posts++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	command := newAppPreordersCommand()
	command.SetContext(context.Background())
	err := runEndAppPreordersWithClient(command, []string{"com.example.app"}, fixtureASCClient(t, srv), []string{"USA"}, true, "json")
	if err == nil || !strings.Contains(err.Error(), "not an observed active pre-order") || posts != 0 {
		t.Fatalf("err = %v, posts = %d", err, posts)
	}
}

func TestF5B_EndAppPreordersConfirmedActionUsesFreshTerritoryIDs(t *testing.T) {
	var postBody struct {
		Data struct {
			Relationships struct {
				TerritoryAvailabilities struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"territoryAvailabilities"`
			} `json:"relationships"`
		} `json:"data"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1"}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[{"type":"territoryAvailabilities","id":"TA-GBR","attributes":{"preOrderEnabled":true},"relationships":{"territory":{"data":{"type":"territories","id":"GBR"}}}}],"links":{}}`))
		case "/v1/endAppAvailabilityPreOrders":
			if err := json.NewDecoder(r.Body).Decode(&postBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"endAppAvailabilityPreOrders","id":"END-1"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	command := newAppPreordersCommand()
	command.SetContext(context.Background())
	var output bytes.Buffer
	command.SetOut(&output)
	if err := runEndAppPreordersWithClient(command, []string{"com.example.app"}, fixtureASCClient(t, srv), []string{"GBR"}, true, "json"); err != nil {
		t.Fatalf("runEndAppPreordersWithClient: %v", err)
	}
	if !strings.Contains(output.String(), `"requestId": "END-1"`) || !strings.Contains(output.String(), `"territories": [`) {
		t.Fatalf("output = %s", output.String())
	}
	resources := postBody.Data.Relationships.TerritoryAvailabilities.Data
	if len(resources) != 1 || resources[0].ID != "TA-GBR" {
		t.Fatalf("post body = %#v", postBody)
	}
}

func TestF5B_EndAppPreordersRequiresConfirmAndConstructorWarns(t *testing.T) {
	command := newAppPreordersCommand()
	command.SetContext(context.Background())
	if err := runEndAppPreordersWithClient(command, []string{"com.example.app"}, nil, []string{"GBR"}, false, "json"); err == nil || !strings.Contains(err.Error(), "release immediately") {
		t.Fatalf("confirmation error = %v", err)
	}
	end, _, err := command.Find([]string{"end"})
	if err != nil || !strings.Contains(end.Long, "release the app immediately") {
		t.Fatalf("end command = %#v, err = %v", end, err)
	}
}
