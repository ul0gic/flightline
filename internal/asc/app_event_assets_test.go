package asc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF8B_MediaReadsBothFamiliesAndPagination(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/appEventLocalizations/L1/appEventScreenshots" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"type":"appEventScreenshots","id":"S2","attributes":{"fileName":"detail.png","appEventAssetType":"EVENT_DETAILS_PAGE","assetDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
		case r.URL.Path == "/v1/appEventLocalizations/L1/appEventScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appEventScreenshots","id":"S1","attributes":{"fileName":"card.png","appEventAssetType":"EVENT_CARD","assetDeliveryState":{"state":"COMPLETE"}}}],"links":{"next":"` + serverURL + `/v1/appEventLocalizations/L1/appEventScreenshots?page=2"}}`))
		case r.URL.Path == "/v1/appEventLocalizations/L1/appEventVideoClips":
			_, _ = w.Write([]byte(`{"data":[{"type":"appEventVideoClips","id":"V1","attributes":{"fileName":"card.mp4","appEventAssetType":"EVENT_CARD","videoDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
		default:
			t.Errorf("unexpected path %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	items, err := ListAppEventMedia(context.Background(), fixtureClient(t, srv), "L1")
	if err != nil || len(items) != 3 || items[2].Kind != "appEventVideoClips" || items[2].DeliveryState.State != "COMPLETE" {
		t.Fatalf("media=%+v err=%v", items, err)
	}
}

func TestF8B_MediaProcessingFailureIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"appEventVideoClips","id":"V1","attributes":{"fileName":"event.mp4","appEventAssetType":"EVENT_CARD","videoDeliveryState":{"state":"FAILED","errors":[{"code":"BAD_VIDEO","description":"Invalid frame"}]}}}}`))
	}))
	defer srv.Close()
	media, err := WaitAppEventMedia(context.Background(), fixtureClient(t, srv), "V1", true, AssetPollOptions{MaxAttempts: 2})
	if err == nil || !strings.Contains(err.Error(), "BAD_VIDEO") || media.ID != "V1" {
		t.Fatalf("media=%+v err=%v", media, err)
	}
}

func TestF8B_ForeignMediaDeleteMakesZeroWrites(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
			t.Errorf("unexpected write %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
	}))
	defer srv.Close()
	if err := DeleteAppEventMedia(context.Background(), fixtureClient(t, srv), "L1", "FOREIGN"); err == nil || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestF8B_UploadAndWaitConfirmsParentTypeAndCompletion(t *testing.T) {
	withUploadCacheRoot(t)
	path := writeUploadPayload(t, "event.png")
	eps, err := AssetKindAppEventCardScreenshot.endpoints()
	if err != nil {
		t.Fatal(err)
	}
	posts, puts, patches, gets := 0, 0, 0, 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			posts++
			writeAssetUploadGateJSON(t, w, http.StatusCreated, assetUploadGateReserved(assetUploadGateCase{resourceType: eps.resourceType}, "S1", srv.URL))
		case http.MethodPut:
			puts++
			w.WriteHeader(http.StatusOK)
		case http.MethodPatch:
			patches++
			writeAssetUploadGateJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{"type": "appEventScreenshots", "id": "S1"}})
		case http.MethodGet:
			gets++
			_, _ = w.Write([]byte(`{"data":{"type":"appEventScreenshots","id":"S1","attributes":{"fileName":"event.png","appEventAssetType":"EVENT_CARD","assetDeliveryState":{"state":"COMPLETE"}},"relationships":{"appEventLocalization":{"data":{"type":"appEventLocalizations","id":"L1"}}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	result, media, err := UploadAppEventMediaAndWait(context.Background(), fixtureClient(t, srv), "L1", path, "EVENT_CARD", false, false, AssetPollOptions{MaxAttempts: 2})
	if err != nil || result.ID != "S1" || media.LocalizationID != "L1" || media.DeliveryState.State != "COMPLETE" || posts != 1 || puts != 1 || patches != 1 || gets != 1 {
		t.Fatalf("result=%+v media=%+v counts=%d/%d/%d/%d err=%v", result, media, posts, puts, patches, gets, err)
	}
}
