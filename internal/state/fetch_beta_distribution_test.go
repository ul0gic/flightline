package state

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF5C_FetchBetaGroupBuildsPagesAndReconstructsIdentity(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/betaGroups/G1/builds":
			if r.URL.Query().Get("cursor") == "next" {
				_, _ = fmt.Fprint(w, `{"data":[{"type":"builds","id":"B2","attributes":{"version":"43"}}]}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"type":"builds","id":"B1","attributes":{"version":"42"}}],"links":{"next":%q}}`, srv.URL+"/v1/betaGroups/G1/builds?cursor=next")
		case "/v1/builds/B1/preReleaseVersion":
			_, _ = fmt.Fprint(w, `{"data":{"type":"preReleaseVersions","id":"P1","attributes":{"version":"1.2","platform":"IOS"}}}`)
		case "/v1/builds/B2/preReleaseVersion":
			_, _ = fmt.Fprint(w, `{"data":{"type":"preReleaseVersions","id":"P2","attributes":{"version":"1.3","platform":"IOS"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	builds, err := FetchBetaGroupBuilds(context.Background(), fixtureClient(t, srv), "G1")
	if err != nil || len(builds) != 2 || builds[0].Number != "42" || builds[1].Version != "1.3" {
		t.Fatalf("builds=%+v err=%v", builds, err)
	}
}
