package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF8C_ExperimentCreatePayloadBindsAppOnly(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/appStoreVersionExperiments" || r.Method != http.MethodPost {
			t.Errorf("request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		posts++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		payload, _ := json.Marshal(body)
		if !strings.Contains(string(payload), `"app":{"data":{"id":"A1","type":"apps"}}`) || strings.Contains(string(payload), `"appStoreVersion":{"data"`) {
			t.Errorf("payload=%s", payload)
		}
		_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionExperiments","id":"E1","attributes":{"name":"Spring","platform":"IOS","trafficProportion":30,"state":"PREPARE_FOR_SUBMISSION"},"relationships":{"app":{"data":{"type":"apps","id":"A1"}}}}}`))
	}))
	defer srv.Close()
	got, err := CreateProductExperiment(context.Background(), newTestClient(t, srv), "A1", "Spring", "IOS", 30)
	if err != nil || got.ID != "E1" || posts != 1 {
		t.Fatalf("got=%+v err=%v posts=%d", got, err, posts)
	}
}

func TestF8C_ExperimentProposalRequiresVersionAndReviewReady(t *testing.T) {
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		switch r.URL.Path {
		case "/v1/appStoreVersions/V1/appStoreVersionExperimentsV2":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersionExperiments","id":"E1","attributes":{"name":"Spring","platform":"IOS","state":"READY_FOR_REVIEW"},"relationships":{"app":{"data":{"type":"apps","id":"A1"}}}}],"links":{}}`))
		case "/v2/appStoreVersionExperiments/E1":
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionExperiments","id":"E1","attributes":{"name":"Spring","platform":"IOS","state":"READY_FOR_REVIEW"},"relationships":{"app":{"data":{"type":"apps","id":"A1"}}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	proposal, err := VerifyExperimentSubmissionProposal(context.Background(), newTestClient(t, srv), "A1", "V1", "E1")
	if err != nil || proposal.Relationship != "appStoreVersionExperimentV2" || gets != 2 {
		t.Fatalf("proposal=%+v err=%v gets=%d", proposal, err, gets)
	}
	_, err = VerifyExperimentSubmissionProposal(context.Background(), newTestClient(t, srv), "A1", "V1", "FOREIGN")
	if err == nil || gets != 3 {
		t.Fatalf("foreign err=%v gets=%d", err, gets)
	}
}

func TestF8C_StoppedExperimentCannotRestartWithoutPatch(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
			return
		}
		_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionExperiments","id":"E1","attributes":{"name":"Spring","platform":"IOS","state":"STOPPED","startDate":"2026-01-01T00:00:00Z","endDate":"2026-02-01T00:00:00Z"},"relationships":{"app":{"data":{"type":"apps","id":"A1"}}}}}`))
	}))
	defer srv.Close()
	_, _, err := SetProductExperimentStarted(context.Background(), newTestClient(t, srv), "A1", "E1", true)
	if err == nil || patches != 0 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}

func TestF8C_ExperimentEditabilityRejectsUnknownAndApproved(t *testing.T) {
	for _, state := range []string{"", "UNKNOWN", "APPROVED", "STOPPED"} {
		if CanEditProductExperiment(ProductExperiment{State: state}) {
			t.Fatalf("editable state %q", state)
		}
	}
	if !CanEditProductExperiment(ProductExperiment{State: "PREPARE_FOR_SUBMISSION"}) {
		t.Fatal("draft rejected")
	}
	if CanEditProductExperiment(ProductExperiment{State: "PREPARE_FOR_SUBMISSION", StartDate: "2026-01-01T00:00:00Z"}) {
		t.Fatal("started draft accepted")
	}
}
