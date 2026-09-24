package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF7B_ReadResponseVerifiesCompleteAppMembership(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps/APP1/customerReviews" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/customerReviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R0"}],"links":{"next":"` + serverURL + `/v1/apps/APP1/customerReviews?page=2"}}`))
		case r.URL.Path == "/v1/customerReviews/R1/response":
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1","attributes":{"responseBody":"Thank you","state":"PUBLISHED"},"relationships":{"review":{"data":{"type":"customerReviews","id":"R1"}}}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	response, err := ReadCustomerReviewResponse(context.Background(), fixtureClient(t, srv), "APP1", "R1")
	if err != nil || response == nil || response.ID != "RESP1" || response.ResponseBody != "Thank you" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestF7B_ForeignReviewBlocksAllResponseRequests(t *testing.T) {
	responseCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/APP1/customerReviews" {
			responseCalls++
			t.Errorf("unexpected response request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"OTHER"}],"links":{}}`))
	}))
	defer srv.Close()
	_, _, err := CreateCustomerReviewResponse(context.Background(), fixtureClient(t, srv), "APP1", "FOREIGN", "Reply")
	if err == nil || responseCalls != 0 {
		t.Fatalf("err=%v responseCalls=%d", err, responseCalls)
	}
}

func TestF7B_ExistingExactBodyNoOpDifferentBodyFails(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts++
			t.Error("unexpected POST")
			return
		}
		switch r.URL.Path {
		case "/v1/apps/APP1/customerReviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
		case "/v1/customerReviews/R1/response":
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1","attributes":{"responseBody":"Exact\nbody","state":"PENDING_PUBLISH"}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	response, changed, err := CreateCustomerReviewResponse(context.Background(), c, "APP1", "R1", "Exact\nbody")
	if err != nil || changed || response.ID != "RESP1" {
		t.Fatalf("response=%+v changed=%v err=%v", response, changed, err)
	}
	_, _, err = CreateCustomerReviewResponse(context.Background(), c, "APP1", "R1", "Different")
	if err == nil || !strings.Contains(err.Error(), "delete it explicitly") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF7B_CreatePreservesExactBodyAndConfirmsRelatedResponse(t *testing.T) {
	body := "  Thanks for the report.\nWe will review it.\n"
	posts, relatedReads := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps/APP1/customerReviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
		case r.URL.Path == "/v1/customerReviews/R1/response":
			relatedReads++
			if posts == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1","attributes":{"responseBody":"  Thanks for the report.\nWe will review it.\n","state":"PENDING_PUBLISH"}}}`))
		case r.URL.Path == "/v1/customerReviewResponses" && r.Method == http.MethodPost:
			posts++
			var request struct {
				Data struct {
					Type       string `json:"type"`
					Attributes struct {
						ResponseBody string `json:"responseBody"`
					} `json:"attributes"`
					Relationships map[string]struct {
						Data struct{ Type, ID string } `json:"data"`
					} `json:"relationships"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Data.Type != "customerReviewResponses" || request.Data.Attributes.ResponseBody != body || request.Data.Relationships["review"].Data.ID != "R1" {
				t.Errorf("request=%+v", request)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	response, changed, err := CreateCustomerReviewResponse(context.Background(), fixtureClient(t, srv), "APP1", "R1", body)
	if err != nil || !changed || response.ResponseBody != body || posts != 1 || relatedReads != 2 {
		t.Fatalf("response=%+v changed=%v err=%v posts=%d reads=%d", response, changed, err, posts, relatedReads)
	}
}

func TestF7B_PostFailureRefetchesAndDoesNotRetry(t *testing.T) {
	posts, reads := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps/APP1/customerReviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
		case r.URL.Path == "/v1/customerReviews/R1/response":
			reads++
			if posts == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1","attributes":{"responseBody":"Reply","state":"PENDING_PUBLISH"}}}`))
		case r.URL.Path == "/v1/customerReviewResponses" && r.Method == http.MethodPost:
			posts++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"status":"500","title":"uncertain"}]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, _, err := CreateCustomerReviewResponse(context.Background(), fixtureClient(t, srv), "APP1", "R1", "Reply")
	if err == nil || !strings.Contains(err.Error(), "RESP1 is now present") || posts != 1 || reads != 2 {
		t.Fatalf("err=%v posts=%d reads=%d", err, posts, reads)
	}
}

func TestF7B_DeleteRequiresExactCurrentResponseID(t *testing.T) {
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.URL.Path {
		case "/v1/apps/APP1/customerReviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
		case "/v1/customerReviews/R1/response":
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1","attributes":{"responseBody":"Reply","state":"PUBLISHED"}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if _, err := DeleteCustomerReviewResponse(context.Background(), c, "APP1", "R1", "OTHER"); err == nil || deletes != 0 {
		t.Fatalf("mismatch err=%v deletes=%d", err, deletes)
	}
	changed, err := DeleteCustomerReviewResponse(context.Background(), c, "APP1", "R1", "RESP1")
	if err != nil || !changed || deletes != 1 {
		t.Fatalf("changed=%v err=%v deletes=%d", changed, err, deletes)
	}
}

func TestF7B_RelatedResponseForeignRelationshipFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP1/customerReviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
		case "/v1/customerReviews/R1/response":
			_, _ = w.Write([]byte(`{"data":{"type":"customerReviewResponses","id":"RESP1","attributes":{"responseBody":"Reply","state":"PUBLISHED"},"relationships":{"review":{"data":{"type":"customerReviews","id":"FOREIGN"}}}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := ReadCustomerReviewResponse(context.Background(), fixtureClient(t, srv), "APP1", "R1")
	if err == nil || !strings.Contains(err.Error(), "different review") {
		t.Fatalf("err=%v", err)
	}
}

func TestF7B_RelatedResponseForbiddenDoesNotLookAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/apps/APP1/customerReviews" {
			_, _ = w.Write([]byte(`{"data":[{"type":"customerReviews","id":"R1"}],"links":{}}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"status":"403","title":"forbidden"}]}`))
	}))
	defer srv.Close()
	_, _, err := CreateCustomerReviewResponse(context.Background(), fixtureClient(t, srv), "APP1", "R1", "Reply")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err=%v", err)
	}
}
