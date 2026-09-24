package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF5C_BetaGroupBuildLinkageUsesAddAndDeleteBodies(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/betaGroups/G1/relationships/builds" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Data []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if len(body.Data) != 1 || body.Data[0].Type != "builds" || body.Data[0].ID != "B1" {
			t.Errorf("body=%+v", body)
		}
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if err := AddBetaGroupBuild(context.Background(), c, "G1", "B1"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBetaGroupBuild(context.Background(), c, "G1", "B1"); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(methods) != "[POST DELETE]" {
		t.Fatalf("methods=%v", methods)
	}
}
