package asc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type assetUploadGateCase struct {
	name         string
	kind         AssetKind
	collection   string
	resourceType string
	relationship string
	parentType   string
	parentID     string
	complete     func(context.Context, *Client, string) error
}

func TestG6_AssetUploadLifecycle(t *testing.T) {
	tests := []assetUploadGateCase{
		{
			name:         "app preview",
			kind:         AssetKindAppPreview,
			collection:   "/v1/appPreviews",
			resourceType: "appPreviews",
			relationship: "appPreviewSet",
			parentType:   "appPreviewSets",
			parentID:     "S1",
			complete: func(ctx context.Context, c *Client, path string) error {
				_, _, err := UploadAppPreviewAndWait(ctx, c, "S1", path, false, AssetPollOptions{MaxAttempts: 1})
				return err
			},
		},
		{
			name:         "review attachment",
			kind:         AssetKindReviewAttachment,
			collection:   "/v1/appStoreReviewAttachments",
			resourceType: "appStoreReviewAttachments",
			relationship: "appStoreReviewDetail",
			parentType:   "appStoreReviewDetails",
			parentID:     "R1",
			complete: func(ctx context.Context, c *Client, path string) error {
				_, _, err := UploadReviewAttachmentAndWait(ctx, c, "R1", path, false, AssetPollOptions{MaxAttempts: 1})
				return err
			},
		},
		{
			name:         "IAP promotional image",
			kind:         AssetKindIAPPromotionalImage,
			collection:   "/v1/inAppPurchaseImages",
			resourceType: "inAppPurchaseImages",
			relationship: "inAppPurchase",
			parentType:   "inAppPurchases",
			parentID:     "I1",
			complete: func(ctx context.Context, c *Client, path string) error {
				result, err := c.Upload(ctx, UploadOptions{Kind: AssetKindIAPPromotionalImage, ParentID: "I1", Asset: UploadAsset{Path: path}})
				if err != nil {
					return err
				}
				_, err = WaitIAPPromotionalImage(ctx, c, result.ID, AssetPollOptions{MaxAttempts: 1})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runAssetUploadGateCase(t, tc)
		})
	}
}

type assetUploadGateFixture struct {
	t        *testing.T
	tc       assetUploadGateCase
	assetID  string
	reserve  int
	commit   int
	complete int
	srv      *httptest.Server
}

func runAssetUploadGateCase(t *testing.T, tc assetUploadGateCase) {
	t.Helper()
	withUploadCacheRoot(t)
	fixture := newAssetUploadGateFixture(t, tc)
	if err := tc.complete(context.Background(), fixtureClient(t, fixture.srv), writeUploadPayload(t, "asset.bin")); err != nil {
		t.Fatal(err)
	}
	if fixture.reserve != 1 || fixture.commit != 1 || fixture.complete != 1 {
		t.Fatalf("reserve=%d commit=%d complete=%d, want 1 each", fixture.reserve, fixture.commit, fixture.complete)
	}
}

func newAssetUploadGateFixture(t *testing.T, tc assetUploadGateCase) *assetUploadGateFixture {
	t.Helper()
	fixture := &assetUploadGateFixture{t: t, tc: tc, assetID: "asset-1"}
	fixture.srv = httptest.NewServer(http.HandlerFunc(fixture.handle))
	t.Cleanup(fixture.srv.Close)
	return fixture
}

func (f *assetUploadGateFixture) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == f.tc.collection:
		f.handleReserve(w, r)
	case r.Method == http.MethodPut && r.URL.Path == "/signed/"+f.assetID:
		f.handleSignedPut(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == f.tc.collection+"/"+f.assetID:
		f.handleCommit(w, r)
	case r.Method == http.MethodGet && r.URL.Path == f.tc.collection+"/"+f.assetID:
		f.complete++
		writeAssetUploadGateJSON(f.t, w, http.StatusOK, assetUploadGateComplete(f.tc, f.assetID))
	default:
		http.NotFound(w, r)
	}
}

func (f *assetUploadGateFixture) handleReserve(w http.ResponseWriter, r *http.Request) {
	assertAssetUploadGateReserve(f.t, r, f.tc)
	f.reserve++
	writeAssetUploadGateJSON(f.t, w, http.StatusCreated, assetUploadGateReserved(f.tc, f.assetID, f.srv.URL))
}

