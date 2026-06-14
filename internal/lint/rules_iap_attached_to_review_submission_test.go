package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIAPAttachedToReviewSubmission_OfflineNoOp(t *testing.T) {
	r := iapAttachedToReviewSubmissionRule{}
	got := r.Check(CheckContext{Ctx: context.Background(), Live: false})
	if len(got) != 0 {
		t.Errorf("offline run returned %d diagnostics, want 0 (rule is live-only)", len(got))
	}
}

// TestIAPAttachedToReviewSubmission_FiresWhenIAPNotInSubmission verifies the rule flags a READY_TO_SUBMIT IAP
// when the submission's items reference a different IAP.
func TestIAPAttachedToReviewSubmission_FiresWhenIAPNotInSubmission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[
				{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"com.example.x.lifetime","state":"READY_TO_SUBMIT"}}
			]}`))
		case r.URL.Path == "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"id":"sub-1","type":"reviewSubmissions","attributes":{"state":"READY_FOR_REVIEW"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/items"):
			_, _ = w.Write([]byte(`{"data":[{"id":"item-1","type":"reviewSubmissionItems","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"appStoreVersion":{"data":{"type":"appStoreVersions","id":"v-1"}}}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	rule := iapAttachedToReviewSubmissionRule{}
	got := rule.Check(CheckContext{
		Ctx:      context.Background(),
		Client:   c,
		BundleID: "com.example.x",
		Live:     true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "iap.attached-to-review-submission" {
		t.Errorf("rule id = %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "com.example.x.lifetime") {
		t.Errorf("message missing productId: %q", got[0].Message)
	}
}

func TestIAPAttachedToReviewSubmission_NoOpWhenAttached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[
				{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"com.example.x.lifetime","state":"READY_TO_SUBMIT"}}
			]}`))
		case r.URL.Path == "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"id":"sub-1","type":"reviewSubmissions","attributes":{"state":"WAITING_FOR_REVIEW"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/items"):
			// item references the IAP itself
			_, _ = w.Write([]byte(`{"data":[{"id":"item-1","type":"reviewSubmissionItems","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"inAppPurchaseV2":{"data":{"type":"inAppPurchaseV2","id":"iap-A"}}}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	rule := iapAttachedToReviewSubmissionRule{}
	got := rule.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Live: true,
	})
	if len(got) != 0 {
		t.Errorf("got %d diagnostics, want 0 when IAP is attached: %+v", len(got), got)
	}
}
