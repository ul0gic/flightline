package state

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestBUG076_AssetTargetOnSecondPage(t *testing.T) {
	cases := []struct {
		name, resource string
		attrs          map[string]any
		resolve        func(context.Context, *asc.Client) (string, error)
	}{
		{"main locale", "appStoreVersionLocalizations", map[string]any{"locale": "en-US"}, func(ctx context.Context, c *asc.Client) (string, error) {
			return resolveVersionLocalizationID(ctx, c, "V1", "en-US")
		}},
		{"screenshot set", "appScreenshotSets", map[string]any{"screenshotDisplayType": "APP_IPHONE_67"}, func(ctx context.Context, c *asc.Client) (string, error) {
			return findOrCreateManagedScreenshotSet(ctx, c, "L1", "appStoreVersionLocalization", "appStoreVersionLocalizations", "APP_IPHONE_67")
		}},
		{"CPP version", "appCustomProductPageVersions", map[string]any{"state": "PREPARE_FOR_SUBMISSION"}, func(ctx context.Context, c *asc.Client) (string, error) {
			return ensureEditableCPPVersion(ctx, c, "P1")
		}},
		{"CPP locale", "appCustomProductPageLocalizations", map[string]any{"locale": "en-US"}, func(ctx context.Context, c *asc.Client) (string, error) {
			return ensureCPPLocalization(ctx, c, "PV1", "en-US", nil)
		}},
		{"CPP name", "appCustomProductPages", map[string]any{"name": "campaign"}, func(ctx context.Context, c *asc.Client) (string, error) {
			return resolveCustomProductPage(ctx, c, "A1", "campaign")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
					http.Error(w, "unexpected mutation", http.StatusInternalServerError)
					return
				}
				if r.URL.Query().Has("filter[name]") {
					t.Error("unsupported CPP name filter")
				}
				response := map[string]any{"data": []any{}, "links": map[string]string{"next": "http://" + r.Host + r.URL.Path + "?page=2"}}
				if r.URL.Query().Get("page") == "2" {
					response = map[string]any{"data": []any{map[string]any{"id": "MATCH", "type": tc.resource, "attributes": tc.attrs}}}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer srv.Close()
			id, err := tc.resolve(context.Background(), fixtureClient(t, srv))
			if err != nil || id != "MATCH" || writes.Load() != 0 {
				t.Fatalf("id=%s err=%v writes=%d", id, err, writes.Load())
			}
		})
	}
}

func TestBUG076_IncompleteOrAmbiguousLookupNeverCreates(t *testing.T) {
	for _, failure := range []string{"late error", "ambiguous"} {
		t.Run(failure, func(t *testing.T) {
			var writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
					http.Error(w, "unexpected mutation", http.StatusInternalServerError)
					return
				}
				if r.URL.Query().Get("page") == "2" && failure == "late error" {
					http.Error(w, "read failed", http.StatusInternalServerError)
					return
				}
				response := map[string]any{"data": []any{map[string]any{"id": "V1", "type": "appCustomProductPageVersions", "attributes": map[string]string{"state": "PREPARE_FOR_SUBMISSION"}}}}
				if r.URL.Query().Get("page") != "2" {
					response["links"] = map[string]string{"next": "http://" + r.Host + r.URL.Path + "?page=2"}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer srv.Close()
			if _, err := ensureEditableCPPVersion(context.Background(), fixtureClient(t, srv), "P1"); err == nil {
				t.Fatal("accepted incomplete or ambiguous lookup")
			}
			if writes.Load() != 0 {
				t.Fatalf("writes=%d", writes.Load())
			}
		})
	}
}
