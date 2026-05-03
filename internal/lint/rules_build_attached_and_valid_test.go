package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildAttachedAndValid_OfflineNoOp(t *testing.T) {
	got := buildAttachedAndValidRule{}.Check(CheckContext{Ctx: context.Background(), Live: false})
	if len(got) != 0 {
		t.Errorf("offline returned %d diags, want 0 (live-only rule)", len(got))
	}
}

func TestBuildAttachedAndValid_FiresWhenNoBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ver-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/build"):
			// data: null shape
			_, _ = w.Write([]byte(`{"data":{"id":"","type":"builds","attributes":{}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	got := buildAttachedAndValidRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Version: "1.0.1", Live: true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "no build attached") {
		t.Errorf("message = %q, want 'no build attached'", got[0].Message)
	}
}

func TestBuildAttachedAndValid_FiresWhenProcessing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ver-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = w.Write([]byte(`{"data":{"id":"b-1","type":"builds","attributes":{"version":"42","processingState":"PROCESSING"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	got := buildAttachedAndValidRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Version: "1.0.1", Live: true,
	})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "PROCESSING") {
		t.Errorf("message did not include processing state: %q", got[0].Message)
	}
}

func TestBuildAttachedAndValid_NoOpWhenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ver-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = w.Write([]byte(`{"data":{"id":"b-1","type":"builds","attributes":{"version":"42","processingState":"VALID"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	got := buildAttachedAndValidRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Version: "1.0.1", Live: true,
	})
	if len(got) != 0 {
		t.Errorf("got %d diags, want 0 when VALID: %+v", len(got), got)
	}
}
