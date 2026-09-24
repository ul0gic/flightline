package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestG8_EventUploadWireContract(t *testing.T) {
	for _, kind := range []AssetKind{AssetKindAppEventCardScreenshot, AssetKindAppEventDetailsScreenshot, AssetKindAppEventCardVideo, AssetKindAppEventDetailsVideo} {
		t.Run(kind.String(), func(t *testing.T) { runEventUploadWireContract(t, kind) })
	}
}

func runEventUploadWireContract(t *testing.T, kind AssetKind) {
	t.Helper()
	withUploadCacheRoot(t)
	ep, err := kind.endpoints()
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.Method]++
		switch r.Method {
		case http.MethodPost:
			assertG8EventReserve(t, r, ep)
			writeAssetUploadGateJSON(t, w, http.StatusCreated, assetUploadGateReserved(assetUploadGateCase{resourceType: ep.resourceType}, "E1", srv.URL))
		case http.MethodPut:
			if r.Header.Get("Authorization") != "" {
				t.Error("authorization sent to signed upload")
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodPatch:
			var body struct {
				Data struct {
					Attributes map[string]any `json:"attributes"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Data.Attributes) != 1 || body.Data.Attributes["uploaded"] != true {
				t.Errorf("event commit must contain uploaded only: %+v", body)
			}
			writeAssetUploadGateJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{"type": ep.resourceType, "id": "E1"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	result, err := fixtureClient(t, srv).Upload(context.Background(), UploadOptions{Kind: kind, ParentID: "L1", Asset: UploadAsset{Path: writeUploadPayload(t, "event.bin")}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Checksum != expectedUploadMD5 || result.ID != "E1" || calls["POST"] != 1 || calls["PUT"] != 1 || calls["PATCH"] != 1 {
		t.Fatalf("result=%+v calls=%v", result, calls)
	}
}

func TestG8_EventCheckpointBindsAssetVariant(t *testing.T) {
	root := withUploadCacheRoot(t)
	path := writeUploadPayload(t, "event.png")
	seedBUG075Checkpoint(t, root, UploadCheckpoint{SchemaVersion: UploadCheckpointSchemaVersion, AssetID: "E1", Kind: AssetKindAppEventCardScreenshot.String(), ResourceType: "appEventScreenshots", ParentType: "appEventLocalizations", ParentID: "L1", FilePath: path, FileSize: int64(len(uploadTestPayload)), Md5Hex: expectedUploadMD5})
	if err := VerifyPendingAssetResume(AssetKindAppEventCardScreenshot, path, "L1", expectedUploadMD5, "E1"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []AssetKind{AssetKindAppEventDetailsScreenshot, AssetKindAppEventCardVideo, AssetKindAppEventDetailsVideo} {
		if err := VerifyPendingAssetResume(kind, path, "L1", expectedUploadMD5, "E1"); err == nil {
			t.Fatalf("cross-variant checkpoint accepted: %s", kind)
		}
	}
}

func assertG8EventReserve(t *testing.T, r *http.Request, ep kindEndpoints) {
	t.Helper()
	var body reserveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	rel := body.Data.Relationships["appEventLocalization"].Data
	if r.URL.Path != ep.collectionPath || body.Data.Type != ep.resourceType || body.Data.Attributes.AppEventAssetType != ep.eventAssetType || rel.ID != "L1" || rel.Type != "appEventLocalizations" {
		t.Errorf("unexpected reserve: %+v", body)
	}
}

func TestG8_EventProcessingStateMatchesMediaKind(t *testing.T) {
	for _, kind := range []AssetKind{AssetKindAppEventCardScreenshot, AssetKindAppEventDetailsScreenshot, AssetKindAppEventCardVideo, AssetKindAppEventDetailsVideo} {
		done, err := inspectAssetState(kind, "E", AppMediaAssetState{State: "PROCESSING"})
		video := kind == AssetKindAppEventCardVideo || kind == AssetKindAppEventDetailsVideo
		if done || (video && err != nil) || (!video && err == nil) {
			t.Fatalf("kind=%s done=%t err=%v", kind, done, err)
		}
	}
}
