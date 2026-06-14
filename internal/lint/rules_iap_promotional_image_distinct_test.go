package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// promoImageServer builds the multi-endpoint fixture; when iapHash == appHash the rule fires (Guideline 2.3.2 reuse).
func promoImageServer(t *testing.T, iapHash, appHash string) *httptest.Server {
	t.Helper()
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
			body := `{"data":{"id":"rs-1","type":"reviewScreenshots","attributes":{"sourceFileChecksum":"` + iapHash + `","fileName":"shot.png","imageAsset":{"templateUrl":"https://example/{w}x{h}{f}/x.png"}}}}`
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"v-1","type":"appStoreVersions","attributes":{}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersionLocalizations"):
			_, _ = w.Write([]byte(`{"data":[{"id":"loc-1","type":"appStoreVersionLocalizations","attributes":{"locale":"en-US"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appScreenshotSets"):
			_, _ = w.Write([]byte(`{"data":[{"id":"set-1","type":"appScreenshotSets","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appScreenshots"):
			body := `{"data":[{"id":"sc-1","type":"appScreenshots","attributes":{"sourceFileChecksum":"` + appHash + `","fileName":"shot.png"}}]}`
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIAPPromotionalImageDistinct_FiresOnReusedHash(t *testing.T) {
	srv := promoImageServer(t, "abc123same", "abc123same")
	c := newTestClient(t, srv)
	got := iapPromotionalImageDistinctRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Live: true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "iap.promotional-image-distinct" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if got[0].Reference != "Apple Guideline 2.3.2" {
		t.Errorf("reference = %q, want Apple Guideline 2.3.2", got[0].Reference)
	}
}

func TestIAPPromotionalImageDistinct_NoOpOnDistinctHashes(t *testing.T) {
	srv := promoImageServer(t, "iap-only-hash", "app-only-hash")
	c := newTestClient(t, srv)
	got := iapPromotionalImageDistinctRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Live: true,
	})
	if len(got) != 0 {
		t.Errorf("got %d diagnostics, want 0 when hashes differ: %+v", len(got), got)
	}
}
