package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewDetailsCompleteness_OfflineNoop(t *testing.T) {
	var r reviewDetailsCompletenessRule
	if diags := r.Check(CheckContext{Live: false}); diags != nil {
		t.Fatalf("offline check returned %v, want nil", diags)
	}
}

func TestReviewDetailsCompleteness_MissingDetailWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case req.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.iap"}}]}`))
		case strings.HasSuffix(req.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"com.example.iap.lifetime","state":"APPROVED"}}]}`))
		case strings.HasSuffix(req.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ver-1","type":"appStoreVersions","attributes":{"versionString":"1.0","platform":"IOS"}}]}`))
		case strings.HasSuffix(req.URL.Path, "/appStoreReviewDetail"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":"404","code":"NOT_FOUND","title":"not found"}]}`))
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
		}
	}))
	defer srv.Close()

	var r reviewDetailsCompletenessRule
	diags := r.Check(CheckContext{
		Ctx:      context.Background(),
		Client:   newTestClient(t, srv),
		BundleID: "com.example.iap",
		Version:  "1.0",
		Live:     true,
	})
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(diags), diags)
	}
	if diags[0].Severity != SeverityWarning || !strings.Contains(diags[0].Message, "App Review notes are empty") {
		t.Errorf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestIAPStateSubmittable_OfflineNoop(t *testing.T) {
	var r iapStateSubmittableRule
	if diags := r.Check(CheckContext{Live: false}); diags != nil {
		t.Fatalf("offline check returned %v, want nil", diags)
	}
}

func TestPaidAppsAgreement_OfflineNoop(t *testing.T) {
	var r paidAppsAgreementRule
	if diags := r.Check(CheckContext{Live: false}); diags != nil {
		t.Fatalf("offline check returned %v, want nil", diags)
	}
}
