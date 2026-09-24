package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestG6_InvalidPreviewPreventsPrecedingRightsWrite(t *testing.T) {
	withTempCacheDir(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "writes must not begin", http.StatusInternalServerError)
	}))
	defer srv.Close()

	changes := []plan.Change{
		{Op: plan.OpUpdate, Path: "/spec/contentRights", To: asc.ContentRightsUsesThirdParty},
		{Op: plan.OpUpdate, Path: "/spec/previews/locales/en-US/IPHONE_67", To: []config.PreviewFile{{}}},
	}
	result, err := Apply(context.Background(), fixtureClient(t, srv), changes, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Confirm: true,
	})
	if err == nil || result == nil || len(result.Errors) != 1 || requests.Load() != 0 {
		t.Fatalf("result=%+v err=%v requests=%d", result, err, requests.Load())
	}
}

func TestG6_ScreenshotOrderSkipsAfterMatchingCollectionFailure(t *testing.T) {
	withTempCacheDir(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "collection resolution failed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	collection := plan.Change{
		Op: plan.OpUpdate, Path: "/spec/screenshots/locales/en-US/APP_IPHONE_67",
		To: []config.ScreenshotFile{{Path: "01.png"}},
	}
	order := plan.Change{
		Op: plan.OpUpdate, Path: "/spec/screenshots/locales/en-US/APP_IPHONE_67/order",
		To: []config.ScreenshotFile{{Path: "01.png"}},
	}
	result, err := Apply(context.Background(), fixtureClient(t, srv), []plan.Change{order, collection}, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Confirm: true,
	})
	if err == nil || result == nil || len(result.Errors) != 1 || len(result.Skipped) != 1 || requests.Load() != 1 {
		t.Fatalf("result=%+v err=%v requests=%d", result, err, requests.Load())
	}
	if result.Skipped[0].Path != order.Path {
		t.Fatalf("skipped=%+v", result.Skipped)
	}
}

func TestG6_CPPPreviewChildDependsOnNewPage(t *testing.T) {
	parent := plan.Change{Op: plan.OpCreate, Path: "/spec/customProductPages/campaign~1summer~02026"}
	child := plan.Change{Op: plan.OpCreate, Path: "/spec/customProductPages/campaign~1summer~02026/localizations/en-US/previews/IPHONE_67"}
	if !changeDependsOn(child, parent) {
		t.Fatal("CPP preview child did not remain a separate parent-dependent apply change")
	}
}
