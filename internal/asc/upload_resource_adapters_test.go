package asc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestF6A_PreviewPollWaitsForComplete(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/appPreviews/P1" {
			t.Errorf("path=%s", r.URL.Path)
		}
		requests++
		state := "PROCESSING"
		if requests == 3 {
			state = "COMPLETE"
		}
		_, _ = w.Write([]byte(`{"data":{"type":"appPreviews","id":"P1","relationships":{"appPreviewSet":{"data":{"type":"appPreviewSets","id":"S1"}}},"attributes":{"videoDeliveryState":{"state":"` + state + `"}}}}`))
	}))
	defer srv.Close()
	preview, err := WaitAppPreviewProcessing(context.Background(), newTestClient(t, srv), "P1", AssetPollOptions{MaxAttempts: 3})
	if err != nil || preview.VideoDeliveryState.State != "COMPLETE" || requests != 3 {
		t.Fatalf("preview=%+v err=%v requests=%d", preview, err, requests)
	}
}

func TestF6A_ProcessingPendingIsTypedAndKeepsAssetID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"type":"appPreviews","id":"P1","relationships":{"appPreviewSet":{"data":{"type":"appPreviewSets","id":"S1"}}},"attributes":{"videoDeliveryState":{"state":"PROCESSING"}}}}`))
	}))
	defer srv.Close()
	_, err := WaitAppPreviewProcessing(context.Background(), newTestClient(t, srv), "P1", AssetPollOptions{MaxAttempts: 1})
	var pending *AssetProcessingPendingError
	if !errors.As(err, &pending) || pending.ID != "P1" || !strings.Contains(err.Error(), "before any new upload") {
		t.Fatalf("err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = WaitAppPreviewProcessing(ctx, newTestClient(t, srv), "P1", AssetPollOptions{MaxAttempts: 2, Interval: time.Millisecond})
	if !errors.As(err, &pending) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestF6A_InvalidPollOptionsFailBeforeUpload(t *testing.T) {
	if _, _, err := UploadAppPreviewAndWait(context.Background(), nil, "S1", "missing.mov", false, AssetPollOptions{MaxAttempts: 0}); err == nil {
		t.Fatal("preview upload reached client with invalid poll options")
	}
	if _, _, err := UploadReviewAttachmentAndWait(context.Background(), nil, "D1", "missing.pdf", false, AssetPollOptions{MaxAttempts: 1, Interval: -time.Second}); err == nil {
		t.Fatal("review attachment upload reached client with invalid poll options")
	}
}

func TestF6A_ResumeRejectsForeignParentCheckpoint(t *testing.T) {
	root := withUploadCacheRoot(t)
	file := writeUploadPayload(t, "preview.mov")
	cp := UploadCheckpoint{SchemaVersion: UploadCheckpointSchemaVersion, AssetID: "P1", Kind: AssetKindAppPreview.String(), ResourceType: "appPreviews", ParentType: "appPreviewSets", ParentID: "FOREIGN", FilePath: file, FileSize: int64(len(uploadTestPayload)), Md5Hex: expectedUploadMD5}
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", "P1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPendingAssetResume(AssetKindAppPreview, file, "S1", expectedUploadMD5, "P1"); err == nil {
		t.Fatal("foreign parent checkpoint accepted")
	}
}

func TestF6A_ProcessingFailureAndUnknownFailClosed(t *testing.T) {
	done, err := inspectAssetState(AssetKindAppPreview, "P1", AppMediaAssetState{State: "FAILED", Errors: []AppMediaStateError{{Code: "VIDEO", Description: "unsupported"}}})
	if done || err == nil || !strings.Contains(err.Error(), "VIDEO") {
		t.Fatalf("failed done=%v err=%v", done, err)
	}
	done, err = inspectAssetState(AssetKindReviewAttachment, "A1", AppMediaAssetState{State: "PROCESSING"})
	if done || err == nil || !strings.Contains(err.Error(), "unknown processing state") {
		t.Fatalf("unknown done=%v err=%v", done, err)
	}
}
