package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestF6C_AppScreenshotOrderReadsFreshMembershipAndReplacesFullLinkage(t *testing.T) {
	var patchBody struct {
		Data []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appScreenshotSets/set-1/appScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"shot-2","attributes":{"fileName":"02.png","sourceFileChecksum":"bbb"}},{"type":"appScreenshots","id":"shot-1","attributes":{"fileName":"01.png","sourceFileChecksum":"aaa"}}],"links":{}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshotSets/set-1/relationships/appScreenshots":
			if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
				t.Errorf("decode PATCH: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := fixtureClient(t, srv)
	got, err := ListAppScreenshots(context.Background(), client, "set-1")
	if err != nil {
		t.Fatalf("ListAppScreenshots: %v", err)
	}
	if ids := []string{got[0].ID, got[1].ID}; !reflect.DeepEqual(ids, []string{"shot-2", "shot-1"}) {
		t.Fatalf("ordered membership = %v", ids)
	}
	if err := ReplaceAppScreenshotOrder(context.Background(), client, "set-1", []string{"shot-1", "shot-2"}); err != nil {
		t.Fatalf("ReplaceAppScreenshotOrder: %v", err)
	}
	if got := []string{patchBody.Data[0].Type, patchBody.Data[0].ID, patchBody.Data[1].Type, patchBody.Data[1].ID}; !reflect.DeepEqual(got, []string{"appScreenshots", "shot-1", "appScreenshots", "shot-2"}) {
		t.Fatalf("PATCH body = %#v", patchBody)
	}
}

func TestF6C_AppScreenshotOrderRejectsDuplicateMembership(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"shot-1"},{"type":"appScreenshots","id":"shot-1"}],"links":{}}`))
	}))
	defer srv.Close()

	if _, err := ListAppScreenshots(context.Background(), fixtureClient(t, srv), "set-1"); err == nil {
		t.Fatal("ListAppScreenshots accepted duplicate membership")
	}
}
