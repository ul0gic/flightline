package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBUG077_CPPCLIReadsEveryVersionPage(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		t.Run(map[bool]string{false: "numeric latest", true: "late failure"}[failSecond], func(t *testing.T) {
			srv := httptest.NewServer(cppVersionPagesHandler(t, failSecond))
			defer srv.Close()
			version, state, err := fetchCurrentCustomProductPageVersion(context.Background(), fixtureASCClient(t, srv), "P")
			if failSecond {
				if err == nil {
					t.Fatal("incomplete read accepted")
				}
				return
			}
			if err != nil || version != "10" || state != "APPROVED" {
				t.Fatalf("version=%s state=%s err=%v", version, state, err)
			}
		})
	}
}

func TestBUG079_CPPAppMembershipAndNameReadCompleteCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"id":"owned","type":"appCustomProductPages","attributes":{"name":"Campaign"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"links":{"next":"http://` + r.Host + `/v1/apps/A/appCustomProductPages?page=2"}}`))
	}))
	defer srv.Close()
	c := fixtureASCClient(t, srv)
	if err := requireCPPAppMembership(context.Background(), c, "A", "owned"); err != nil {
		t.Fatal(err)
	}
	if err := requireCPPAppMembership(context.Background(), c, "A", "foreign"); err == nil {
		t.Fatal("foreign CPP accepted")
	}
	row, err := findCustomProductPageByName(context.Background(), c, "A", "Campaign")
	if err != nil || row == nil || row.ID != "owned" {
		t.Fatalf("later-page CPP was not reused: row=%+v err=%v", row, err)
	}
}

func cppVersionPagesHandler(t *testing.T, failSecond bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			if failSecond {
				http.Error(w, "late failure", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"ten","type":"appCustomProductPageVersions","attributes":{"version":"10","state":"APPROVED"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"nine","type":"appCustomProductPageVersions","attributes":{"version":"9","state":"REPLACED_WITH_NEW_VERSION"}}],"links":{"next":"http://` + r.Host + `/v1/appCustomProductPages/P/versions?page=2"}}`))
	})
}
