package asc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPerfPowerMetrics_UsesXcodeMetricsAccept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != xcodeMetricsMediaType {
			t.Errorf("Accept = %q, want %q", got, xcodeMetricsMediaType)
		}
		w.Header().Set("Content-Type", xcodeMetricsMediaType)
		_, _ = w.Write([]byte(`{"version":"1.0","productData":[]}`))
	}))
	t.Cleanup(srv.Close)

	c := fixtureClient(t, srv)
	got, err := c.FetchPerfPowerMetrics(context.Background(), "/v1/apps/123/perfPowerMetrics", nil)
	if err != nil {
		t.Fatalf("FetchPerfPowerMetrics: %v", err)
	}
	if got.Version != "1.0" || got.ProductData == nil {
		t.Fatalf("response = %+v", got)
	}
}

func TestFetchPerfPowerMetrics_RejectsUnrelatedPath(t *testing.T) {
	c := &Client{}
	if _, err := c.FetchPerfPowerMetrics(context.Background(), "/v1/apps", nil); err == nil {
		t.Fatal("expected path validation error")
	}
}
