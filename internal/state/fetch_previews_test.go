package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF6A_FetchPreviewsRejectsPendingExistingAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/appStoreVersions/V1/appStoreVersionLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersionLocalizations","id":"L1","attributes":{"locale":"en-US"}}],"links":{}}`))
		case "/v1/appStoreVersionLocalizations/L1/appPreviewSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"}}],"links":{}}`))
		case "/v1/appPreviewSets/S1/appPreviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P1","attributes":{"fileName":"one.mov","sourceFileChecksum":"ABC","videoDeliveryState":{"state":"PROCESSING"}}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := FetchPreviews(context.Background(), fixtureClient(t, srv), "V1")
	if err == nil || !strings.Contains(err.Error(), "P1") || !strings.Contains(err.Error(), "PROCESSING") {
		t.Fatalf("err=%v", err)
	}
}

func TestF6A_FetchCPPPreviewsPreservesOrderChecksumFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/appCustomProductPageLocalizations/L1/appPreviewSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"}}],"links":{}}`))
		case "/v1/appPreviewSets/S1/appPreviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P1","attributes":{"fileName":"one.mov","sourceFileChecksum":"ABC","previewFrameTimeCode":"00:00:01.000","videoDeliveryState":{"state":"COMPLETE"}}},{"type":"appPreviews","id":"P2","attributes":{"fileName":"two.mov","sourceFileChecksum":"DEF","videoDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	types, err := FetchCPPPreviews(context.Background(), fixtureClient(t, srv), "L1")
	if err != nil || len(types["IPHONE_67"]) != 2 || types["IPHONE_67"][0].SourceFileChecksum != "ABC" || types["IPHONE_67"][0].PreviewFrameTimeCode == nil || types["IPHONE_67"][1].Path != "two.mov" {
		t.Fatalf("types=%+v err=%v", types, err)
	}
}
