package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG6_AssetOrderApplyFetchDiffConvergesWithoutUploads(t *testing.T) {
	mainIDs := []string{"SH2", "SH1"}
	cppIDs := []string{"CPPSH2", "CPPSH1"}
	patches := map[string]int{}
	reservations := 0
	base := fullCoverageHandler(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps/APP1/appCustomProductPages":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPages","id":"CPP1","attributes":{"name":"campaign/~2026","visible":true}}],"links":{}}`))
		case "/v1/appCustomProductPages/CPP1/appCustomProductPageVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPageVersions","id":"CPPV1","attributes":{"state":"PREPARE_FOR_SUBMISSION","version":"1"}}],"links":{}}`))
		case "/v1/appScreenshotSets/SS1/appScreenshots", "/v1/appScreenshotSets/SS1/relationships/appScreenshots":
			if r.Method == http.MethodPatch {
				mainIDs = decodeOrderLinkage(t, r)
				patches["main"]++
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeOrderAssets(w, mainIDs, "SH", "shot", []string{"aaa", "bbb"})
		case "/v1/appScreenshotSets/CPPSS1/appScreenshots", "/v1/appScreenshotSets/CPPSS1/relationships/appScreenshots":
			if r.Method == http.MethodPatch {
				cppIDs = decodeOrderLinkage(t, r)
				patches["cpp"]++
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeOrderAssets(w, cppIDs, "CPPSH", "cpp", []string{"ccc", "ddd"})
		case "/v1/appScreenshots", "/v1/appPreviews":
			reservations++
			http.Error(w, "uploads are forbidden in reorder convergence", http.StatusInternalServerError)
		default:
			base.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()

	client := fixtureClient(t, srv)
	live, err := Fetch(context.Background(), client, "com.example.app", FetchOpts{Version: "1.0"})
	if err != nil {
		t.Fatalf("initial Fetch: %v", err)
	}
	desired := reorderedAssetState(t, live)
	changes := plan.Diff(desired, live)
	if len(changes) != 2 {
		t.Fatalf("initial changes = %#v", changes)
	}
	for _, change := range changes {
		if !strings.HasSuffix(change.Path, "/order") || change.Op != plan.OpUpdate {
			t.Fatalf("expected only order updates, got %#v", changes)
		}
	}

	result, err := Apply(context.Background(), client, changes, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Confirm: true,
	})
	if err != nil || result == nil || len(result.Applied) != 2 || patches["main"] != 1 || patches["cpp"] != 1 || reservations != 0 {
		t.Fatalf("apply result=%+v err=%v patches=%v reservations=%d", result, err, patches, reservations)
	}

	after, err := Fetch(context.Background(), client, "com.example.app", FetchOpts{Version: "1.0"})
	if err != nil {
		t.Fatalf("post-apply Fetch: %v", err)
	}
	if residual := plan.Diff(desired, after); len(residual) != 0 {
		t.Fatalf("post-apply residual = %#v", residual)
	}
	second, err := Apply(context.Background(), client, nil, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Confirm: true,
	})
	if err != nil || second == nil || len(second.Applied) != 0 || patches["main"] != 1 || patches["cpp"] != 1 || reservations != 0 {
		t.Fatalf("second apply=%+v err=%v patches=%v reservations=%d", second, err, patches, reservations)
	}
}

func reorderedAssetState(t *testing.T, live *config.State) *config.State {
	t.Helper()
	ordered := true
	if live.Spec.Screenshots == nil || live.Spec.CustomProductPages == nil {
		t.Fatal("fixture did not return main and CPP screenshots")
	}
	main := reverseScreenshotFiles(live.Spec.Screenshots.Locales["en-US"]["APP_IPHONE_69"])
	pageName := "campaign/~2026"
	page := (*live.Spec.CustomProductPages)[pageName]
	localization := page.Localizations["en-US"]
	localization.Screenshots = map[string][]config.ScreenshotFile{
		"APP_IPHONE_69": reverseScreenshotFiles(localization.Screenshots["APP_IPHONE_69"]),
	}
	localization.ScreenshotOrder = &ordered
	page.Localizations = map[string]config.CustomProductPageLocale{"en-US": localization}
	pages := config.CustomProductPagesSpec{pageName: page}
	return &config.State{
		APIVersion: live.APIVersion,
		Kind:       live.Kind,
		Metadata:   live.Metadata,
		Spec: config.StateSpec{
			ContentRights: live.Spec.ContentRights,
			AppEULA:       live.Spec.AppEULA,
			Previews:      live.Spec.Previews,
			Screenshots: &config.ScreenshotsSpec{
				Order:   &ordered,
				Locales: map[string]map[string][]config.ScreenshotFile{"en-US": {"APP_IPHONE_69": main}},
			},
			CustomProductPages: &pages,
		},
	}
}

func reverseScreenshotFiles(files []config.ScreenshotFile) []config.ScreenshotFile {
	out := make([]config.ScreenshotFile, len(files))
	for index := range files {
		out[len(files)-1-index] = files[index]
	}
	return out
}

func decodeOrderLinkage(t *testing.T, r *http.Request) []string {
	t.Helper()
	var body struct {
		Data []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode order linkage: %v", err)
	}
	ids := make([]string, 0, len(body.Data))
	for _, linkage := range body.Data {
		if linkage.Type != "appScreenshots" || linkage.ID == "" {
			t.Fatalf("invalid order linkage: %#v", linkage)
		}
		ids = append(ids, linkage.ID)
	}
	return ids
}

func writeOrderAssets(w http.ResponseWriter, ids []string, idPrefix, filePrefix string, checksums []string) {
	indexByID := map[string]int{idPrefix + "1": 0, idPrefix + "2": 1}
	resources := make([]string, 0, len(ids))
	for _, id := range ids {
		index, ok := indexByID[id]
		if !ok {
			http.Error(w, "unknown screenshot ID", http.StatusBadRequest)
			return
		}
		resources = append(resources, fmt.Sprintf(`{"type":"appScreenshots","id":%q,"attributes":{"fileName":%q,"sourceFileChecksum":%q}}`, id, fmt.Sprintf("%s%d.png", filePrefix, index+1), checksums[index]))
	}
	_, _ = w.Write([]byte(`{"data":[` + strings.Join(resources, ",") + `],"links":{}}`))
}
