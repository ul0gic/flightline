package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestF5A_PromotionImagesPageAndProcessing(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/images":
			if r.URL.Query().Get("cursor") == "next" {
				_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchaseImages","id":"IM2","attributes":{"fileName":"second.png","state":"APPROVED"}}]}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"inAppPurchaseImages","id":"IM1","attributes":{"fileName":"first.png","state":"UPLOAD_COMPLETE"}}],"links":{"next":%q}}`, srv.URL+"/v2/inAppPurchases/I1/images?cursor=next")
		case "/v1/inAppPurchaseImages/IM1":
			_, _ = fmt.Fprint(w, `{"data":{"type":"inAppPurchaseImages","id":"IM1","attributes":{"fileName":"first.png","state":"PREPARE_FOR_SUBMISSION"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	images, err := ListIAPPromotionalImages(context.Background(), c, "I1")
	if err != nil || len(images) != 2 || images[1].ID != "IM2" {
		t.Fatalf("images=%+v err=%v", images, err)
	}
	image, err := WaitIAPPromotionalImage(context.Background(), c, "IM1", AssetPollOptions{MaxAttempts: 2, Interval: time.Millisecond})
	if err != nil || image.State != "PREPARE_FOR_SUBMISSION" {
		t.Fatalf("image=%+v err=%v", image, err)
	}
}

func TestF5A_PromotionProcessingPendingRetainsID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"type":"inAppPurchaseImages","id":"IM1","attributes":{"state":"UPLOAD_COMPLETE"}}}`)
	}))
	defer srv.Close()
	_, err := WaitIAPPromotionalImage(context.Background(), fixtureClient(t, srv), "IM1", AssetPollOptions{MaxAttempts: 1})
	var pending *AssetProcessingPendingError
	if err == nil || !strings.Contains(err.Error(), "IM1") || !errors.As(err, &pending) || pending.ID != "IM1" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestF5A_PromotedPurchaseListAndOrderPayload(t *testing.T) {
	var order []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/A1/promotedPurchases":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"promotedPurchases","id":"P1","attributes":{"visibleForAllUsers":false},"relationships":{"inAppPurchaseV2":{"data":{"type":"inAppPurchases","id":"I1"}}}},{"type":"promotedPurchases","id":"P2","attributes":{"enabled":true},"relationships":{"subscription":{"data":{"type":"subscriptions","id":"S1"}}}}]}`)
		case "/v1/apps/A1/relationships/promotedPurchases":
			if r.Method == http.MethodGet {
				_, _ = fmt.Fprint(w, `{"data":[{"type":"promotedPurchases","id":"P1"},{"type":"promotedPurchases","id":"P2"}]}`)
				return
			}
			var payload struct {
				Data []map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			order = payload.Data
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	rows, err := ListAppPromotedPurchases(context.Background(), c, "A1")
	if err != nil || len(rows) != 2 || rows[0].IAPID != "I1" || rows[1].SubscriptionID != "S1" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err := OrderAppPromotedPurchases(context.Background(), c, "A1", []string{"P2", "P1"}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0]["id"] != "P2" || order[1]["id"] != "P1" {
		t.Fatalf("order=%v", order)
	}
}

func TestF5A_PromotedPurchaseCreatePatchPayload(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"type":"promotedPurchases","id":"P1"}}`)
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if _, err := CreateIAPPromotedPurchase(context.Background(), c, "A1", "I1", false, nil); err != nil {
		t.Fatal(err)
	}
	visible := true
	if err := PatchPromotedPurchase(context.Background(), c, "P1", &visible, nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies=%+v", bodies)
	}
	create, _ := bodies[0]["data"].(map[string]any)
	rels, _ := create["relationships"].(map[string]any)
	iap, _ := rels["inAppPurchaseV2"].(map[string]any)
	ref, _ := iap["data"].(map[string]any)
	if ref["type"] != "inAppPurchases" || ref["id"] != "I1" {
		t.Fatalf("create body=%v", bodies[0])
	}
	patch, _ := bodies[1]["data"].(map[string]any)
	attrs, _ := patch["attributes"].(map[string]any)
	if patch["id"] != "P1" || attrs["visibleForAllUsers"] != true || len(attrs) != 1 {
		t.Fatalf("patch body=%v", bodies[1])
	}
}
