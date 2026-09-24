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

func TestG2_UnsupportedIntentPreventsAllWrites(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		switch r.URL.Path {
		case "/v1/appStoreVersions/VER1/appStoreVersionPhasedRelease":
			w.WriteHeader(http.StatusNotFound)
		case "/v1/apps/APP1":
			_, _ = w.Write([]byte(`{"data":{"type":"apps","id":"APP1","attributes":{}}}`))
		case "/v1/apps/APP1/endUserLicenseAgreement":
			_, _ = w.Write([]byte(`{"data":null}`))
		case "/v1/apps/APP1/appAvailabilityV2":
			w.WriteHeader(http.StatusNotFound)
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}]}`))
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","platform":"IOS","appVersionState":"PREPARE_FOR_SUBMISSION","copyright":"old"}}]}`))
		case "/v1/appStoreVersions/VER1/build", "/v1/appStoreVersions/VER1/appStoreReviewDetail", "/v1/apps/APP1/appPriceSchedule":
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
	copyright, name, hosting := "new", "Unlock", "NON_HOSTED"
	desired := &config.State{
		APIVersion: "flightline.dev/v1alpha1", Kind: "AppState",
		Metadata: config.StateMetadata{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"},
		Spec: config.StateSpec{
			Version: &config.VersionSpec{Copyright: &copyright},
			IAP:     &config.IAPSpec{Products: map[string]config.IAPProduct{"com.example.unlock": {Type: "NON_CONSUMABLE", Name: &name, ContentHosting: &hosting}}},
		},
	}
	err := runApplyWithClient(cmd, "state.yaml", desired, fixtureASCClient(t, srv))
	if err == nil || !strings.Contains(err.Error(), "write intent") {
		t.Fatalf("expected write-intent error before mutation, got %v", err)
	}
	if got := writes.Load(); got != 0 {
		t.Fatalf("unsupported intent allowed %d writes", got)
	}
}
