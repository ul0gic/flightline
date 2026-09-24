package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestF4A_ListAccessibilityDeclarationsPaginatesAndPreservesFalse(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"type":"accessibilityDeclarations","id":"D2","attributes":{"deviceFamily":"IPAD","state":"PUBLISHED","supportsVoiceover":false}}],"links":{}}`))
			return
		}
		if r.URL.Path != "/v1/apps/APP1/accessibilityDeclarations" || r.URL.Query().Get("limit") != "200" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsCaptions":true}}],"links":{"next":"` + serverURL + `/v1/apps/APP1/accessibilityDeclarations?page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	got, err := ListAccessibilityDeclarations(context.Background(), newTestClient(t, srv), "APP1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].State != "DRAFT" || got[1].State != "PUBLISHED" || got[1].SupportsVoiceover == nil || *got[1].SupportsVoiceover {
		t.Fatalf("declarations=%+v", got)
	}
}

func TestF4A_ListAccessibilityDeclarationsLaterPageFailure(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"links":{"next":"` + serverURL + `/v1/apps/APP1/accessibilityDeclarations?page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	if _, err := ListAccessibilityDeclarations(context.Background(), newTestClient(t, srv), "APP1"); err == nil {
		t.Fatal("later page failure accepted")
	}
}

func TestF4A_CreateKeepsExplicitFalseAndNoPublish(t *testing.T) {
	postedCh := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/accessibilityDeclarations" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var posted map[string]any
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Error(err)
		}
		postedCh <- posted
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsVoiceover":false}}}`))
	}))
	defer srv.Close()
	falseAnswer := false
	got, err := CreateAccessibilityDeclaration(context.Background(), newTestClient(t, srv), "APP1", AccessibilityDeclarationAttributes{
		DeviceFamily: "IPHONE", SupportsVoiceover: &falseAnswer,
	})
	if err != nil || got.State != "DRAFT" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	buf, _ := json.Marshal(<-postedCh)
	if !strings.Contains(string(buf), `"supportsVoiceover":false`) || strings.Contains(string(buf), `"publish"`) {
		t.Fatalf("request=%s", buf)
	}
}

func TestF4A_PublishRequiresFreshDraft(t *testing.T) {
	var patches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPatch {
			patches.Add(1)
		}
		_, _ = w.Write([]byte(`{"data":{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"PUBLISHED"}}}`))
	}))
	defer srv.Close()
	if _, err := PublishAccessibilityDeclaration(context.Background(), newTestClient(t, srv), "D1"); err == nil || patches.Load() != 0 {
		t.Fatalf("err=%v patches=%d", err, patches.Load())
	}
}

func TestF4A_UpdateMatchingAnswerDoesNotPatch(t *testing.T) {
	var patches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsVoiceover":false}}}`))
	}))
	defer srv.Close()
	falseValue := false
	got, err := UpdateAccessibilityDeclaration(context.Background(), newTestClient(t, srv), "D1", AccessibilityDeclarationAttributes{SupportsVoiceover: &falseValue})
	if err != nil || got.ID != "D1" || patches.Load() != 0 {
		t.Fatalf("got=%+v err=%v patches=%d", got, err, patches.Load())
	}
}

func TestF4A_PublishRequiresConfirmedResponse(t *testing.T) {
	var patches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPatch {
			patches.Add(1)
		}
		_, _ = w.Write([]byte(`{"data":{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT"}}}`))
	}))
	defer srv.Close()
	if _, err := PublishAccessibilityDeclaration(context.Background(), newTestClient(t, srv), "D1"); err == nil || patches.Load() != 1 {
		t.Fatalf("err=%v patches=%d", err, patches.Load())
	}
}
