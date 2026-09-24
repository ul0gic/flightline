package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestC1C_StatusSeparatesDefinitionsFromUncheckedData(t *testing.T) {
	t.Parallel()

	v := AnalyticsStatusView{
		Status:                     "reports_available",
		ReportDefinitionsAvailable: true,
		DataReadiness:              "unchecked",
		Reports: []asc.PersistedAnalyticsReport{{
			ID: "RPT-1", Name: "App Sessions", Category: asc.CategoryAppUsage,
		}},
	}

	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got["status"] != "reports_available" {
		t.Errorf("status = %v, want existing compatibility value", got["status"])
	}
	if got["reportDefinitionsAvailable"] != true {
		t.Errorf("reportDefinitionsAvailable = %v, want true", got["reportDefinitionsAvailable"])
	}
	if got["dataReadiness"] != "unchecked" {
		t.Errorf("dataReadiness = %v, want unchecked", got["dataReadiness"])
	}

	_, rows := v.TableRows()
	labels := make(map[string]string, len(rows))
	for _, row := range rows {
		labels[row[0]] = row[1]
	}
	if labels["REPORT_DEFINITIONS_AVAILABLE"] != "true" || labels["REPORT_DEFINITIONS"] != "1" {
		t.Errorf("definition rows = %#v", labels)
	}
	if labels["DATA_READINESS"] != "unchecked" {
		t.Errorf("DATA_READINESS = %q, want unchecked", labels["DATA_READINESS"])
	}
	if labels["STATUS"] != "reports_available" {
		t.Errorf("request status = %q, want existing reports_available value", labels["STATUS"])
	}
}

func TestC1C_ReportDataReadinessIsScopedToCheckedReport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		instanceCount int
		want          string
	}{
		{name: "no instances", want: "none_available"},
		{name: "instances returned", instanceCount: 1, want: "available"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analyticsReportDataReadiness(tc.instanceCount)
			if got != tc.want {
				t.Errorf("readiness = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestC1C_InstanceReadinessIsPresentInJSON(t *testing.T) {
	t.Parallel()

	entry := AnalyticsReportInstancesEntry{
		Report: asc.PersistedAnalyticsReport{
			ID: "RPT-1", Name: "App Sessions", Category: asc.CategoryAppUsage,
		},
		Instances:     []asc.AnalyticsReportInstance{},
		DataReadiness: "none_available",
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal report instances: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode report instances: %v", err)
	}
	if got["dataReadiness"] != "none_available" {
		t.Errorf("dataReadiness = %v, want none_available for this checked report", got["dataReadiness"])
	}
}

func TestC1C_RefreshDoesNotEnumerateReportInstances(t *testing.T) {
	withCmdStateRoot(t)
	var instanceCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/analyticsReportRequests/REQ-C1C-1":
			_, _ = w.Write([]byte(`{"data":{"type":"analyticsReportRequests","id":"REQ-C1C-1","attributes":{"accessType":"ONE_TIME_SNAPSHOT","stoppedDueToInactivity":false}}}`))
		case "/v1/analyticsReportRequests/REQ-C1C-1/reports":
			_, _ = w.Write([]byte(`{"data":[{"type":"analyticsReports","id":"RPT-C1C-1","attributes":{"name":"App Sessions","category":"APP_USAGE"}}],"links":{}}`))
		default:
			if r.URL.Path == "/v1/analyticsReports/RPT-C1C-1/instances" {
				instanceCalls.Add(1)
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	c, err := asc.New(asc.Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    writeEphemeralKey(t),
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		UserAgent:  "flightline-test/1.0",
	})
	if err != nil {
		t.Fatalf("create ASC client: %v", err)
	}
	state := asc.AsyncState{
		BundleID:    "com.example.alpha",
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   "REQ-C1C-1",
		Status:      "processing",
		Reports:     []asc.PersistedAnalyticsReport{},
	}
	refreshed, err := refreshAnalyticsState(t.Context(), c, state)
	if err != nil {
		t.Fatalf("refresh analytics state: %v", err)
	}
	if refreshed.Status != "reports_available" || len(refreshed.Reports) != 1 {
		t.Fatalf("refreshed state = %+v", refreshed)
	}
	if got := instanceCalls.Load(); got != 0 {
		t.Errorf("refresh requested report instances %d times, want 0", got)
	}
}
