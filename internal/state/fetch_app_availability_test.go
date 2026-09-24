package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF5B_FetchAppAvailabilityPreservesObservedStateWithoutASCIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP-1/appAvailabilityV2":
			_, _ = w.Write([]byte(`{"data":{"type":"appAvailabilities","id":"AVAIL-1","attributes":{"availableInNewTerritories":false}}}`))
		case "/v2/appAvailabilities/AVAIL-1/territoryAvailabilities":
			_, _ = w.Write([]byte(`{"data":[{"type":"territoryAvailabilities","id":"TA-GBR","attributes":{"available":true,"releaseDate":"2027-02-03","preOrderEnabled":true,"preOrderPublishDate":"2026-12-03","contentStatuses":["AVAILABLE_FOR_PREORDER"]},"relationships":{"territory":{"data":{"type":"territories","id":"GBR"}}}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	spec, err := FetchAppAvailability(context.Background(), fixtureClient(t, srv), "APP-1")
	if err != nil {
		t.Fatalf("FetchAppAvailability: %v", err)
	}
	gb := spec.Territories["GBR"]
	if spec.AvailableInNewTerritories == nil || *spec.AvailableInNewTerritories || gb.Available == nil || !*gb.Available || gb.ReleaseDate == nil || *gb.ReleaseDate != "2027-02-03" || gb.ContentStatuses[0] != "AVAILABLE_FOR_PREORDER" {
		t.Fatalf("spec = %#v", spec)
	}
}

func TestF5B_FetchAppAvailabilityTreatsOnly404AsAbsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantNil bool
	}{
		{name: "absent", status: http.StatusNotFound, wantNil: true},
		{name: "server failure", status: http.StatusInternalServerError},
		{name: "null data", status: http.StatusOK, body: `{"data":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			spec, err := FetchAppAvailability(context.Background(), fixtureClient(t, srv), "APP-1")
			if tc.wantNil {
				if err != nil || spec != nil {
					t.Fatalf("FetchAppAvailability = (%#v, %v), want (nil, nil)", spec, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("FetchAppAvailability = (%#v, nil), want error", spec)
			}
		})
	}
}