func (f *assetUploadGateFixture) handleSignedPut(w http.ResponseWriter, r *http.Request) {
	body, err := readAllLimit(r.Body, 1<<20)
	if err != nil {
		f.t.Errorf("read signed upload: %v", err)
	}
	if !bytes.Equal(body, uploadTestPayload) {
		f.t.Errorf("signed bytes = %q, want %q", body, uploadTestPayload)
	}
	w.WriteHeader(http.StatusOK)
}

func (f *assetUploadGateFixture) handleCommit(w http.ResponseWriter, r *http.Request) {
	assertAssetUploadGateCommit(f.t, r, f.tc, f.assetID)
	f.commit++
	writeAssetUploadGateJSON(f.t, w, http.StatusOK, map[string]any{"data": map[string]any{"type": f.tc.resourceType, "id": f.assetID}})
}

func TestG6_AssetUploadProcessingFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/appPreviews/P1" {
			http.NotFound(w, r)
			return
		}
		writeAssetUploadGateJSON(t, w, http.StatusOK, map[string]any{"data": map[string]any{
			"type": "appPreviews", "id": "P1",
			"relationships": map[string]any{"appPreviewSet": map[string]any{"data": map[string]any{"type": "appPreviewSets", "id": "S1"}}},
			"attributes":    map[string]any{"videoDeliveryState": map[string]any{"state": "FAILED", "errors": []map[string]string{{"code": "VIDEO", "description": "bad codec"}}}},
		}})
	}))
	defer srv.Close()

	_, err := WaitAppPreviewProcessing(context.Background(), fixtureClient(t, srv), "P1", AssetPollOptions{MaxAttempts: 1})
	if err == nil {
		t.Fatal("WaitAppPreviewProcessing succeeded for failed asset")
	}
}

func assertAssetUploadGateReserve(t *testing.T, r *http.Request, tc assetUploadGateCase) {
	t.Helper()
	var body struct {
		Data struct {
			Type          string `json:"type"`
			Relationships map[string]struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode reserve: %v", err)
		return
	}
	if body.Data.Type != tc.resourceType || len(body.Data.Relationships) != 1 {
		t.Errorf("reserve type/relationships = %q/%d, want %q/1", body.Data.Type, len(body.Data.Relationships), tc.resourceType)
		return
	}
	rel, ok := body.Data.Relationships[tc.relationship]
	if !ok || rel.Data.Type != tc.parentType || rel.Data.ID != tc.parentID {
		t.Errorf("reserve relationship = %+v, want %s/%s", rel.Data, tc.parentType, tc.parentID)
	}
}

func assertAssetUploadGateCommit(t *testing.T, r *http.Request, tc assetUploadGateCase, assetID string) {
	t.Helper()
	var body struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Uploaded           bool   `json:"uploaded"`
				SourceFileChecksum string `json:"sourceFileChecksum"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode commit: %v", err)
		return
	}
	if body.Data.Type != tc.resourceType || body.Data.ID != assetID || !body.Data.Attributes.Uploaded || body.Data.Attributes.SourceFileChecksum != expectedUploadMD5 {
		t.Errorf("commit = %+v, want %s/%s uploaded with checksum %s", body.Data, tc.resourceType, assetID, expectedUploadMD5)
	}
}

func assetUploadGateReserved(tc assetUploadGateCase, assetID, serverURL string) map[string]any {
	return map[string]any{"data": map[string]any{
		"type": tc.resourceType, "id": assetID,
		"attributes": map[string]any{"uploadOperations": []map[string]any{{"method": "PUT", "url": serverURL + "/signed/" + assetID, "length": len(uploadTestPayload), "offset": 0}}},
	}}
}

func assetUploadGateComplete(tc assetUploadGateCase, assetID string) map[string]any {
	data := map[string]any{"type": tc.resourceType, "id": assetID, "attributes": map[string]any{}}
	switch tc.kind {
	case AssetKindAppPreview:
		data["relationships"] = map[string]any{"appPreviewSet": map[string]any{"data": map[string]any{"type": tc.parentType, "id": tc.parentID}}}
		data["attributes"] = map[string]any{"videoDeliveryState": map[string]any{"state": "COMPLETE"}}
	case AssetKindReviewAttachment:
		data["relationships"] = map[string]any{"appStoreReviewDetail": map[string]any{"data": map[string]any{"type": tc.parentType, "id": tc.parentID}}}
		data["attributes"] = map[string]any{"assetDeliveryState": map[string]any{"state": "COMPLETE"}}
	case AssetKindIAPPromotionalImage:
		data["attributes"] = map[string]any{"state": "PREPARE_FOR_SUBMISSION"}
	}
	return map[string]any{"data": data}
}

func writeAssetUploadGateJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
