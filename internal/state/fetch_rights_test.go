package state

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF6B_FetchRightsProjectsCompleteObservedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/A1":
			_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1","attributes":{"contentRightsDeclaration":"USES_THIRD_PARTY_CONTENT"}}}`)
		case "/v1/apps/A1/endUserLicenseAgreement":
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"Terms"}}}`)
		case "/v1/endUserLicenseAgreements/E1/territories":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"territories","id":"USA"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	rights, err := FetchContentRights(context.Background(), c, "A1")
	if err != nil || rights == nil || *rights != asc.ContentRightsUsesThirdParty {
		t.Fatalf("rights=%v err=%v", rights, err)
	}
	eula, err := FetchAppEULA(context.Background(), c, "A1")
	if err != nil || eula == nil || eula.AgreementText == nil || *eula.AgreementText != "Terms" || eula.Territories == nil || len(*eula.Territories) != 1 {
		t.Fatalf("eula=%+v err=%v", eula, err)
	}
}
