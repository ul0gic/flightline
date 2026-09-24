package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF7A_PhasedReleaseAndManualReleaseWireContracts(t *testing.T) {
	var creates, patches, releases int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/appStoreVersionPhasedReleases":
			creates++
			var body struct {
				Data struct {
					Type       string `json:"type"`
					Attributes struct {
						State string `json:"phasedReleaseState"`
					} `json:"attributes"`
					Relationships map[string]struct {
						Data struct {
							Type string `json:"type"`
							ID   string `json:"id"`
						} `json:"data"`
					} `json:"relationships"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create: %v", err)
			} else if body.Data.Type != "appStoreVersionPhasedReleases" || body.Data.Attributes.State != "INACTIVE" || body.Data.Relationships["appStoreVersion"].Data.Type != "appStoreVersions" || body.Data.Relationships["appStoreVersion"].Data.ID != "V1" {
				t.Errorf("create body=%+v", body.Data)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionPhasedReleases","id":"PR1","attributes":{"phasedReleaseState":"INACTIVE"}}}`))
		case "/v1/appStoreVersionPhasedReleases/PR1":
			patches++
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionPhasedReleases","id":"PR1","attributes":{"phasedReleaseState":"PAUSED"}}}`))
		case "/v1/appStoreVersionReleaseRequests":
			releases++
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionReleaseRequests","id":"RR1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)
	created, err := EnableAppStoreVersionPhasedRelease(context.Background(), c, "V1")
	if err != nil || created.ID != "PR1" || created.PhasedReleaseState != "INACTIVE" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	paused, err := PauseAppStoreVersionPhasedRelease(context.Background(), c, "PR1")
	if err != nil || paused.PhasedReleaseState != "PAUSED" {
		t.Fatalf("paused=%+v err=%v", paused, err)
	}
	request, err := CreateAppStoreVersionReleaseRequest(context.Background(), c, "V1")
	if err != nil || request.ID != "RR1" || creates != 1 || patches != 1 || releases != 1 {
		t.Fatalf("request=%+v err=%v counts=%d/%d/%d", request, err, creates, patches, releases)
	}
}

func TestF7A_ReadPhasedReleaseRejectsInvalidState(t *testing.T) {
	for _, state := range []string{"", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionPhasedReleases","id":"PR1","attributes":{"phasedReleaseState":"` + state + `"}}}`))
			}))
			t.Cleanup(srv.Close)
			if _, err := ReadAppStoreVersionPhasedRelease(context.Background(), fixtureClient(t, srv), "V1"); err == nil {
				t.Fatal("invalid state accepted")
			}
		})
	}
}
