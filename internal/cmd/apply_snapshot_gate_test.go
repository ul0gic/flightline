package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func TestG1_FailedSnapshotPreventsConfirmedApply(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}]}`))
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","platform":"IOS","appVersionState":"PREPARE_FOR_SUBMISSION","copyright":"old"}}]}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"status":"403","code":"FORBIDDEN","title":"Forbidden"}]}`))
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
	copyright := "new"
	desired := &config.State{
		APIVersion: "flightline.dev/v1alpha1", Kind: "AppState",
		Metadata: config.StateMetadata{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"},
		Spec:     config.StateSpec{Version: &config.VersionSpec{Copyright: &copyright}},
	}
	err := runApplyWithClient(cmd, "state.yaml", desired, fixtureASCClient(t, srv))
	if !errors.Is(err, asc.ErrForbidden) {
		t.Fatalf("expected typed forbidden read error, got %v", err)
	}
	if got := writes.Load(); got != 0 {
		t.Fatalf("failed snapshot allowed %d writes", got)
	}
}
