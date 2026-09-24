package state

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF5C_FetchBetaMetadataScopesBuildsAndOmitsPassword(t *testing.T) {
	buildReads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/A1/betaAppLocalizations":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"betaAppLocalizations","id":"L1","attributes":{"locale":"en-US","description":"Beta"}}]}`)
		case "/v1/betaAppReviewDetails":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"betaAppReviewDetails","id":"R1","attributes":{"contactEmail":"beta@example.com","demoAccountPassword":"secret"}}]}`)
		case "/v1/builds":
			buildReads++
			if r.URL.Query().Get("filter[app]") != "A1" || r.URL.Query().Get("filter[version]") != "42" || r.URL.Query().Get("filter[preReleaseVersion.version]") != "1.2" {
				t.Errorf("build lookup query=%v", r.URL.Query())
			}
			_, _ = fmt.Fprint(w, `{"data":[{"type":"builds","id":"B1","attributes":{"version":"42"}}]}`)
		case "/v1/builds/B1/preReleaseVersion":
			_, _ = fmt.Fprint(w, `{"data":{"type":"preReleaseVersions","id":"P1","attributes":{"version":"1.2","platform":"IOS"}}}`)
		case "/v1/builds/B1/betaBuildLocalizations":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"betaBuildLocalizations","id":"BL1","attributes":{"locale":"fr-FR","whatsNew":"Try sync"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	appOnly, err := FetchBetaMetadata(context.Background(), c, "A1", nil)
	if err != nil || appOnly == nil || buildReads != 0 || len(appOnly.Builds) != 0 {
		t.Fatalf("app-only metadata=%+v buildReads=%d err=%v", appOnly, buildReads, err)
	}
	selector := config.BetaBuildSelector{Number: "42", Version: "1.2", Platform: "IOS"}
	selected, err := FetchBetaMetadata(context.Background(), c, "A1", []config.BetaBuildSelector{selector})
	if err != nil || selected == nil || buildReads != 1 || len(selected.Builds) != 1 || *selected.Builds[0].Localizations["fr-FR"].WhatsNew != "Try sync" {
		t.Fatalf("selected metadata=%+v buildReads=%d err=%v", selected, buildReads, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", selected), "secret") {
		t.Fatalf("password leaked in metadata: %+v", selected)
	}
}

func TestF5C_ResolveBetaBuildSelectorRejectsMismatchedIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/builds" {
			_, _ = fmt.Fprint(w, `{"data":[{"type":"builds","id":"B1","attributes":{"version":"42"}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"type":"preReleaseVersions","id":"P1","attributes":{"version":"1.1","platform":"IOS"}}}`)
	}))
	defer srv.Close()
	_, err := resolveBetaBuildSelector(context.Background(), fixtureClient(t, srv), "A1", config.BetaBuildSelector{Number: "42", Version: "1.2", Platform: "IOS"})
	if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("err=%v", err)
	}
}
