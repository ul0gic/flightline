package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF8C_ExperimentSetCreateUsesTreatmentLocalization(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
			return
		}
		posts++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		payload, _ := json.Marshal(body)
		if !strings.Contains(string(payload), `"appStoreVersionExperimentTreatmentLocalization":{"data":{"id":"L1","type":"appStoreVersionExperimentTreatmentLocalizations"}}`) {
			t.Errorf("body=%s", payload)
		}
		_, _ = w.Write([]byte(`{"data":{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}}`))
	}))
	defer srv.Close()
	set, created, err := FindOrCreateExperimentAssetSet(context.Background(), newTestClient(t, srv), "L1", "APP_IPHONE_67", false)
	if err != nil || !created || set.ID != "S1" || posts != 1 {
		t.Fatalf("set=%+v created=%v err=%v posts=%d", set, created, err, posts)
	}
}

func TestF8C_ExperimentSetRejectsForeignLocalization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"},"relationships":{"appStoreVersionExperimentTreatmentLocalization":{"data":{"type":"appStoreVersionExperimentTreatmentLocalizations","id":"FOREIGN"}}}}],"links":{}}`))
	}))
	defer srv.Close()
	_, err := ListExperimentAssetSets(context.Background(), newTestClient(t, srv), "L1", false)
	if err == nil {
		t.Fatal("foreign localization accepted")
	}
}
