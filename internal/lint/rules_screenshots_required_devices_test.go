package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestScreenshotsRequiredDevices_OfflineMissingFires(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Screenshots: &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{
			"en-US": {
				"APP_IPAD_PRO_3GEN_129": {{Path: "shot.png"}}, // no large-iPhone tier at all
			},
		}},
	}}
	got := screenshotsRequiredDevicesRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].Path != "/spec/screenshots/locales/en-US" {
		t.Errorf("path = %q", got[0].Path)
	}
}

func TestScreenshotsRequiredDevices_OfflineCompleteNoOp(t *testing.T) {
	s := &config.State{Spec: config.StateSpec{
		Screenshots: &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{
			"en-US": {
				"APP_IPHONE_65": {{Path: "a.png"}}, // 6.5-only satisfies the tier (Apple scales down)
			},
		}},
	}}
	got := screenshotsRequiredDevicesRule{}.Check(CheckContext{Ctx: context.Background(), State: s})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0: %+v", len(got), got)
	}
}

func TestScreenshotsRequiredDevices_OfflineUnmanagedNoOp(t *testing.T) {
	got := screenshotsRequiredDevicesRule{}.Check(CheckContext{
		Ctx:   context.Background(),
		State: &config.State{Spec: config.StateSpec{
			// Screenshots == nil: unmanaged, no flag.
		}},
	})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 unmanaged: %+v", len(got), got)
	}
}

func TestScreenshotsRequiredDevices_LiveMissingFires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ver-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersionLocalizations"):
			_, _ = w.Write([]byte(`{"data":[{"id":"loc-1","type":"appStoreVersionLocalizations","attributes":{"locale":"en-US"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appScreenshotSets"):
			// only an iPad set: no large-iPhone tier member
			_, _ = w.Write([]byte(`{"data":[{"id":"set-1","type":"appScreenshotSets","attributes":{"screenshotDisplayType":"APP_IPAD_PRO_3GEN_129"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	got := screenshotsRequiredDevicesRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Version: "1.0.1", Live: true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "APP_IPHONE_69") {
		t.Errorf("message should list accepted types: %q", got[0].Message)
	}
}
