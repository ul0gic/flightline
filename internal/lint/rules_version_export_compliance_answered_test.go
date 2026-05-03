package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestVersionExportComplianceAnswered_OfflineUnmanagedNoOp(t *testing.T) {
	got := versionExportComplianceAnsweredRule{}.Check(CheckContext{
		Ctx:   context.Background(),
		State: &config.State{Spec: config.StateSpec{
			// ExportCompliance is nil: not managed, not flagged.
		}},
	})
	if len(got) != 0 {
		t.Errorf("nil ExportCompliance returned %d diags, want 0: %+v", len(got), got)
	}
}

func TestVersionExportComplianceAnswered_OfflineUnsetFires(t *testing.T) {
	got := versionExportComplianceAnsweredRule{}.Check(CheckContext{
		Ctx: context.Background(),
		State: &config.State{Spec: config.StateSpec{
			ExportCompliance: &config.ExportComplianceSpec{}, // managed, but UsesNonExemptEncryption nil
		}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "version.export-compliance-answered" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
	if got[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", got[0].Severity)
	}
}

func TestVersionExportComplianceAnswered_OfflineSetNoOp(t *testing.T) {
	f := false
	got := versionExportComplianceAnsweredRule{}.Check(CheckContext{
		Ctx: context.Background(),
		State: &config.State{Spec: config.StateSpec{
			ExportCompliance: &config.ExportComplianceSpec{UsesNonExemptEncryption: &f},
		}},
	})
	if len(got) != 0 {
		t.Errorf("answer=false returned %d diags, want 0: %+v", len(got), got)
	}
}

func TestVersionExportComplianceAnswered_LiveBuildUnsetFires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ver-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/build"):
			// usesNonExemptEncryption omitted = nil pointer in our type
			_, _ = w.Write([]byte(`{"data":{"id":"b-1","type":"builds","attributes":{"version":"42","processingState":"VALID"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	got := versionExportComplianceAnsweredRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Version: "1.0.1", Live: true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
}
