package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/state"
)

func TestG8_AssemblyRuntimeRejectsForeignIAPBeforeWrites(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/A/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"V","attributes":{"platform":"IOS","versionString":"1.0","appVersionState":"PREPARE_FOR_SUBMISSION"}}]}`))
		case "/v1/inAppPurchaseVersions/IV":
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchaseVersions","id":"IV","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"inAppPurchase":{"data":{"type":"inAppPurchases","id":"foreign"}}}}}`))
		case "/v1/apps/A/inAppPurchasesV2":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchases","id":"owned"}]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	plan, err := state.BuildSubmissionPlan("A", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V"}, []asc.SubmissionItemProposal{{Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: "IV"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.ApplySubmissionPlan(context.Background(), fixtureASCClient(t, srv), plan, verifySubmissionItem, preflightSubmission)
	if err == nil || !strings.Contains(err.Error(), "0 matches") || writes.Load() != 0 {
		t.Fatalf("err=%v writes=%d", err, writes.Load())
	}
}

func TestG8_FinalSubmitCannotSkipProductionPreflight(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S","attributes":{"platform":"IOS","state":"READY_FOR_REVIEW"}}]}`))
		case "/v1/apps/A/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"V","attributes":{"platform":"IOS","versionString":"1.0","appVersionState":"PREPARE_FOR_SUBMISSION"}}]}`))
		case "/v1/reviewSubmissions/S/items":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissionItems","id":"I","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"appStoreVersion":{"data":{"type":"appStoreVersions","id":"V"}}}}]}`))
		case "/v1/apps/A":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	plan, err := state.BuildSubmissionPlan("A", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = state.SubmitSubmissionPlan(context.Background(), fixtureASCClient(t, srv), plan, "S", verifySubmissionItem, preflightSubmission)
	if err == nil || !strings.Contains(err.Error(), "fresh preflight") || writes.Load() != 0 {
		t.Fatalf("err=%v writes=%d", err, writes.Load())
	}
}

func TestG8_ReviewSubmissionItemsRejectForeignTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/reviewSubmissions" {
			t.Errorf("foreign items endpoint requested: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"owned","attributes":{"state":"IN_REVIEW"}}]}`))
	}))
	defer srv.Close()
	if err := requireReviewSubmissionMembership(context.Background(), fixtureASCClient(t, srv), "A", "foreign"); err == nil {
		t.Fatal("foreign submission accepted")
	}
}
