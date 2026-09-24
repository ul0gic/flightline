package asc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF6A_ReviewAttachmentsListPreservesParentAndPagination(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/appStoreReviewDetails/R1/appStoreReviewAttachments" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreReviewAttachments","id":"A2","attributes":{"fileName":"support.pdf","assetDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appStoreReviewAttachments","id":"A1","attributes":{"fileName":"details.pdf","sourceFileChecksum":"abc","assetDeliveryState":{"state":"UPLOAD_COMPLETE"}},"relationships":{"appStoreReviewDetail":{"data":{"type":"appStoreReviewDetails","id":"R1"}}}}],"links":{"next":"` + serverURL + `/v1/appStoreReviewDetails/R1/appStoreReviewAttachments?page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	attachments, err := ListReviewAttachments(context.Background(), newTestClient(t, srv), "R1")
	if err != nil || len(attachments) != 2 || attachments[0].SourceFileChecksum != "abc" || attachments[1].ReviewDetailID != "R1" {
		t.Fatalf("attachments=%+v err=%v", attachments, err)
	}
}

func TestF6A_ReviewAttachmentRejectsForeignParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"type":"appStoreReviewAttachments","id":"A1","relationships":{"appStoreReviewDetail":{"data":{"type":"appStoreReviewDetails","id":"OTHER"}}}}],"links":{}}`))
	}))
	defer srv.Close()
	_, err := ListReviewAttachments(context.Background(), newTestClient(t, srv), "R1")
	if err == nil || !strings.Contains(err.Error(), "another review detail") {
		t.Fatalf("err=%v", err)
	}
}

func TestF6A_DeleteReviewAttachmentChecksParent(t *testing.T) {
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appStoreReviewAttachments","id":"A1"}],"links":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := DeleteReviewAttachment(context.Background(), c, "R1", "FOREIGN"); err == nil || deletes != 0 {
		t.Fatalf("foreign err=%v deletes=%d", err, deletes)
	}
	if err := DeleteReviewAttachment(context.Background(), c, "R1", "A1"); err != nil || deletes != 1 {
		t.Fatalf("delete err=%v deletes=%d", err, deletes)
	}
}
