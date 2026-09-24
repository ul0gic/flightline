package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF8A_AssemblyOrdersVersionIAPAndReplayIsWriteFree(t *testing.T) {
	var attached []asc.SubmissionItemProposal
	var posts []string
	preflights := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S1","attributes":{"state":"READY_FOR_REVIEW","platform":"IOS"}}]}`))
		case "/v1/reviewSubmissions/S1/items":
			_, _ = w.Write([]byte(submissionItemsJSON(attached)))
		case "/v1/reviewSubmissionItems":
			var body struct {
				Data struct {
					Relationships map[string]struct {
						Data asc.SubmissionItemProposal `json:"data"`
					} `json:"relationships"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			var proposal asc.SubmissionItemProposal
			for relationship, relationshipData := range body.Data.Relationships {
				if relationship != "reviewSubmission" {
					proposal = relationshipData.Data
					proposal.Relationship = relationship
				}
			}
			attached = append(attached, proposal)
			posts = append(posts, proposal.Relationship+"/"+proposal.ID)
			_, _ = w.Write([]byte(submissionItemJSON(proposal, "I"+strconv.Itoa(len(attached)), asc.ReviewSubmissionItemStateReadyForReview)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	plan, err := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, []asc.SubmissionItemProposal{{Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: "P1"}})
	if err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error { return nil }
	preflight := func(context.Context, *asc.Client, SubmissionPlan, string) error { preflights++; return nil }
	client := fixtureClient(t, srv)
	if _, err := ApplySubmissionPlan(context.Background(), client, plan, verify, preflight); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(posts, ","), "appStoreVersion/V1,inAppPurchaseVersion/P1"; got != want {
		t.Fatalf("posts=%q want=%q", got, want)
	}
	if _, err := ApplySubmissionPlan(context.Background(), client, plan, verify, preflight); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || preflights != 2 {
		t.Fatalf("posts=%v preflights=%d", posts, preflights)
	}
}

func TestF8A_UncertainAttachmentProvenByReadDoesNotDuplicate(t *testing.T) {
	attached := false
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S1","attributes":{"state":"READY_FOR_REVIEW","platform":"IOS"}}]}`))
		case "/v1/reviewSubmissions/S1/items":
			if attached {
				_, _ = w.Write([]byte(submissionItemsJSON([]asc.SubmissionItemProposal{{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}})))
			} else {
				_, _ = w.Write([]byte(`{"data":[]}`))
			}
		case "/v1/reviewSubmissionItems":
			posts++
			attached = true
			http.Error(w, "lost response", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	plan, _ := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, nil)
	verify := func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error { return nil }
	preflight := func(context.Context, *asc.Client, SubmissionPlan, string) error { return nil }
	if _, err := ApplySubmissionPlan(context.Background(), fixtureClient(t, srv), plan, verify, preflight); err != nil || posts != 1 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF8A_RejectedRecoveryStopsBeforeWrite(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/reviewSubmissions" {
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S1","attributes":{"state":"UNRESOLVED_ISSUES","platform":"IOS"}}]}`))
			return
		}
		posts++
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	plan, _ := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, nil)
	verify := func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error { return nil }
	preflight := func(context.Context, *asc.Client, SubmissionPlan, string) error { return nil }
	if _, err := ApplySubmissionPlan(context.Background(), fixtureClient(t, srv), plan, verify, preflight); err == nil || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func submissionItemsJSON(items []asc.SubmissionItemProposal) string {
	parts := make([]string, 0, len(items))
	for i, item := range items {
		parts = append(parts, submissionItemJSON(item, fmt.Sprintf("I%d", i+1), asc.ReviewSubmissionItemStateReadyForReview))
	}
	return `{"data":[` + strings.Join(parts, ",") + `]}`
}

func submissionItemJSON(item asc.SubmissionItemProposal, id, state string) string {
	return fmt.Sprintf(`{"type":"reviewSubmissionItems","id":%q,"attributes":{"state":%q},"relationships":{%q:{"data":{"type":%q,"id":%q}}}}`, id, state, item.Relationship, item.Type, item.ID)
}

func TestF8A_AssemblyRejectsForeignExistingItemBeforeWrite(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S1","attributes":{"state":"READY_FOR_REVIEW","platform":"IOS"}}]}`))
		case "/v1/reviewSubmissions/S1/items":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissionItems","id":"X1","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"appEvent":{"data":{"type":"appEvents","id":"foreign"}}}}]}`))
		case "/v1/reviewSubmissionItems":
			posts++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	plan, err := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error { return nil }
	preflight := func(context.Context, *asc.Client, SubmissionPlan, string) error { return nil }
	_, err = ApplySubmissionPlan(context.Background(), fixtureClient(t, srv), plan, verify, preflight)
	if err == nil || !strings.Contains(err.Error(), "outside the exact assembly plan") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF8A_SubmitRequiresVerifierAndPreflight(t *testing.T) {
	plan, err := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SubmitSubmissionPlan(context.Background(), nil, plan, "S1", nil, nil); err == nil || !strings.Contains(err.Error(), "verifier") {
		t.Fatalf("err=%v", err)
	}
}

func TestF8A_SubmitRejectsNonReadyItemBeforePatch(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S1","attributes":{"state":"READY_FOR_REVIEW","platform":"IOS"}}]}`))
		case "/v1/reviewSubmissions/S1/items":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissionItems","id":"I1","attributes":{"state":"REJECTED"},"relationships":{"appStoreVersion":{"data":{"type":"appStoreVersions","id":"V1"}}}}]}`))
		case "/v1/reviewSubmissions/S1":
			patches++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	plan, err := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error { return nil }
	preflight := func(context.Context, *asc.Client, SubmissionPlan, string) error { return nil }
	err = SubmitSubmissionPlan(context.Background(), fixtureClient(t, srv), plan, "S1", verify, preflight)
	if err == nil || !strings.Contains(err.Error(), "not READY_FOR_REVIEW") || patches != 0 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}

func TestF8A_SubmitRechecksMembershipAfterPreflight(t *testing.T) {
	drifted, patches := false, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"type":"reviewSubmissions","id":"S1","attributes":{"state":"READY_FOR_REVIEW","platform":"IOS"}}]}`))
		case "/v1/reviewSubmissions/S1/items":
			items := []asc.SubmissionItemProposal{{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}}
			if drifted {
				items = append(items, asc.SubmissionItemProposal{Relationship: "appEvent", Type: "appEvents", ID: "E1"})
			}
			_, _ = w.Write([]byte(submissionItemsJSON(items)))
		case "/v1/reviewSubmissions/S1":
			patches++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	plan, _ := BuildSubmissionPlan("A1", "IOS", asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}, nil)
	verify := func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error { return nil }
	preflight := func(context.Context, *asc.Client, SubmissionPlan, string) error { drifted = true; return nil }
	err := SubmitSubmissionPlan(context.Background(), fixtureClient(t, srv), plan, "S1", verify, preflight)
	if err == nil || !strings.Contains(err.Error(), "membership differs") || patches != 0 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}
