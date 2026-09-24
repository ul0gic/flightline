package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF6B_ContentRightsGetShowsObservedDeclaration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1":
			_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1","attributes":{"contentRightsDeclaration":"USES_THIRD_PARTY_CONTENT"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cmd := newContentRightsCommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runContentRightsGetWithClient(cmd, "com.example.app", fixtureASCClient(t, srv), "json"); err != nil {
		t.Fatal(err)
	}
	var view ContentRightsView
	if err := json.Unmarshal(output.Bytes(), &view); err != nil || view.AppID != "A1" || view.Declaration == nil || *view.Declaration != "USES_THIRD_PARTY_CONTENT" {
		t.Fatalf("view=%+v err=%v output=%s", view, err, output.String())
	}
}

func TestF6B_ContentRightsSetRequiresConfirmation(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer srv.Close()
	cmd := newContentRightsCommand()
	cmd.SetContext(context.Background())
	err := runContentRightsSetWithClient(cmd, "com.example.app", "USES_THIRD_PARTY_CONTENT", false, fixtureASCClient(t, srv), "json")
	if err == nil || requests != 0 {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
}
