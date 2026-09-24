package state

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestF6C_ApplyScreenshotOrderMainUsesFreshCompleteChecksumMembership(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"V1","attributes":{"versionString":"1.0","platform":"IOS"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appStoreVersions/V1/appStoreVersionLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersionLocalizations","id":"L1","attributes":{"locale":"en-US"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appStoreVersionLocalizations/L1/appScreenshotSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appScreenshotSets/S1/appScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"shot-2","attributes":{"fileName":"02.png","sourceFileChecksum":"two"}},{"type":"appScreenshots","id":"shot-1","attributes":{"fileName":"01.png","sourceFileChecksum":"one"}}],"links":{}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshotSets/S1/relationships/appScreenshots":
			patches++
			var body struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PATCH: %v", err)
			}
			if got := []string{body.Data[0].ID, body.Data[1].ID}; !reflect.DeepEqual(got, []string{"shot-1", "shot-2"}) {
				t.Errorf("PATCH IDs = %v", got)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	change := plan.Change{Op: plan.OpUpdate, Path: "/spec/screenshots/locales/en-US/APP_IPHONE_67/order", To: []config.ScreenshotFile{
		{Path: "any-name.png", SourceFileChecksum: "one"}, {Path: "02.png", SourceFileChecksum: "two"},
	}}
	err := applyScreenshotOrderChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, change)
	if err != nil || patches != 1 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}

func TestF6C_ApplyScreenshotOrderCPPUsesExistingEditableVersionOnly(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/APP1/appCustomProductPages":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPages","id":"CPP1","attributes":{"name":"campaign/summer~2026"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appCustomProductPages/CPP1/appCustomProductPageVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPageVersions","id":"CPPV1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appCustomProductPageVersions/CPPV1/appCustomProductPageLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPageLocalizations","id":"CPPL1","attributes":{"locale":"en-US"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appCustomProductPageLocalizations/CPPL1/appScreenshotSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshotSets","id":"S1","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}}],"links":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appScreenshotSets/S1/appScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"shot-2","attributes":{"fileName":"02.png","sourceFileChecksum":"two"}},{"type":"appScreenshots","id":"shot-1","attributes":{"fileName":"01.png","sourceFileChecksum":"one"}}],"links":{}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshotSets/S1/relationships/appScreenshots":
			patches++
			var body struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PATCH: %v", err)
			}
			if got := []string{body.Data[0].ID, body.Data[1].ID}; !reflect.DeepEqual(got, []string{"shot-1", "shot-2"}) {
				t.Errorf("CPP PATCH IDs = %v", got)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	checksum := "checksum:one"
	change := plan.Change{Op: plan.OpUpdate, Path: "/spec/customProductPages/campaign~1summer~02026/localizations/en-US/screenshots/APP_IPHONE_67/order", To: []config.ScreenshotFile{
		{Path: "01.png", Alt: &checksum}, {Path: "02.png", SourceFileChecksum: "two"},
	}}
	parent := plan.Change{Op: plan.OpUpdate, Path: "/spec/customProductPages/campaign~1summer~02026/localizations/en-US/screenshots/APP_IPHONE_67"}
	if !changeDependsOn(change, parent) {
		t.Fatal("CPP order change did not depend on its escaped screenshot collection path")
	}
	err := applyScreenshotOrderChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app"}, change)
	if err != nil || patches != 1 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}

func TestF6C_ScreenshotOrderRejectsAmbiguousAndForeignMembership(t *testing.T) {
	files := []config.ScreenshotFile{{Path: "same.png"}, {Path: "other.png"}}
	ambiguous := []asc.AppScreenshot{{ID: "one", FileName: "same.png"}, {ID: "two", FileName: "same.png"}}
	if _, err := screenshotOrderMemberIDs(files, ambiguous); err == nil {
		t.Fatal("ambiguous filename membership accepted")
	}
	foreign := []asc.AppScreenshot{{ID: "one", FileName: "same.png"}, {ID: "two", FileName: "other.png"}}
	if _, err := screenshotOrderMemberIDs([]config.ScreenshotFile{{Path: "same.png", SourceFileChecksum: "missing"}, {Path: "other.png"}}, foreign); err == nil {
		t.Fatal("foreign checksum membership accepted")
	}
}
