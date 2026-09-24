package asc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestC2C_CreateAppEncryptionDeclaration_Request(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/appEncryptionDeclarations" {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"data":{"type":"appEncryptionDeclarations","id":"DECL1"}}`)
	}))
	defer srv.Close()

	attrs := AppEncryptionDeclarationCreateAttributes{
		AppDescription:                  "Uses TLS",
		ContainsProprietaryCryptography: true,
		ContainsThirdPartyCryptography:  false,
		AvailableOnFrenchStore:          true,
	}
	created, err := CreateAppEncryptionDeclaration(context.Background(), fixtureClient(t, srv), "APP1", attrs)
	if err != nil {
		t.Fatalf("CreateAppEncryptionDeclaration: %v", err)
	}
	if created.ID != "DECL1" {
		t.Fatalf("created ID = %q", created.ID)
	}
	data := requiredMap(t, body, "data")
	if data["type"] != "appEncryptionDeclarations" {
		t.Fatalf("data=%v", data)
	}
	gotAttrs := requiredMap(t, data, "attributes")
	if gotAttrs["appDescription"] != "Uses TLS" || gotAttrs["containsProprietaryCryptography"] != true ||
		gotAttrs["containsThirdPartyCryptography"] != false || gotAttrs["availableOnFrenchStore"] != true {
		t.Fatalf("attributes=%v", gotAttrs)
	}
	rels := requiredMap(t, data, "relationships")
	app := requiredMap(t, requiredMap(t, rels, "app"), "data")
	if app["type"] != "apps" || app["id"] != "APP1" {
		t.Fatalf("app relationship=%v", app)
	}
}

func TestC2C_ListAppEncryptionDeclarations_UsesTopLevelFilterAndPages(t *testing.T) {
	first, err := os.ReadFile(filepath.Join("testdata", "c2c", "declarations-page-1.json"))
	if err != nil {
		t.Fatalf("read first fixture: %v", err)
	}
	second, err := os.ReadFile(filepath.Join("testdata", "c2c", "declarations-page-2.json"))
	if err != nil {
		t.Fatalf("read second fixture: %v", err)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/appEncryptionDeclarations" {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "next" {
			_, _ = w.Write(second)
			return
		}
		if got := r.URL.Query().Get("filter[app]"); got != "APP1" {
			t.Fatalf("filter[app] = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "200" {
			t.Fatalf("limit = %q", got)
		}
		body := string(first)
		body = replaceFixtureNext(t, body, srv.URL+"/v1/appEncryptionDeclarations?cursor=next")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	got, err := ListAppEncryptionDeclarations(context.Background(), fixtureClient(t, srv), "APP1")
	if err != nil {
		t.Fatalf("ListAppEncryptionDeclarations: %v", err)
	}
	if len(got) != 2 || got[0].ID != "DECL1" || got[1].ID != "DECL2" {
		t.Fatalf("declarations=%+v", got)
	}
}

func TestC2C_SetBuildEncryptionDeclaration_PatchesDirectRelationship(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/builds/BUILD1/relationships/appEncryptionDeclaration" {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := SetBuildEncryptionDeclaration(context.Background(), fixtureClient(t, srv), "BUILD1", "DECL1"); err != nil {
		t.Fatalf("SetBuildEncryptionDeclaration: %v", err)
	}
	data := requiredMap(t, body, "data")
	if data["type"] != "appEncryptionDeclarations" || data["id"] != "DECL1" {
		t.Fatalf("body=%v", body)
	}
}

func requiredMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	got, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("%s=%T, want object", key, value[key])
	}
	return got
}

func replaceFixtureNext(t *testing.T, body, next string) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	requiredMap(t, value, "links")["next"] = next
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return string(encoded)
}
