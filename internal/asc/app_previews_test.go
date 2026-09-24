package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF6A_PreviewSetsAndPreviewsUseMainParentAndAllPages(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/appStoreVersionLocalizations/L1/appPreviewSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"},"relationships":{"appStoreVersionLocalization":{"data":{"type":"appStoreVersionLocalizations","id":"L1"}}}}],"links":{}}`))
		case r.URL.Path == "/v1/appPreviewSets/S1/appPreviews" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P2","attributes":{"fileName":"second.mov","videoDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
		case r.URL.Path == "/v1/appPreviewSets/S1/appPreviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P1","attributes":{"fileName":"first.mov","sourceFileChecksum":"abc","videoDeliveryState":{"state":"PROCESSING"}}}],"links":{"next":"` + serverURL + `/v1/appPreviewSets/S1/appPreviews?page=2"}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	c := newTestClient(t, srv)
	sets, err := ListAppPreviewSets(context.Background(), c, PreviewParent{Type: "appStoreVersionLocalizations", ID: "L1"})
	if err != nil || len(sets) != 1 || sets[0].PreviewType != "IPHONE_67" {
		t.Fatalf("sets=%+v err=%v", sets, err)
	}
	previews, err := ListAppPreviews(context.Background(), c, sets[0].ID)
	if err != nil || len(previews) != 2 || previews[0].SourceFileChecksum != "abc" || previews[1].ID != "P2" {
		t.Fatalf("previews=%+v err=%v", previews, err)
	}
}

func TestF6A_PreviewSetRejectsForeignCPPRelationship(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"},"relationships":{"appCustomProductPageLocalization":{"data":{"type":"appCustomProductPageLocalizations","id":"FOREIGN"}}}}],"links":{}}`))
	}))
	defer srv.Close()
	_, err := ListAppPreviewSets(context.Background(), newTestClient(t, srv), PreviewParent{Type: "appCustomProductPageLocalizations", ID: "CPP-L1"})
	if err == nil || !strings.Contains(err.Error(), "different parent") {
		t.Fatalf("err=%v", err)
	}
}

func TestF6A_FindOrCreatePreviewSetUsesCPPRelationship(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			buf, _ := json.Marshal(body)
			if !strings.Contains(string(buf), `"appCustomProductPageLocalization"`) || !strings.Contains(string(buf), `"id":"CPP-L1"`) || !strings.Contains(string(buf), `"previewType":"IPHONE_67"`) {
				t.Errorf("body=%s", buf)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
	}))
	defer srv.Close()
	set, created, err := FindOrCreateAppPreviewSet(context.Background(), newTestClient(t, srv), PreviewParent{Type: "appCustomProductPageLocalizations", ID: "CPP-L1"}, "IPHONE_67")
	if err != nil || !created || set.ID != "S1" || posts != 1 {
		t.Fatalf("set=%+v created=%v err=%v posts=%d", set, created, err, posts)
	}
}

func TestF6A_DeletePreviewRequiresSetMembership(t *testing.T) {
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P1","attributes":{"fileName":"preview.mov"}}],"links":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := DeleteAppPreview(context.Background(), c, "S1", "FOREIGN"); err == nil || deletes != 0 {
		t.Fatalf("foreign err=%v deletes=%d", err, deletes)
	}
	if err := DeleteAppPreview(context.Background(), c, "S1", "P1"); err != nil || deletes != 1 {
		t.Fatalf("delete err=%v deletes=%d", err, deletes)
	}
}
