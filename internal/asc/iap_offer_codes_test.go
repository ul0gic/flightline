package asc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestF5A_OfferReadPaginatesAndChecksOwnership(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/inAppPurchases/I1/offerCodes" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodes","id":"O2","attributes":{"name":"Second"}}],"links":{}}`))
		case r.URL.Path == "/v2/inAppPurchases/I1/offerCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodes","id":"O1","attributes":{"name":"First"}}],"links":{"next":"` + serverURL + `/v2/inAppPurchases/I1/offerCodes?page=2"}}`))
		case r.URL.Path == "/v1/inAppPurchaseOfferCodes/O2":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseOfferCodes","id":"O2","attributes":{"name":"Second"}}}`))
		case r.URL.Path == "/v1/inAppPurchaseOfferCodes/O2/prices":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferPrices","id":"PR1","relationships":{"territory":{"data":{"type":"territories","id":"USA"}},"pricePoint":{"data":{"type":"inAppPurchasePricePoints","id":"P1"}}}}],"links":{}}`))
		case r.URL.Path == "/v1/inAppPurchaseOfferCodes/O2/customCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodeCustomCodes","id":"C1","attributes":{"customCode":"private-value","numberOfCodes":10}}],"links":{}}`))
		case r.URL.Path == "/v1/inAppPurchaseOfferCodes/O2/oneTimeUseCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodeOneTimeUseCodes","id":"B1","attributes":{"numberOfCodes":50,"environment":"SANDBOX"}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	c := newTestClient(t, srv)
	detail, err := GetIAPOfferCode(context.Background(), c, "I1", "O2")
	if err != nil || detail.Name != "Second" || len(detail.Prices) != 1 || detail.Prices[0].PricePointID != "P1" || len(detail.CustomBatches) != 1 || len(detail.OneTimeBatches) != 1 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	encoded, err := json.Marshal(detail)
	if err != nil || strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "customCode") {
		t.Fatalf("custom code leaked in JSON detail: %s err=%v", encoded, err)
	}
	if _, err := GetIAPOfferCode(context.Background(), c, "I1", "OTHER"); err == nil {
		t.Fatal("foreign offer ID accepted")
	}
}

func TestF5A_OneTimePOSTFailureRequiresInspectBeforeRetry(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/offerCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodes","id":"O1","attributes":{"name":"Fall"}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodeOneTimeUseCodes":
			posts++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"status":"500","detail":"temporary failure"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer srv.Close()
	_, err := CreateIAPOneTimeCodeBatch(context.Background(), newTestClient(t, srv), "I1", "O1", 10, "2027-01-01", "SANDBOX", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "inspect live batches before retrying") || posts != 1 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF5A_OfferDefinitionPayloadAndDuplicateGuard(t *testing.T) {
	posts := 0
	var createdBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/offerCodes":
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		case "/v2/inAppPurchases/I1/pricePoints":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchasePricePoints","id":"P1","relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodes":
			posts++
			if err := json.NewDecoder(r.Body).Decode(&createdBody); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseOfferCodes","id":"O1","attributes":{"name":"Fall","customerEligibilities":["NON_SPENDER"]}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	created, err := CreateIAPOfferCode(context.Background(), c, "I1", "Fall", []string{"NON_SPENDER"}, []IAPOfferPriceChoice{{TerritoryID: "USA", PricePointID: "P1"}})
	if err != nil || created.ID != "O1" || posts != 1 {
		t.Fatalf("created=%+v err=%v posts=%d", created, err, posts)
	}
	buf, _ := json.Marshal(createdBody)
	if !strings.Contains(string(buf), `"type":"inAppPurchaseOfferPrices"`) || !strings.Contains(string(buf), `"id":"P1"`) || !strings.Contains(string(buf), `"id":"I1"`) {
		t.Fatalf("payload=%s", buf)
	}
}

func TestF5A_CustomCodeErrorRedactsValue(t *testing.T) {
	secret := "SUPER-SECRET-CODE"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/offerCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodes","id":"O1","attributes":{"name":"Fall"}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodes/O1/customCodes":
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodeCustomCodes":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"status":"400","detail":"rejected SUPER-SECRET-CODE"}]}`))
		default:
			t.Errorf("request=%s", r.URL)
		}
	}))
	defer srv.Close()
	_, err := CreateIAPCustomCodeBatch(context.Background(), newTestClient(t, srv), "I1", "O1", secret, 10, "", time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked code: %v", err)
	}
}

func TestF5A_OneTimeDownloadChecksBatchAndStreamsCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/offerCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodes","id":"O1","attributes":{"name":"Fall"}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodes/O1/oneTimeUseCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodeOneTimeUseCodes","id":"B1","attributes":{"numberOfCodes":1,"environment":"SANDBOX"}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodeOneTimeUseCodes/B1/values":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("code\nprivate-value\n"))
		default:
			t.Errorf("request=%s", r.URL)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	var buf bytes.Buffer
	if _, err := DownloadIAPOneTimeCodes(context.Background(), c, "I1", "O1", "OTHER", &buf); err == nil || buf.Len() != 0 {
		t.Fatal("foreign batch accepted")
	}
	count, err := DownloadIAPOneTimeCodes(context.Background(), c, "I1", "O1", "B1", &buf)
	if err != nil || count == 0 || !strings.Contains(buf.String(), "private-value") {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
