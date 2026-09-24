package cmd

import (
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

func TestF8C_PendingExperimentScreenshotBlocksSecondReservation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts++
			return
		}
		switch r.URL.Path {
		case "/v1/appStoreVersionExperimentTreatmentLocalizations/L1/appScreenshotSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}],"links":{}}`))
		case "/v1/appScreenshotSets/S1/appScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"I1","attributes":{"fileName":"test.png","assetDeliveryState":{"state":"AWAITING_UPLOAD"}}}],"links":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := uploadExperimentAsset(context.Background(), fixtureASCClient(t, srv), "L1", false, "APP_IPHONE_67", file, false, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "I1") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
	_, err = uploadExperimentAsset(context.Background(), fixtureASCClient(t, srv), "L1", false, "APP_IPHONE_67", file, true, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "checkpoint") || posts != 0 {
		t.Fatalf("resume err=%v posts=%d", err, posts)
	}
}

func TestF8C_InvalidPollPreventsExperimentSetCreation(t *testing.T) {
	_, err := uploadExperimentAsset(context.Background(), nil, "L1", false, "APP_IPHONE_67", "missing.png", false, asc.AssetPollOptions{})
	if err == nil || !strings.Contains(err.Error(), "poll") {
		t.Fatalf("err=%v", err)
	}
}

func TestF8C_ResumeDoesNotCreateAbsentSet(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
	}))
	defer srv.Close()
	_, err := uploadExperimentAsset(context.Background(), fixtureASCClient(t, srv), "L1", false, "APP_IPHONE_67", file, true, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF8C_AmbiguousScreenshotMatchBlocksUpload(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"I1","attributes":{"fileName":"test.png","assetDeliveryState":{"state":"COMPLETE"}}},{"type":"appScreenshots","id":"I2","attributes":{"fileName":"test.png","assetDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
	}))
	defer srv.Close()
	_, err := uploadExperimentScreenshot(context.Background(), fixtureASCClient(t, srv), "S1", file, "known", false, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestF8C_ExperimentScreenshotUploadConverges(t *testing.T) {
	t.Setenv("FLIGHTLINE_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(file, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	checksum, err := md5HexOfFile(file)
	if err != nil {
		t.Fatal(err)
	}
	f := &experimentUploadFixture{t: t, checksum: checksum}
	srv := httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	defer srv.Close()
	f.url = srv.URL
	c := fixtureASCClient(t, srv)
	poll := asc.AssetPollOptions{MaxAttempts: 2}
	first, err := uploadExperimentAsset(context.Background(), c, "L1", false, "APP_IPHONE_67", file, false, poll)
	if err != nil || first.Action != "uploaded" || first.AssetID != "I1" || first.State != "COMPLETE" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := uploadExperimentAsset(context.Background(), c, "L1", false, "APP_IPHONE_67", file, false, poll)
	if err != nil || second.Action != "existing" || second.Changed || f.setsCreated != 1 || f.reserves != 1 || f.chunks != 1 || f.commits != 1 {
		t.Fatalf("second=%+v err=%v counts=%d/%d/%d/%d", second, err, f.setsCreated, f.reserves, f.chunks, f.commits)
	}
}

type experimentUploadFixture struct {
	t                                      *testing.T
	url, checksum                          string
	setsCreated, reserves, chunks, commits int
}

func (f *experimentUploadFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/appStoreVersionExperimentTreatmentLocalizations/L1/appScreenshotSets" && r.Method == http.MethodGet:
		f.writeSets(w)
	case r.URL.Path == "/v1/appScreenshotSets" && r.Method == http.MethodPost:
		f.setsCreated++
		f.checkRelationship(r, `"appStoreVersionExperimentTreatmentLocalization"`)
		_, _ = fmt.Fprint(w, `{"data":{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}}`)
	case r.URL.Path == "/v1/appScreenshotSets/S1/appScreenshots" && r.Method == http.MethodGet:
		f.writeScreenshots(w)
	case r.URL.Path == "/v1/appScreenshots" && r.Method == http.MethodPost:
		f.reserves++
		f.checkRelationship(r, `"appScreenshotSet":{"data":{"id":"S1","type":"appScreenshotSets"}}`)
		_, _ = fmt.Fprintf(w, `{"data":{"type":"appScreenshots","id":"I1","attributes":{"fileName":"test.png","fileSize":5,"assetDeliveryState":{"state":"AWAITING_UPLOAD"},"uploadOperations":[{"method":"PUT","url":%q,"length":5,"offset":0}]}}}`, f.url+"/chunk")
	case r.URL.Path == "/chunk" && r.Method == http.MethodPut:
		f.chunks++
		if r.Header.Get("Authorization") != "" {
			f.t.Error("signed chunk carried authorization")
		}
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/v1/appScreenshots/I1" && r.Method == http.MethodPatch:
		f.commits++
		_, _ = fmt.Fprintf(w, `{"data":{"type":"appScreenshots","id":"I1","attributes":{"sourceFileChecksum":%q,"assetDeliveryState":{"state":"UPLOAD_COMPLETE"}}}}`, f.checksum)
	case r.URL.Path == "/v1/appScreenshots/I1" && r.Method == http.MethodGet:
		_, _ = fmt.Fprintf(w, `{"data":{"type":"appScreenshots","id":"I1","attributes":{"fileName":"test.png","sourceFileChecksum":%q,"assetDeliveryState":{"state":"COMPLETE"}},"relationships":{"appScreenshotSet":{"data":{"type":"appScreenshotSets","id":"S1"}}}}}`, f.checksum)
	default:
		f.t.Errorf("unexpected %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}
}
func (f *experimentUploadFixture) writeSets(w http.ResponseWriter) {
	if f.setsCreated == 0 {
		_, _ = fmt.Fprint(w, `{"data":[],"links":{}}`)
		return
	}
	_, _ = fmt.Fprint(w, `{"data":[{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"},"relationships":{"appStoreVersionExperimentTreatmentLocalization":{"data":{"type":"appStoreVersionExperimentTreatmentLocalizations","id":"L1"}}}}],"links":{}}`)
}
func (f *experimentUploadFixture) writeScreenshots(w http.ResponseWriter) {
	if f.commits == 0 {
		_, _ = fmt.Fprint(w, `{"data":[],"links":{}}`)
		return
	}
	_, _ = fmt.Fprintf(w, `{"data":[{"type":"appScreenshots","id":"I1","attributes":{"fileName":"test.png","sourceFileChecksum":%q,"assetDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`, f.checksum)
}
func (f *experimentUploadFixture) checkRelationship(r *http.Request, needle string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Error(err)
	}
	buf, _ := json.Marshal(body)
	if !strings.Contains(string(buf), needle) {
		f.t.Errorf("body=%s", buf)
	}
}
