package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF5A_PromotionTargetRejectsIAPIdentityMismatchBeforeWrite(t *testing.T) {
	writes := 0
	srv := promotionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		if r.URL.Path == "/v1/apps/A1/inAppPurchasesV2" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchases","id":"I1","attributes":{"productId":"wrong.product"}}]}`)
			return
		}
		http.NotFound(w, r)
	})
	defer srv.Close()
	cmd := newIAPPromotionCommand()
	cmd.SetContext(context.Background())
	err := runPromotedSet(cmd, fixtureASCClient(t, srv), "com.example.app", "wanted.product", true, true, nil, true, "json")
	if err == nil || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestF5A_PromotionImageUploadNoopsOnProcessedChecksum(t *testing.T) {
	file := filepath.Join(t.TempDir(), "promo.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	checksum, err := fileMD5Hex(file)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	srv := promotionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		switch r.URL.Path {
		case "/v1/apps/A1/inAppPurchasesV2":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchases","id":"I1","attributes":{"productId":"prod"}}]}`)
		case "/v2/inAppPurchases/I1/images":
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"inAppPurchaseImages","id":"IM1","attributes":{"fileName":"promo.png","sourceFileChecksum":%q,"state":"APPROVED"}}]}`, checksum)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	cmd := newIAPPromotionCommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runPromotionImageUpload(cmd, fixtureASCClient(t, srv), "com.example.app", "prod", file, true, false, asc.AssetPollOptions{MaxAttempts: 1}, "json"); err != nil {
		t.Fatal(err)
	}
	var result IAPPromotionImageResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.NoOp || result.Image.ID != "IM1" || writes != 0 {
		t.Fatalf("result=%+v err=%v writes=%d", result, err, writes)
	}
}

func TestF5A_PromotionImagePendingBlocksNewReservation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "promo.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	srv := promotionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		switch r.URL.Path {
		case "/v1/apps/A1/inAppPurchasesV2":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchases","id":"I1","attributes":{"productId":"prod"}}]}`)
		case "/v2/inAppPurchases/I1/images":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchaseImages","id":"IM1","attributes":{"fileName":"promo.png","state":"UPLOAD_COMPLETE"}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	cmd := newIAPPromotionCommand()
	cmd.SetContext(context.Background())
	err := runPromotionImageUpload(cmd, fixtureASCClient(t, srv), "com.example.app", "prod", file, true, false, asc.AssetPollOptions{MaxAttempts: 1}, "json")
	if err == nil || !strings.Contains(err.Error(), "IM1") || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestF5A_PromotionImagePartialReserveDoesNotBlindlyRetry(t *testing.T) {
	file := filepath.Join(t.TempDir(), "promo.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := promotionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/A1/inAppPurchasesV2":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchases","id":"I1","attributes":{"productId":"prod"}}]}`)
		case "/v2/inAppPurchases/I1/images":
			if posts == 0 {
				_, _ = fmt.Fprint(w, `{"data":[]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"data":[{"type":"inAppPurchaseImages","id":"IM1","attributes":{"fileName":"promo.png","state":"AWAITING_UPLOAD"}}]}`)
		case "/v1/inAppPurchaseImages":
			posts++
			_, _ = fmt.Fprint(w, `{"data":{"type":"inAppPurchaseImages","id":"IM1","attributes":{"uploadOperations":[]}}}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	cmd := newIAPPromotionCommand()
	cmd.SetContext(context.Background())
	c := fixtureASCClient(t, srv)
	for i := range 2 {
		if err := runPromotionImageUpload(cmd, c, "com.example.app", "prod", file, true, false, asc.AssetPollOptions{MaxAttempts: 1}, "json"); err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded", i)
		}
	}
	if posts != 1 {
		t.Fatalf("reservation posts=%d, want 1", posts)
	}
}

func TestF5A_PromotionImageInvalidPollStopsBeforeNetwork(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer srv.Close()
	cmd := newIAPPromotionCommand()
	cmd.SetContext(context.Background())
	err := runPromotionImageUpload(cmd, fixtureASCClient(t, srv), "com.example.app", "prod", "missing.png", true, false, asc.AssetPollOptions{}, "json")
	if err == nil || requests != 0 {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
}

func TestF5A_PromotedOrderRequiresCompleteSetIncludingSubscription(t *testing.T) {
	writes := 0
	srv := promotionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		if r.URL.Path == "/v1/apps/A1/promotedPurchases" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"promotedPurchases","id":"P1","relationships":{"inAppPurchaseV2":{"data":{"type":"inAppPurchases","id":"I1"}}}},{"type":"promotedPurchases","id":"P2","relationships":{"subscription":{"data":{"type":"subscriptions","id":"S1"}}}}]}`)
			return
		}
		if r.URL.Path == "/v1/apps/A1/relationships/promotedPurchases" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"promotedPurchases","id":"P1"},{"type":"promotedPurchases","id":"P2"}]}`)
			return
		}
		http.NotFound(w, r)
	})
	defer srv.Close()
	cmd := newIAPPromotionCommand()
	cmd.SetContext(context.Background())
	err := runPromotedOrder(cmd, fixtureASCClient(t, srv), "com.example.app", []string{"P1"}, true, "json")
	if err == nil || !strings.Contains(err.Error(), "including subscriptions") || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func promotionFixture(t *testing.T, next http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1":
			_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}}`)
		default:
			next(w, r)
		}
	}))
}
