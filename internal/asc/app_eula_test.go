package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestF6B_ReadAppEULAPagesAndSortsTerritories(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/A1/endUserLicenseAgreement":
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"Legal text"}}}`)
		case "/v1/endUserLicenseAgreements/E1/territories":
			if r.URL.Query().Get("cursor") == "next" {
				_, _ = fmt.Fprint(w, `{"data":[{"type":"territories","id":"USA"}]}`)
				return
			}
			if r.URL.Query().Get("limit") != "200" {
				t.Errorf("query=%v", r.URL.Query())
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"territories","id":"GBR"}],"links":{"next":%q}}`, srv.URL+"/v1/endUserLicenseAgreements/E1/territories?cursor=next")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	got, err := ReadAppEULA(context.Background(), fixtureClient(t, srv), "A1")
	if err != nil || got == nil || got.ID != "E1" || got.AgreementText != "Legal text" || !reflect.DeepEqual(got.Territories, []string{"GBR", "USA"}) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestF6B_ReadAppEULANullAndMalformed(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantError  bool
	}{
		{name: "absent", body: `{"data":null}`},
		{name: "missing text", body: `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{}}}`, wantError: true},
		{name: "wrong type", body: `{"data":{"type":"apps","id":"E1","attributes":{"agreementText":"text"}}}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer srv.Close()
			got, err := ReadAppEULA(context.Background(), fixtureClient(t, srv), "A1")
			if (err != nil) != test.wantError || !test.wantError && got != nil {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

func TestF6B_ReadAppEULARejectsDuplicateTerritory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "endUserLicenseAgreement") {
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"text"}}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"type":"territories","id":"USA"},{"type":"territories","id":"USA"}]}`)
	}))
	defer srv.Close()
	_, err := ReadAppEULA(context.Background(), fixtureClient(t, srv), "A1")
	if err == nil || !strings.Contains(err.Error(), "duplicate territory") {
		t.Fatalf("err=%v", err)
	}
}

func TestF6B_AppEULAWritesUseAppAndTerritoryRelationships(t *testing.T) {
	var calls []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls = append(calls, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"Terms"}}}`)
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if _, err := CreateAppEULA(context.Background(), c, "A1", "Terms", []string{"USA"}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"USA", "GBR"}
	if err := PatchAppEULA(context.Background(), c, "E1", nil, &ids); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
	create, _ := calls[0]["data"].(map[string]any)
	rels, _ := create["relationships"].(map[string]any)
	app, _ := rels["app"].(map[string]any)
	appRef, _ := app["data"].(map[string]any)
	if appRef["type"] != "apps" || appRef["id"] != "A1" {
		t.Fatalf("create=%v", calls[0])
	}
	patch, _ := calls[1]["data"].(map[string]any)
	if _, exists := patch["attributes"]; exists {
		t.Fatalf("patch unexpectedly changed text: %v", calls[1])
	}
	patchRels, _ := patch["relationships"].(map[string]any)
	territories, _ := patchRels["territories"].(map[string]any)
	refs, _ := territories["data"].([]any)
	if len(refs) != 2 {
		t.Fatalf("patch territory refs=%v", calls[1])
	}
}
