package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF6A_ReviewAttachmentUploadBlocksUnresolvedSameFilename(t *testing.T) {
	t.Setenv("FLIGHTLINE_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "review.pdf")
	if err := os.WriteFile(file, []byte("attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts++
			t.Errorf("unexpected POST %s", r.URL)
			return
		}
		if r.URL.Path != "/v1/appStoreReviewDetails/D1/appStoreReviewAttachments" {
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appStoreReviewAttachments","id":"A1","attributes":{"fileName":"review.pdf","assetDeliveryState":{"state":"AWAITING_UPLOAD"}}}],"links":{}}`))
	}))
	defer srv.Close()
	_, err := uploadReviewAttachmentWithClient(context.Background(), fixtureASCClient(t, srv), "D1", file, false, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "A1") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
	_, err = uploadReviewAttachmentWithClient(context.Background(), fixtureASCClient(t, srv), "D1", file, true, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "checkpoint") || posts != 0 {
		t.Fatalf("resume err=%v posts=%d", err, posts)
	}
}

func TestF6A_ReviewAttachmentListAlwaysIncludesEmptyArray(t *testing.T) {
	result := ReviewAttachmentsResult{Items: make([]asc.ReviewAttachment, 0)}
	data, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(data), `"attachments":[]`) {
		t.Fatalf("json=%s err=%v", data, err)
	}
}
