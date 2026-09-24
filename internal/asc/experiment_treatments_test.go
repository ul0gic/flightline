package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF8C_TreatmentAndLocalizationCreateUseV2Ownership(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		payload, _ := json.Marshal(body)
		switch r.URL.Path {
		case "/v1/appStoreVersionExperimentTreatments":
			if !strings.Contains(string(payload), `"appStoreVersionExperimentV2":{"data":{"id":"E1","type":"appStoreVersionExperiments"}}`) || strings.Contains(string(payload), `"appStoreVersionExperiment":`) {
				t.Errorf("treatment body=%s", payload)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionExperimentTreatments","id":"T1","attributes":{"name":"Blue"},"relationships":{"appStoreVersionExperimentV2":{"data":{"type":"appStoreVersionExperiments","id":"E1"}}}}}`))
		case "/v1/appStoreVersionExperimentTreatmentLocalizations":
			if !strings.Contains(string(payload), `"appStoreVersionExperimentTreatment":{"data":{"id":"T1","type":"appStoreVersionExperimentTreatments"}}`) {
				t.Errorf("localization body=%s", payload)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionExperimentTreatmentLocalizations","id":"L1","attributes":{"locale":"en-US"},"relationships":{"appStoreVersionExperimentTreatment":{"data":{"type":"appStoreVersionExperimentTreatments","id":"T1"}}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	treatment, err := CreateExperimentTreatment(context.Background(), c, "E1", "Blue", "")
	if err != nil || treatment.ID != "T1" {
		t.Fatalf("treatment=%+v err=%v", treatment, err)
	}
	loc, err := CreateExperimentLocalization(context.Background(), c, "T1", "en-US")
	if err != nil || loc.ID != "L1" || writes != 2 {
		t.Fatalf("loc=%+v err=%v writes=%d", loc, err, writes)
	}
}

func TestF8C_TreatmentForeignExperimentRejectedBeforeDelete(t *testing.T) {
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			return
		}
		_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionExperimentTreatments","id":"T1","attributes":{"name":"Blue"},"relationships":{"appStoreVersionExperimentV2":{"data":{"type":"appStoreVersionExperiments","id":"FOREIGN"}}}}}`))
	}))
	defer srv.Close()
	err := DeleteExperimentTreatment(context.Background(), newTestClient(t, srv), "E1", "T1")
	if err == nil || deletes != 0 {
		t.Fatalf("err=%v deletes=%d", err, deletes)
	}
}
