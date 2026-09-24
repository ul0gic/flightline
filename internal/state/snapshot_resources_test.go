package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestC1A_NestedReadErrors(t *testing.T) {
	tests := []struct {
		name, path string
		status     int
		body       string
		run        func(*testing.T, *httptest.Server) error
	}{
		{"metadata second source", "/v1/appInfos/AINFO1/appInfoLocalizations", http.StatusForbidden, `{"errors":[]}`, func(t *testing.T, srv *httptest.Server) error {
			_, e := fetchMetadataLocales(context.Background(), fixtureClient(t, srv), "VER1", "AINFO1")
			return e
		}},
		{"IAP localization", "/v2/inAppPurchases/IAP1/inAppPurchaseLocalizations", http.StatusTooManyRequests, `{"errors":[]}`, func(t *testing.T, srv *httptest.Server) error {
			_, e := fetchIAPs(context.Background(), fixtureClient(t, srv), "APP1")
			return e
		}},
		{"group testers", "/v1/betaGroups/BG1/betaTesters", http.StatusInternalServerError, `{"errors":[]}`, func(t *testing.T, srv *httptest.Server) error {
			_, e := fetchTestFlightGroups(context.Background(), fixtureClient(t, srv), "APP1")
			return e
		}},
		{"screenshots decode", "/v1/appScreenshotSets/SS1/appScreenshots", http.StatusOK, `{broken`, func(t *testing.T, srv *httptest.Server) error {
			_, e := fetchScreenshots(context.Background(), fixtureClient(t, srv), "VER1")
			return e
		}},
		{"CPP localization", "/v1/appCustomProductPageVersions/CPPV1/appCustomProductPageLocalizations", http.StatusForbidden, `{"errors":[]}`, func(t *testing.T, srv *httptest.Server) error {
			_, e := fetchCustomProductPages(context.Background(), fixtureClient(t, srv), "APP1")
			return e
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := fullCoverageHandler(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tt.path {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
					return
				}
				base.ServeHTTP(w, r)
			}))
			defer srv.Close()
			if err := tt.run(t, srv); err == nil {
				t.Fatal("partial snapshot accepted")
			}
		})
	}
}

func TestC1A_Optional404Narrow(t *testing.T) {
	base := fullCoverageHandler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/appStoreVersions/VER1/appStoreReviewDetail" || r.URL.Path == "/v2/inAppPurchases/IAP1/appStoreReviewScreenshot" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		base.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if got, err := fetchReviewerDemo(context.Background(), c, "VER1"); err != nil || got != nil {
		t.Fatalf("review detail = %v, %v", got, err)
	}
	if got, err := fetchIAPReviewScreenshotProjection(context.Background(), c, "IAP1"); err != nil || got != nil {
		t.Fatalf("IAP screenshot = %v, %v", got, err)
	}
}

func TestC1A_PaginatedCollections(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/betaGroups/G1/betaTesters":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"data":[{"id":"T2","attributes":{"email":"two@example.com"}}],"links":{}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"T1","attributes":{"email":"one@example.com"}}],"links":{"next":"` + serverURL + `/v1/betaGroups/G1/betaTesters?page=2"}}`))
		case "/v2/inAppPurchases/I1/inAppPurchaseLocalizations":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"data":[{"id":"L2","attributes":{"locale":"fr-FR","name":"Deux"}}],"links":{}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"L1","attributes":{"locale":"en-US","name":"One"}}],"links":{"next":"` + serverURL + `/v2/inAppPurchases/I1/inAppPurchaseLocalizations?page=2"}}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	c := fixtureClient(t, srv)
	testers, err := fetchGroupTesters(context.Background(), c, "G1")
	if err != nil || len(testers) != 2 {
		t.Fatalf("testers = %v, %v", testers, err)
	}
	locs, err := fetchIAPLocalizations(context.Background(), c, "I1")
	if err != nil || len(locs) != 2 {
		t.Fatalf("localizations = %v, %v", locs, err)
	}
}

func TestC1A_LaterPageFailure(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "page=2") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"T1","attributes":{"email":"one@example.com"}}],"links":{"next":"` + serverURL + `/v1/betaGroups/G1/betaTesters?page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	if got, err := fetchGroupTesters(context.Background(), fixtureClient(t, srv), "G1"); err == nil || got != nil {
		t.Fatalf("accepted partial roster: %v, %v", got, err)
	}
}

func TestC1A_PaginatedIAPAndGroups(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps/APP1/inAppPurchasesV2" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"id":"I2","attributes":{"productId":"second","inAppPurchaseType":"CONSUMABLE"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/inAppPurchasesV2":
			_, _ = w.Write([]byte(`{"data":[{"id":"I1","attributes":{"productId":"first","inAppPurchaseType":"CONSUMABLE"}}],"links":{"next":"` + serverURL + `/v1/apps/APP1/inAppPurchasesV2?page=2"}}`))
		case r.URL.Path == "/v1/apps/APP1/betaGroups" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"data":[{"id":"G2","attributes":{"name":"second"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"id":"G1","attributes":{"name":"first"}}],"links":{"next":"` + serverURL + `/v1/apps/APP1/betaGroups?page=2"}}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreReviewScreenshot"), strings.HasSuffix(r.URL.Path, "/iapPriceSchedule"), strings.HasSuffix(r.URL.Path, "/inAppPurchaseAvailability"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/inAppPurchaseLocalizations"), strings.HasSuffix(r.URL.Path, "/betaTesters"):
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL
	c := fixtureClient(t, srv)
	iaps, err := fetchIAPs(context.Background(), c, "APP1")
	if err != nil || len(iaps.Products) != 2 {
		t.Fatalf("IAPs = %v, %v", iaps, err)
	}
	groups, err := fetchTestFlightGroups(context.Background(), c, "APP1")
	if err != nil || len(groups.Groups) != 2 {
		t.Fatalf("groups = %v, %v", groups, err)
	}
}
