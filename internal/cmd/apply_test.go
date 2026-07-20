package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
	"github.com/ul0gic/flightline/internal/state"
)

// TestApplyResult_TableEmpty: no changes renders the (none) row.
func TestApplyResult_TableEmpty(t *testing.T) {
	r := &ApplyResult{}
	headers, rows := r.TableRows()
	if len(headers) != 4 {
		t.Errorf("headers len %d, want 4", len(headers))
	}
	if len(rows) != 1 || rows[0][0] != "(none)" {
		t.Errorf("expected (none) row; got %+v", rows)
	}
}

// TestApplyResult_TableMixed: applied + skipped + errors render in
// stable order with status labels.
func TestApplyResult_TableMixed(t *testing.T) {
	r := &ApplyResult{
		Applied: []plan.Change{{Op: plan.OpUpdate, Path: "/spec/version/copyright"}},
		Skipped: []plan.Change{{Op: plan.OpUpdate, Path: "/spec/version/releaseType"}},
		Errors: []state.ChangeError{{
			Change:  plan.Change{Op: plan.OpUpdate, Path: "/spec/iap/products/x/name"},
			Message: "boom",
			Err:     errors.New("boom"),
		}},
	}
	_, rows := r.TableRows()
	if len(rows) != 3 {
		t.Fatalf("rows len %d, want 3", len(rows))
	}
	if rows[0][0] != "applied" || rows[1][0] != "skipped" || rows[2][0] != "error" {
		t.Errorf("status column drift: %+v", rows)
	}
	if rows[2][3] != "boom" {
		t.Errorf("error detail missing from table: %+v", rows[2])
	}
}

func TestApplyResult_JSONIncludesPerChangeErrorMessage(t *testing.T) {
	r := &ApplyResult{
		BundleID: "com.example.app",
		Errors: []state.ChangeError{{
			Change:  plan.Change{Op: plan.OpUpdate, Path: "/spec/iap/products/x/name"},
			Message: "request failed",
		}},
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(buf)
	if !strings.Contains(got, `"message":"request failed"`) {
		t.Errorf("JSON drops error message: %s", got)
	}
	if strings.Contains(got, `"errors":[{}]`) {
		t.Errorf("JSON still serializes errors as empty objects: %s", got)
	}
}

func TestRunApplyWithClient_DryRunReadsLiveStateWithoutMutations(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}],"links":{}}`))
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","platform":"IOS","appVersionState":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		}
	}))
	defer srv.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().String("version", "", "")
	cmd.Flags().String("platform", "", "")
	cmd.Flags().Bool("confirm", false, "")
	cmd.Flags().Bool("resume", false, "")
	cmd.Flags().Bool("dry-run", true, "")
	desired := &config.State{
		APIVersion: "flightline.dev/v1alpha1",
		Kind:       "AppState",
		Metadata: config.StateMetadata{
			BundleID: "com.example.app",
			Version:  "1.0",
			Platform: "IOS",
		},
	}
	viper.Reset()
	viper.Set("output", "json")
	captureStdout(t, func() {
		if err := runApplyWithClient(cmd, "state.yaml", desired, fixtureASCClient(t, srv)); err != nil {
			t.Errorf("runApplyWithClient: %v", err)
		}
	})

	mu.Lock()
	defer mu.Unlock()
	if len(methods) == 0 {
		t.Fatal("dry-run made no live read requests")
	}
	for _, method := range methods {
		if method != http.MethodGet {
			t.Errorf("dry-run issued mutating request method %s; methods=%v", method, methods)
		}
	}
}
