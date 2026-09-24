package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF6B_ReadContentRightsRequiresExactAppAndKnownDeclaration(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantError  bool
		wantValue  *string
	}{
		{name: "declared", body: `{"data":{"type":"apps","id":"A1","attributes":{"contentRightsDeclaration":"USES_THIRD_PARTY_CONTENT"}}}`, wantValue: ptrF6B(ContentRightsUsesThirdParty)},
		{name: "undeclared", body: `{"data":{"type":"apps","id":"A1","attributes":{}}}`},
		{name: "wrong app", body: `{"data":{"type":"apps","id":"A2","attributes":{"contentRightsDeclaration":"USES_THIRD_PARTY_CONTENT"}}}`, wantError: true},
		{name: "unknown declaration", body: `{"data":{"type":"apps","id":"A1","attributes":{"contentRightsDeclaration":"UNMODIFIED"}}}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/apps/A1" || r.URL.Query().Get("fields[apps]") != "contentRightsDeclaration" {
					t.Errorf("request: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer srv.Close()
			got, err := ReadContentRights(context.Background(), fixtureClient(t, srv), "A1")
			if (err != nil) != test.wantError {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			if !test.wantError && ((got.Declaration == nil) != (test.wantValue == nil) || got.Declaration != nil && *got.Declaration != *test.wantValue) {
				t.Fatalf("declaration=%v, want=%v", got.Declaration, test.wantValue)
			}
		})
	}
}

func ptrF6B(value string) *string { return &value }

func TestF6B_PatchContentRightsSendsOnlyExplicitDeclaration(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/apps/A1" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1"}}`)
	}))
	defer srv.Close()
	if err := PatchContentRights(context.Background(), fixtureClient(t, srv), "A1", ContentRightsUsesThirdParty); err != nil {
		t.Fatal(err)
	}
	data, _ := body["data"].(map[string]any)
	attrs, _ := data["attributes"].(map[string]any)
	if data["type"] != "apps" || data["id"] != "A1" || len(attrs) != 1 || attrs["contentRightsDeclaration"] != ContentRightsUsesThirdParty {
		t.Fatalf("body=%v", body)
	}
}
