package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/config"
)

func TestG7_PhasedIntentCannotAuthorizeReleasedMetadataWrite(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"A","type":"apps"}]}`))
		case "/v1/apps/A":
			_, _ = w.Write([]byte(`{"data":{"id":"A","type":"apps","attributes":{}}}`))
		case "/v1/apps/A/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"id":"V","type":"appStoreVersions","attributes":{"versionString":"1.0","platform":"IOS","appVersionState":"READY_FOR_DISTRIBUTION","copyright":"old"}}]}`))
		case "/v1/appStoreVersions/V/appStoreVersionPhasedRelease":
			_, _ = w.Write([]byte(`{"data":{"id":"PH","type":"appStoreVersionPhasedReleases","attributes":{"phasedReleaseState":"ACTIVE"}}}`))
		case "/v1/apps/A/appAvailabilityV2":
			w.WriteHeader(http.StatusNotFound)
		case "/v1/apps/A/endUserLicenseAgreement", "/v1/apps/A/appPriceSchedule", "/v1/appStoreVersions/V/build", "/v1/appStoreVersions/V/appStoreReviewDetail":
			_, _ = w.Write([]byte(`{"data":null}`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().String("version", "", "")
	cmd.Flags().String("platform", "", "")
	cmd.Flags().Bool("confirm", true, "")
	cmd.Flags().Bool("resume", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	desired := &config.State{APIVersion: "flightline.dev/v1alpha1", Kind: "AppState", Metadata: config.StateMetadata{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Spec: config.StateSpec{Version: &config.VersionSpec{Copyright: new("changed"), PhasedRelease: &config.PhasedReleaseSpec{State: new("PAUSED")}}}}
	err := runApplyWithClient(cmd, "state.yaml", desired, fixtureASCClient(t, srv))
	if err == nil || !strings.Contains(err.Error(), "write intent") || writes.Load() != 0 {
		t.Fatalf("err=%v writes=%d", err, writes.Load())
	}
}
