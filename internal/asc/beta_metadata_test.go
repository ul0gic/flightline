package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5C_BetaMetadataReadersPageAndScope(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/A1/betaAppLocalizations":
			if r.URL.Query().Get("cursor") == "next" {
				_, _ = fmt.Fprint(w, `{"data":[{"type":"betaAppLocalizations","id":"L2","attributes":{"locale":"fr-FR"}}]}`)
				return
			}
			if r.URL.Query().Get("limit") != "200" {
				t.Errorf("app localization query=%v", r.URL.Query())
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"betaAppLocalizations","id":"L1","attributes":{"locale":"en-US","description":"Test"}}],"links":{"next":%q}}`, server.URL+"/v1/apps/A1/betaAppLocalizations?cursor=next")
		case "/v1/builds/B1/betaBuildLocalizations":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"betaBuildLocalizations","id":"BL1","attributes":{"locale":"en-US","whatsNew":"Try sync"}}]}`)
		case "/v1/betaAppReviewDetails":
			if r.URL.Query().Get("filter[app]") != "A1" {
				t.Errorf("beta review filter=%v", r.URL.Query())
			}
			_, _ = fmt.Fprint(w, `{"data":[{"type":"betaAppReviewDetails","id":"R1","attributes":{"contactEmail":"beta@example.com","demoAccountPassword":"server-secret"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := fixtureClient(t, server)
	apps, err := ListBetaAppLocalizations(context.Background(), c, "A1")
	if err != nil || len(apps) != 2 || apps[1].Attributes.Locale != "fr-FR" {
		t.Fatalf("app localizations=%+v err=%v", apps, err)
	}
	builds, err := ListBetaBuildLocalizations(context.Background(), c, "B1")
	if err != nil || len(builds) != 1 || builds[0].Attributes.WhatsNew != "Try sync" {
		t.Fatalf("build localizations=%+v err=%v", builds, err)
	}
	detail, err := GetBetaAppReviewDetail(context.Background(), c, "A1")
	if err != nil || detail == nil || detail.Attributes.ContactEmail != "beta@example.com" {
		t.Fatalf("review detail=%+v err=%v", detail, err)
	}
	encoded, err := json.Marshal(detail)
	if err != nil || strings.Contains(string(encoded), "server-secret") {
		t.Fatalf("review detail leaked password: %s err=%v", encoded, err)
	}
}

func TestF5C_BetaReviewDetailRejectsAmbiguousRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"type":"betaAppReviewDetails","id":"R1"},{"type":"betaAppReviewDetails","id":"R2"}]}`)
	}))
	defer server.Close()
	_, err := GetBetaAppReviewDetail(context.Background(), fixtureClient(t, server), "A1")
	if err == nil || !strings.Contains(err.Error(), "multiple beta app review details") {
		t.Fatalf("err=%v", err)
	}
}

func TestF5C_BetaMetadataWritesUseCorrectResourceAndRelationship(t *testing.T) {
	var calls []struct {
		method string
		path   string
		data   map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		data, _ := body["data"].(map[string]any)
		calls = append(calls, struct {
			method string
			path   string
			data   map[string]any
		}{r.Method, r.URL.Path, data})
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":{"type":%q,"id":"R1","attributes":{}}}`, data["type"])
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if _, err := CreateBetaAppLocalization(context.Background(), c, "A1", "en-US", map[string]any{"description": "Beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBetaBuildLocalization(context.Background(), c, "B1", "fr-FR", map[string]any{"whatsNew": "Test sync"}); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateBetaAppReviewDetail(context.Background(), c, "R1", map[string]any{"contactEmail": "beta@example.com"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0].method != "POST" || calls[0].path != "/v1/betaAppLocalizations" || calls[1].path != "/v1/betaBuildLocalizations" || calls[2].method != "PATCH" || calls[2].path != "/v1/betaAppReviewDetails/R1" {
		t.Fatalf("calls=%+v", calls)
	}
	for i, want := range []struct{ name, resource, id string }{{"app", "apps", "A1"}, {"build", "builds", "B1"}} {
		attributes, ok := calls[i].data["attributes"].(map[string]any)
		if !ok || attributes["locale"] == "" {
			t.Fatalf("%s attributes=%+v", want.name, calls[i].data["attributes"])
		}
		rels, _ := calls[i].data["relationships"].(map[string]any)
		ref, _ := rels[want.name].(map[string]any)
		data, _ := ref["data"].(map[string]any)
		if data["type"] != want.resource || data["id"] != want.id {
			t.Fatalf("%s relationship=%+v", want.name, data)
		}
	}
}
