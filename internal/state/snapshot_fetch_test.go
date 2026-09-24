package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestC1A_FetchRejectsIncompleteSnapshot(t *testing.T) {
	base := fullCoverageHandler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/appInfos/AINFO1/ageRatingDeclaration" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		base.ServeHTTP(w, r)
	}))
	defer srv.Close()
	state, err := Fetch(context.Background(), fixtureClient(t, srv), "com.example.app", FetchOpts{Version: "1.0"})
	if err == nil || state != nil || !strings.Contains(err.Error(), "age rating") {
		t.Fatalf("Fetch = %v, %v; want contextual error and no partial state", state, err)
	}
}

func TestC1A_OptionalSingletonNullVersusMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(*testing.T, *httptest.Server) error
	}{
		{"version build", func(t *testing.T, srv *httptest.Server) error {
			id, _, err := fetchVersionBuildEncryption(context.Background(), fixtureClient(t, srv), "VER1")
			if id != "" {
				t.Fatalf("build id %q", id)
			}
			return err
		}},
		{"age rating", func(t *testing.T, srv *httptest.Server) error {
			got, err := fetchAgeRating(context.Background(), fixtureClient(t, srv), "AINFO1")
			if got != nil {
				t.Fatalf("age rating %+v", got)
			}
			return err
		}},
		{"review detail", func(t *testing.T, srv *httptest.Server) error {
			got, err := fetchReviewerDemo(context.Background(), fixtureClient(t, srv), "VER1")
			if got != nil {
				t.Fatalf("review detail %+v", got)
			}
			return err
		}},
		{"IAP screenshot", func(t *testing.T, srv *httptest.Server) error {
			got, err := fetchIAPReviewScreenshotProjection(context.Background(), fixtureClient(t, srv), "IAP1")
			if got != nil {
				t.Fatalf("IAP screenshot %+v", got)
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, body := range []string{`{"data":null}`, `{"data":{}}`} {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(body))
				}))
				err := tc.read(t, srv)
				srv.Close()
				if (err != nil) != (body == `{"data":{}}`) {
					t.Fatalf("body %s: err %v", body, err)
				}
			}
		})
	}
}
