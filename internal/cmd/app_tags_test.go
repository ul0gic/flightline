package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF4B_CmdVisibilityNoOp(t *testing.T) {
	var patchRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}]}`))
		case "/v1/apps/APP-1/appTags":
			_, _ = w.Write([]byte(`{"data":[{"type":"appTags","id":"TAG-1","attributes":{"name":"Focus","visibleInAppStore":true}}],"links":{}}`))
		case "/v1/appTags/TAG-1":
			patchRequests++
			t.Errorf("unexpected PATCH for identical visibility")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := fixtureASCClient(t, srv)

	result, err := setAppTagVisibility(context.Background(), client, "com.example.app", "TAG-1", true)
	if err != nil {
		t.Fatalf("setAppTagVisibility: %v", err)
	}
	if result.Changed || result.Tag.ID != "TAG-1" || patchRequests != 0 {
		t.Fatalf("result = %#v, patch requests = %d", result, patchRequests)
	}
}

func TestF4B_CmdVisibilityPatchPayload(t *testing.T) {
	var patchBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}]}`))
		case "/v1/apps/APP-1/appTags":
			_, _ = w.Write([]byte(`{"data":[{"type":"appTags","id":"TAG-1","attributes":{"name":"Focus","visibleInAppStore":true}}],"links":{}}`))
		case "/v1/appTags/TAG-1":
			if r.Method != http.MethodPatch {
				t.Errorf("method = %s, want PATCH", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
				t.Errorf("decode patch: %v", err)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appTags","id":"TAG-1","attributes":{"name":"Focus","visibleInAppStore":false}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := fixtureASCClient(t, srv)

	result, err := setAppTagVisibility(context.Background(), client, "com.example.app", "TAG-1", false)
	if err != nil {
		t.Fatalf("setAppTagVisibility: %v", err)
	}
	if !result.Changed || result.Tag.Attributes.VisibleInAppStore == nil || *result.Tag.Attributes.VisibleInAppStore {
		t.Fatalf("result = %#v, want changed and hidden", result)
	}
	data, ok := patchBody["data"].(map[string]any)
	if !ok || data["type"] != "appTags" || data["id"] != "TAG-1" {
		t.Fatalf("PATCH data = %#v", patchBody["data"])
	}
	attributes, ok := data["attributes"].(map[string]any)
	if !ok || len(attributes) != 1 || attributes["visibleInAppStore"] != false {
		t.Fatalf("PATCH attributes = %#v, want visibility only", data["attributes"])
	}
}

func TestF4B_CmdVisibilityRequiresAssignedTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/apps" {
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
	}))
	defer srv.Close()
	client := fixtureASCClient(t, srv)

	_, err := setAppTagVisibility(context.Background(), client, "com.example.app", "TAG-OTHER", false)
	if err == nil || !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("setAppTagVisibility error = %v, want unassigned tag failure", err)
	}
}

func TestF4B_CmdMalformedPatchResponseRequiresFreshRead(t *testing.T) {
	var patchRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP-1","attributes":{"bundleId":"com.example.app"}}]}`))
		case "/v1/apps/APP-1/appTags":
			_, _ = w.Write([]byte(`{"data":[{"type":"appTags","id":"TAG-1","attributes":{"name":"Focus","visibleInAppStore":true}}],"links":{}}`))
		case "/v1/appTags/TAG-1":
			patchRequests++
			_, _ = w.Write([]byte(`{"data":{"type":"appTags","id":"TAG-1","attributes":{"name":"Focus"}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := fixtureASCClient(t, srv)

	_, err := setAppTagVisibility(context.Background(), client, "com.example.app", "TAG-1", false)
	if err == nil || !strings.Contains(err.Error(), "inspect current state") {
		t.Fatalf("setAppTagVisibility error = %v, want fresh-read guidance", err)
	}
	if patchRequests != 1 {
		t.Fatalf("PATCH requests = %d, want exactly one", patchRequests)
	}
}

func TestF4B_CmdEmptyListJSON(t *testing.T) {
	encoded, err := json.Marshal(AppTagList{Tags: []AppTagView{}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(encoded); got != `{"tags":[]}` {
		t.Fatalf("empty list JSON = %s, want {\"tags\":[]}", got)
	}
	var attrs asc.AppTagAttributes
	if err := json.Unmarshal([]byte(`{"name":"Focus","visibleInAppStore":false}`), &attrs); err != nil || attrs.VisibleInAppStore == nil || *attrs.VisibleInAppStore {
		t.Fatalf("false visibility did not survive decoding: attrs=%#v err=%v", attrs, err)
	}
}

func TestF4B_CmdConstructorHasExpectedCommands(t *testing.T) {
	group := newAppTagsCommand()
	if group.Name() != "app-tags" || group.Commands()[0].Name() == "" {
		t.Fatalf("constructor returned unexpected command tree: %#v", group)
	}
	found := map[string]bool{}
	for _, child := range group.Commands() {
		found[child.Name()] = true
	}
	if !found["list"] || !found["set-visibility"] {
		t.Fatal("constructor must expose list and set-visibility")
	}
}
