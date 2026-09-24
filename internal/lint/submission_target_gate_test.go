package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestG8_PreflightUsesExactDraftAcrossPages(t *testing.T) {
	for _, selected := range []string{"target", "foreign"} {
		t.Run(selected, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/v1/apps":
					_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
				case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
					_, _ = w.Write([]byte(`{"data":[{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"sku","state":"READY_TO_SUBMIT"}}]}`))
				case r.URL.Path == "/v1/reviewSubmissions" && r.URL.Query().Get("cursor") == "next":
					_, _ = w.Write([]byte(`{"data":[{"id":"target","type":"reviewSubmissions","attributes":{"state":"READY_FOR_REVIEW"}}]}`))
				case r.URL.Path == "/v1/reviewSubmissions":
					_, _ = w.Write([]byte(`{"data":[{"id":"other","type":"reviewSubmissions","attributes":{"state":"IN_REVIEW"}}],"links":{"next":"` + srv.URL + `/v1/reviewSubmissions?cursor=next"}}`))
				case r.URL.Path == "/v1/reviewSubmissions/target/items":
					_, _ = w.Write([]byte(`{"data":[]}`))
				case r.URL.Path == "/v2/inAppPurchases/iap-A/versions":
					_, _ = w.Write([]byte(`{"data":[]}`))
				default:
					t.Errorf("unexpected request: %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			got := (iapAttachedToReviewSubmissionRule{}).Check(CheckContext{Ctx: context.Background(), Client: newTestClient(t, srv), BundleID: "com.example.x", Live: true, ReviewSubmissionID: selected})
			if len(got) != 1 || got[0].Severity != SeverityError {
				t.Fatalf("expected blocking diagnostic: %+v", got)
			}
			if selected == "foreign" && !strings.Contains(got[0].Message, "0 matches") {
				t.Fatalf("foreign draft not blocked: %+v", got)
			}
			if selected == "target" && !strings.Contains(got[0].Message, "target") {
				t.Fatalf("wrong draft checked: %+v", got)
			}
		})
	}
}
