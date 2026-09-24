package asc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF8A_SubmissionItemsRequireOneExactRelationship(t *testing.T) {
	for _, body := range []string{
		`{"data":[{"type":"reviewSubmissionItems","id":"I1","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{}}]}`,
		`{"data":[{"type":"reviewSubmissionItems","id":"I1","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"appEvent":{"data":{"type":"appEvents","id":"E1"}},"appStoreVersion":{"data":{"type":"appStoreVersions","id":"V1"}}}}]}`,
		`{"data":[{"type":"reviewSubmissionItems","id":"I1","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"backgroundAssetVersion":{"data":{"type":"backgroundAssetVersions","id":"B1"}}}}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		_, err := ListSubmissionAssemblyItems(context.Background(), fixtureClient(t, srv), "S1")
		srv.Close()
		if err == nil {
			t.Fatalf("body accepted: %s", body)
		}
	}
}

func TestF8A_SubmitRequiresProgressedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"reviewSubmissions","id":"S1","attributes":{"state":"READY_FOR_REVIEW"}}}`))
	}))
	defer srv.Close()
	if _, err := SubmitSubmissionAssembly(context.Background(), fixtureClient(t, srv), "S1"); err == nil {
		t.Fatal("READY_FOR_REVIEW response accepted as submitted")
	}
}

func TestF8A_AddSubmissionItemUsesExactRelationship(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/reviewSubmissionItems" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"reviewSubmissionItems","id":"I1","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"inAppPurchaseVersion":{"data":{"type":"inAppPurchaseVersions","id":"P1"}}}}}`))
	}))
	defer srv.Close()
	got, err := AddSubmissionAssemblyItem(context.Background(), fixtureClient(t, srv), "S1", SubmissionItemProposal{Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: "P1"})
	if err != nil || got.Proposal.ID != "P1" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
