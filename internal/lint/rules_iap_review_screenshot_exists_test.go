package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIAPReviewScreenshotExists_FiresWhenMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[
				{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"com.example.x.lifetime","state":"READY_TO_SUBMIT"}}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreReviewScreenshot"):
			_, _ = w.Write([]byte(`{"data":{"id":"rs-1","type":"reviewScreenshots","attributes":{}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	got := iapReviewScreenshotExistsRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Live: true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "iap.review-screenshot-exists" || got[0].Severity != SeverityError {
		t.Errorf("rule/sev = %s/%v, want iap.review-screenshot-exists/error", got[0].RuleID, got[0].Severity)
	}
}

func TestIAPReviewScreenshotExists_NoOpWhenPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[
				{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"com.example.x.lifetime","state":"READY_TO_SUBMIT"}}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreReviewScreenshot"):
			_, _ = w.Write([]byte(`{"data":{"id":"rs-1","type":"reviewScreenshots","attributes":{"fileName":"shot.png","imageAsset":{"templateUrl":"https://example/{w}x{h}{f}/shot.png","width":1200,"height":1600}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	got := iapReviewScreenshotExistsRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Live: true,
	})
	if len(got) != 0 {
		t.Errorf("got %d diagnostics, want 0 when screenshot present: %+v", len(got), got)
	}
}
