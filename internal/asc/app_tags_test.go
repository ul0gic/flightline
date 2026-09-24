package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF4B_ASCListAppTagsPaginatesWithoutTerritories(t *testing.T) {
	var requests int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assertF4BAppTagRequest(t, r, requests == 1)
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":  []any{map[string]any{"type": "appTags", "id": "TAG-1", "attributes": map[string]any{"name": "Focus", "visibleInAppStore": true}}},
				"links": map[string]string{"next": srv.URL + "/v1/apps/APP-1/appTags?cursor=next"},
			})
			return
		}
		if got := r.URL.Query().Get("cursor"); got != "next" {
			t.Errorf("cursor = %q, want next", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []any{map[string]any{"type": "appTags", "id": "TAG-2", "attributes": map[string]any{"name": "Work", "visibleInAppStore": false}}},
			"links": map[string]string{},
		})
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	tags, err := ListAppTags(context.Background(), c, "APP-1")
	if err != nil {
		t.Fatalf("ListAppTags: %v", err)
	}
	if len(tags) != 2 || tags[0].ID != "TAG-1" || tags[1].ID != "TAG-2" {
		t.Fatalf("tags = %#v, want two ordered pages", tags)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func assertF4BAppTagRequest(t *testing.T, r *http.Request, firstPage bool) {
	t.Helper()
	if r.Method != http.MethodGet || r.URL.Path != "/v1/apps/APP-1/appTags" {
		t.Errorf("request = %s %s, want GET /v1/apps/APP-1/appTags", r.Method, r.URL.Path)
	}
	if firstPage && r.URL.Query().Get("fields[appTags]") != "name,visibleInAppStore" {
		t.Errorf("fields[appTags] = %q", r.URL.Query().Get("fields[appTags]"))
	}
	if firstPage && r.URL.Query().Get("limit") != "200" {
		t.Errorf("limit = %q, want 200", r.URL.Query().Get("limit"))
	}
	if r.URL.Query().Has("include") || r.URL.Query().Has("fields[territories]") || r.URL.Query().Has("limit[territories]") {
		t.Errorf("query includes deprecated territory assumptions: %s", r.URL.RawQuery)
	}
}

func TestF4B_ASCPatchAppTagVisibilityPayload(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/appTags/TAG-1" {
			t.Errorf("request = %s %s, want PATCH /v1/appTags/TAG-1", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"type":"appTags","id":"TAG-1","attributes":{"name":"Focus","visibleInAppStore":false}}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	updated, err := PatchAppTagVisibility(context.Background(), c, "TAG-1", false)
	if err != nil {
		t.Fatalf("PatchAppTagVisibility: %v", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok || data["type"] != "appTags" || data["id"] != "TAG-1" {
		t.Fatalf("request data = %#v", body["data"])
	}
	attributes, ok := data["attributes"].(map[string]any)
	if !ok || len(attributes) != 1 || attributes["visibleInAppStore"] != false {
		t.Fatalf("request attributes = %#v, want visibility only", data["attributes"])
	}
	if updated.ID != "TAG-1" || updated.Attributes.VisibleInAppStore == nil || *updated.Attributes.VisibleInAppStore {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestF4B_ASCTagRequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"status":"403","code":"FORBIDDEN","title":"Forbidden","detail":"No access"}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	_, err := ListAppTags(context.Background(), c, "APP-1")
	if err == nil || !strings.Contains(err.Error(), "FORBIDDEN") {
		t.Fatalf("ListAppTags error = %v, want surfaced API failure", err)
	}
}
