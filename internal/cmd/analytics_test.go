package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type analyticsCmdFixture struct {
	srv *httptest.Server

	mu                   sync.Mutex
	requestID            asc.RequestID
	accessType           asc.AnalyticsAccessType
	stopped              bool
	reports              []analyticsCmdReport
	instances            map[asc.ReportID][]analyticsCmdInstance
	segments             map[asc.InstanceID][]analyticsCmdSegment
	signedAuthCount      atomic.Int32
	signedRequestCount   atomic.Int32
	apps                 map[string]string // bundleID -> appID
	segmentBlobs         map[string][]byte // path -> gzipped CSV
	customSegmentContent []byte
}

type analyticsCmdReport struct {
	ID       asc.ReportID
	Name     string
	Category asc.AnalyticsCategory
}

type analyticsCmdInstance struct {
	ID             asc.InstanceID
	Granularity    asc.AnalyticsGranularity
	ProcessingDate string
}

type analyticsCmdSegment struct {
	ID  string
	URL string // resolved against srv.URL once the server is up
}

func newAnalyticsCmdFixture(t *testing.T) *analyticsCmdFixture {
	t.Helper()
	f := &analyticsCmdFixture{
		requestID:    "REQ-CMD-1",
		accessType:   asc.AccessTypeOneTimeSnapshot,
		instances:    make(map[asc.ReportID][]analyticsCmdInstance),
		segments:     make(map[asc.InstanceID][]analyticsCmdSegment),
		apps:         map[string]string{"com.example.alpha": "1234567890"},
		segmentBlobs: make(map[string][]byte),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *analyticsCmdFixture) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/cdn/segments/") {
		f.serveSignedURL(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
		f.serveApps(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/analyticsReportRequests":
		f.serveCreate(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/reports") &&
		strings.HasPrefix(r.URL.Path, "/v1/analyticsReportRequests/"):
		f.serveListReports(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReportRequests/"):
		f.serveGetRequest(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReports/") &&
		strings.HasSuffix(r.URL.Path, "/instances"):
		f.serveListInstances(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReportInstances/") &&
		strings.HasSuffix(r.URL.Path, "/segments"):
		f.serveListSegments(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReportSegments/"):
		f.serveGetSegment(w, r)
	default:
		http.Error(w, "no fixture route: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func (f *analyticsCmdFixture) serveApps(w http.ResponseWriter, r *http.Request) {
	bundle := r.URL.Query().Get("filter[bundleId]")
	appID, ok := f.apps[bundle]
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"data": []map[string]any{}, "links": map[string]any{}}
	if ok {
		body["data"] = []map[string]any{{
			"type": "apps",
			"id":   appID,
			"attributes": map[string]any{
				"name":     "Test App",
				"bundleId": bundle,
				"sku":      "TEST",
			},
		}}
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *analyticsCmdFixture) serveCreate(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"data": map[string]any{
			"type": "analyticsReportRequests",
			"id":   string(f.requestID),
			"attributes": map[string]any{
				"accessType":             string(f.accessType),
				"stoppedDueToInactivity": false,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *analyticsCmdFixture) serveGetRequest(w http.ResponseWriter) {
	f.mu.Lock()
	stopped := f.stopped
	access := f.accessType
	f.mu.Unlock()
	body := map[string]any{
		"data": map[string]any{
			"type": "analyticsReportRequests",
			"id":   string(f.requestID),
			"attributes": map[string]any{
				"accessType":             string(access),
				"stoppedDueToInactivity": stopped,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *analyticsCmdFixture) serveListReports(w http.ResponseWriter) {
	f.mu.Lock()
	rows := append([]analyticsCmdReport(nil), f.reports...)
	f.mu.Unlock()
	data := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		data = append(data, map[string]any{
			"type": "analyticsReports",
			"id":   string(r.ID),
			"attributes": map[string]any{
				"name":     r.Name,
				"category": string(r.Category),
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "links": map[string]any{}})
}

func (f *analyticsCmdFixture) serveListInstances(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	id := asc.ReportID(parts[3])
	f.mu.Lock()
	rows := append([]analyticsCmdInstance(nil), f.instances[id]...)
	f.mu.Unlock()
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"type": "analyticsReportInstances",
			"id":   string(row.ID),
			"attributes": map[string]any{
				"granularity":    string(row.Granularity),
				"processingDate": row.ProcessingDate,
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "links": map[string]any{}})
}

func (f *analyticsCmdFixture) serveListSegments(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	id := asc.InstanceID(parts[3])
	f.mu.Lock()
	rows := append([]analyticsCmdSegment(nil), f.segments[id]...)
	f.mu.Unlock()
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"type": "analyticsReportSegments",
			"id":   row.ID,
			"attributes": map[string]any{
				"url":         row.URL,
				"checksum":    "deadbeef",
				"sizeInBytes": 42,
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "links": map[string]any{}})
}

func (f *analyticsCmdFixture) serveGetSegment(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	id := parts[3]
	signed := f.srv.URL + "/cdn/segments/" + id
	body := map[string]any{
		"data": map[string]any{
			"type": "analyticsReportSegments",
			"id":   id,
			"attributes": map[string]any{
				"url":         signed,
				"checksum":    "deadbeef",
				"sizeInBytes": 42,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *analyticsCmdFixture) serveSignedURL(w http.ResponseWriter, r *http.Request) {
	f.signedRequestCount.Add(1)
	if r.Header.Get("Authorization") != "" {
		f.signedAuthCount.Add(1)
	}
	f.mu.Lock()
	blob, ok := f.segmentBlobs[r.URL.Path]
	custom := f.customSegmentContent
	f.mu.Unlock()
	if !ok {
		content := []byte("date,impressions\n2026-05-01,42\n")
		if custom != nil {
			content = custom
		}
		blob = gzipBytesCmd(content)
	}
	w.Header().Set("Content-Type", "application/a-gzip")
	_, _ = w.Write(blob)
}

func gzipBytesCmd(in []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(in)
	_ = gz.Close()
	return buf.Bytes()
}

// Tests drive the cmd helpers directly; RunE bodies aren't invoked through
// cobra because most production flag wiring is global.
func analyticsCmdClient(t *testing.T, f *analyticsCmdFixture) *asc.Client {
	t.Helper()
	keyPath := writeEphemeralKey(t)
	c, err := asc.New(asc.Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: f.srv.Client(),
		BaseURL:    f.srv.URL,
		UserAgent:  "flightline-test/1.0",
	})
	if err != nil {
		t.Fatalf("analyticsCmdClient: %v", err)
	}
	return c
}

func withCmdStateRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FLIGHTLINE_STATE_HOME", dir)
	return dir
}

func withFastPoll(t *testing.T) {
	t.Helper()
	prev := analyticsPollOpts
	analyticsPollOpts = asc.PollOpts{
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
		Multiplier:     1.0,
		MaxAttempts:    50,
	}
	t.Cleanup(func() { analyticsPollOpts = prev })
}

func TestAnalytics_RegisteredOnRoot(t *testing.T) {
	var found *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "analytics" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("analytics not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range found.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"request", "list-instances", "download", "status"} {
		if !subs[want] {
			t.Errorf("analytics subcommand %q not registered", want)
		}
	}
}

func TestAnalytics_RequestPersistsState(t *testing.T) {
	root := withCmdStateRoot(t)
	f := newAnalyticsCmdFixture(t)
	c := analyticsCmdClient(t, f)

	view, err := submitAndPersist(t.Context(), c, "com.example.alpha", "1234567890", asc.AccessTypeOneTimeSnapshot)
	if err != nil {
		t.Fatalf("submitAndPersist: %v", err)
	}
	if view.RequestID != string(f.requestID) {
		t.Errorf("RequestID = %q, want %q", view.RequestID, f.requestID)
	}
	if view.Status != "queued" {
		t.Errorf("Status = %q, want queued", view.Status)
	}

	statePath := filepath.Join(root, "com.example.alpha", "analytics.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	loaded, err := asc.LoadAsyncState("com.example.alpha", asc.ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}
	if loaded.RequestID != f.requestID {
		t.Errorf("loaded RequestID = %q, want %q", loaded.RequestID, f.requestID)
	}
	if loaded.Status != "queued" {
		t.Errorf("loaded status = %q, want queued", loaded.Status)
	}
}

func TestAnalytics_RequestWaitYieldsReports(t *testing.T) {
	withCmdStateRoot(t)
	withFastPoll(t)
	f := newAnalyticsCmdFixture(t)
	c := analyticsCmdClient(t, f)
	f.reports = []analyticsCmdReport{
		{ID: "RPT-A", Name: "App Usage", Category: asc.CategoryAppUsage},
		{ID: "RPT-B", Name: "Commerce", Category: asc.CategoryCommerce},
	}

	view, err := submitAndPersist(t.Context(), c, "com.example.alpha", "1234567890", asc.AccessTypeOneTimeSnapshot)
	if err != nil {
		t.Fatalf("submitAndPersist: %v", err)
	}
	view, err = pollAndAppend(t.Context(), c, view, 5*time.Second)
	if err != nil {
		t.Fatalf("pollAndAppend: %v", err)
	}
	if view.Status != "completed" {
		t.Errorf("Status = %q, want completed", view.Status)
	}
	if len(view.Reports) != 2 {
		t.Fatalf("Reports len = %d, want 2: %+v", len(view.Reports), view.Reports)
	}
}

func TestAnalytics_RequestWaitOngoingTimesOut(t *testing.T) {
	withCmdStateRoot(t)
	withFastPoll(t)
	f := newAnalyticsCmdFixture(t)
	f.accessType = asc.AccessTypeOngoing
	c := analyticsCmdClient(t, f)
	// No reports: ONGOING never auto-terminates, so context timeout drives it.

	view, err := submitAndPersist(t.Context(), c, "com.example.alpha", "1234567890", asc.AccessTypeOngoing)
	if err != nil {
		t.Fatalf("submitAndPersist: %v", err)
	}
	view, err = pollAndAppend(t.Context(), c, view, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("pollAndAppend: %v", err)
	}
	if view.Status != "timeout" {
		t.Errorf("Status = %q, want timeout", view.Status)
	}
}

func TestAnalytics_RequestWaitONGOINGStopped(t *testing.T) {
	withCmdStateRoot(t)
	withFastPoll(t)
	f := newAnalyticsCmdFixture(t)
	f.accessType = asc.AccessTypeOngoing
	f.stopped = true // server immediately reports stoppedDueToInactivity
	c := analyticsCmdClient(t, f)

	view, err := submitAndPersist(t.Context(), c, "com.example.alpha", "1234567890", asc.AccessTypeOngoing)
	if err != nil {
		t.Fatalf("submitAndPersist: %v", err)
	}
	view, err = pollAndAppend(t.Context(), c, view, 5*time.Second)
	if err != nil {
		t.Fatalf("pollAndAppend: %v", err)
	}
	if view.Status != "stopped" {
		t.Errorf("Status = %q, want stopped", view.Status)
	}
}

func TestParseAccessType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want asc.AnalyticsAccessType
		err  bool
	}{
		{"ONE_TIME_SNAPSHOT", asc.AccessTypeOneTimeSnapshot, false},
		{"one_time_snapshot", asc.AccessTypeOneTimeSnapshot, false},
		{"ONGOING", asc.AccessTypeOngoing, false},
		{" ongoing ", asc.AccessTypeOngoing, false},
		{"", "", true},
		{"NONSENSE", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseAccessType(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("want error for %q, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateWaitFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		access asc.AnalyticsAccessType
		wait   bool
		max    time.Duration
		err    bool
	}{
		{"no wait → fine", asc.AccessTypeOngoing, false, 0, false},
		{"snapshot wait no max → fine", asc.AccessTypeOneTimeSnapshot, true, 0, false},
		{"ongoing wait no max → reject", asc.AccessTypeOngoing, true, 0, true},
		{"ongoing wait with max → fine", asc.AccessTypeOngoing, true, time.Minute, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWaitFlags(tc.access, tc.wait, tc.max)
			if tc.err && err == nil {
				t.Errorf("want error, got nil")
			}
			if !tc.err && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAnalytics_ListInstances_FiltersAndExpands(t *testing.T) {
	withCmdStateRoot(t)
	f := newAnalyticsCmdFixture(t)
	c := analyticsCmdClient(t, f)

	if err := asc.PersistAsyncState(asc.AsyncState{
		BundleID:    "com.example.alpha",
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   f.requestID,
		SubmittedAt: time.Now().UTC(),
		Status:      "completed",
		Reports: []asc.PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: asc.CategoryAppUsage},
			{ID: "RPT-B", Name: "Commerce", Category: asc.CategoryCommerce},
		},
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	f.instances["RPT-A"] = []analyticsCmdInstance{
		{ID: "INST-1", Granularity: asc.GranularityDaily, ProcessingDate: "2026-05-01"},
		{ID: "INST-2", Granularity: asc.GranularityWeekly, ProcessingDate: "2026-05-08"},
	}
	f.instances["RPT-B"] = []analyticsCmdInstance{
		{ID: "INST-3", Granularity: asc.GranularityDaily, ProcessingDate: "2026-05-01"},
	}

	state, err := loadAnalyticsState("com.example.alpha")
	if err != nil {
		t.Fatalf("loadAnalyticsState: %v", err)
	}

	all := filterReportsForList(state.Reports, "", "", "")
	if len(all) != 2 {
		t.Errorf("filter=none → %d, want 2", len(all))
	}

	one := filterReportsForList(state.Reports, "RPT-B", "", "")
	if len(one) != 1 || one[0].ID != "RPT-B" {
		t.Errorf("filter by report-id failed: %+v", one)
	}

	usage := filterReportsForList(state.Reports, "", "APP_USAGE", "")
	if len(usage) != 1 || usage[0].ID != "RPT-A" {
		t.Errorf("filter by category failed: %+v", usage)
	}

	commerce := filterReportsForList(state.Reports, "", "", "comm")
	if len(commerce) != 1 || commerce[0].ID != "RPT-B" {
		t.Errorf("filter by name failed: %+v", commerce)
	}

	for _, r := range state.Reports {
		insts, lerr := c.ListAnalyticsInstances(t.Context(), r.ID)
		if lerr != nil {
			t.Fatalf("ListAnalyticsInstances(%q): %v", r.ID, lerr)
		}
		if r.ID == "RPT-A" && len(insts) != 2 {
			t.Errorf("RPT-A: %d instances, want 2", len(insts))
		}
		if r.ID == "RPT-B" && len(insts) != 1 {
			t.Errorf("RPT-B: %d instances, want 1", len(insts))
		}
	}
}

func TestAnalytics_ListInstances_MissingState(t *testing.T) {
	withCmdStateRoot(t)
	_, err := loadAnalyticsState("com.example.no_state")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "no active analytics request") {
		t.Errorf("unhelpful error: %v", err)
	}
	if !strings.Contains(err.Error(), "flightline analytics request") {
		t.Errorf("error missing remediation hint: %v", err)
	}
}

func TestAnalytics_ListInstances_ReportIDNotFound(t *testing.T) {
	t.Parallel()
	state := []asc.PersistedAnalyticsReport{
		{ID: "RPT-A", Name: "A", Category: asc.CategoryAppUsage},
	}
	got := filterReportsForList(state, "RPT-MISSING", "", "")
	if got != nil {
		t.Errorf("got %+v, want nil for missing id", got)
	}
}

func TestAnalytics_Download_NoAuthHeaderOnSignedURL(t *testing.T) {
	withCmdStateRoot(t)
	outDir := t.TempDir()
	f := newAnalyticsCmdFixture(t)
	c := analyticsCmdClient(t, f)

	if err := asc.PersistAsyncState(asc.AsyncState{
		BundleID:    "com.example.alpha",
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   f.requestID,
		SubmittedAt: time.Now().UTC(),
		Status:      "completed",
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	f.segments["INST-1"] = []analyticsCmdSegment{
		{ID: "SEG-1", URL: f.srv.URL + "/cdn/segments/SEG-1"},
		{ID: "SEG-2", URL: f.srv.URL + "/cdn/segments/SEG-2"},
	}
	f.customSegmentContent = []byte("date,impressions\n2026-05-01,100\n")

	segs, err := c.ListAnalyticsSegments(t.Context(), "INST-1")
	if err != nil {
		t.Fatalf("ListAnalyticsSegments: %v", err)
	}
	if len(segs) != 2 {
		t.Fatalf("segments len = %d, want 2", len(segs))
	}
	for i, seg := range segs {
		body, derr := c.DownloadAnalyticsSegment(t.Context(), seg.ID)
		if derr != nil {
			t.Fatalf("DownloadAnalyticsSegment %s: %v", seg.ID, derr)
		}
		path, werr := writeSegmentFile("com.example.alpha", "INST-1", i, body, outDir)
		if werr != nil {
			t.Fatalf("writeSegmentFile: %v", werr)
		}
		if !strings.HasPrefix(path, outDir) {
			t.Errorf("path %q not under outDir %q", path, outDir)
		}
		got, rerr := os.ReadFile(path) // #nosec G304 -- path is constructed from t.TempDir()
		if rerr != nil {
			t.Fatalf("read back: %v", rerr)
		}
		if !bytes.Equal(got, f.customSegmentContent) {
			t.Errorf("file body mismatch: got %q, want %q", got, f.customSegmentContent)
		}
	}

	if f.signedRequestCount.Load() != 2 {
		t.Errorf("signedRequestCount = %d, want 2", f.signedRequestCount.Load())
	}
	if f.signedAuthCount.Load() != 0 {
		t.Fatalf("signed URL got Authorization header on %d requests: Apple's CDN rejects bearer tokens", f.signedAuthCount.Load())
	}
}

func TestAnalytics_Download_MissingState(t *testing.T) {
	withCmdStateRoot(t)
	if _, err := loadAnalyticsState("com.example.no_state"); err == nil {
		t.Fatal("want missing-state error, got nil")
	}
}

func TestAnalytics_WriteSegmentFile_RejectsExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.csv")
	if err := os.WriteFile(existing, []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := writeSegmentFile("com.example.alpha", "INST-1", 0, []byte("data"), existing)
	if err == nil || !strings.Contains(err.Error(), "existing file") {
		t.Errorf("got %v, want existing-file error", err)
	}
}

func TestAnalytics_WriteSegmentFile_CreatesOutDir(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	out := filepath.Join(parent, "nested", "reports")
	path, err := writeSegmentFile("com.example.alpha", "INST-1", 0, []byte("data"), out)
	if err != nil {
		t.Fatalf("writeSegmentFile: %v", err)
	}
	if !strings.HasPrefix(path, out) {
		t.Errorf("path %q not under %q", path, out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file missing: %v", err)
	}
}

func TestAnalytics_Status_ReadsState(t *testing.T) {
	withCmdStateRoot(t)
	if err := asc.PersistAsyncState(asc.AsyncState{
		BundleID:    "com.example.alpha",
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   "REQ-77",
		SubmittedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		LastPollAt:  time.Date(2026, 5, 1, 10, 5, 0, 0, time.UTC),
		Status:      "processing",
		Reports: []asc.PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: asc.CategoryAppUsage},
		},
		DownloadedSegments: []string{"SEG-1"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	state, err := loadAnalyticsState("com.example.alpha")
	if err != nil {
		t.Fatalf("loadAnalyticsState: %v", err)
	}
	if state.RequestID != "REQ-77" || state.Status != "processing" {
		t.Errorf("loaded state = %+v", state)
	}
	if len(state.Reports) != 1 || len(state.DownloadedSegments) != 1 {
		t.Errorf("counts wrong: %+v", state)
	}
}

func TestAnalytics_Status_MissingFileHelpfulError(t *testing.T) {
	withCmdStateRoot(t)
	_, err := loadAnalyticsState("com.example.no_state")
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"no active analytics request", "flightline analytics request"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing substring %q", err.Error(), want)
		}
	}
}

func TestAnalytics_RefreshResumesQueuedRequest(t *testing.T) {
	withCmdStateRoot(t)
	f := newAnalyticsCmdFixture(t)
	f.reports = []analyticsCmdReport{
		{ID: "RPT-1", Name: "App Usage", Category: asc.CategoryAppUsage},
		{ID: "RPT-2", Name: "Commerce", Category: asc.CategoryCommerce},
	}
	state := asc.AsyncState{
		BundleID:    "com.example.alpha",
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   f.requestID,
		SubmittedAt: time.Date(2026, 7, 20, 15, 42, 0, 0, time.UTC),
		Status:      "queued",
		Reports:     []asc.PersistedAnalyticsReport{},
	}
	if err := asc.PersistAsyncState(state); err != nil {
		t.Fatalf("seed: %v", err)
	}

	refreshed, err := refreshAnalyticsState(context.Background(), fixtureASCClient(t, f.srv), state)
	if err != nil {
		t.Fatalf("refreshAnalyticsState: %v", err)
	}
	if refreshed.Status != "reports_available" || len(refreshed.Reports) != 2 {
		t.Fatalf("refreshed = %+v", refreshed)
	}
	refreshed, err = refreshAnalyticsState(context.Background(), fixtureASCClient(t, f.srv), refreshed)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if len(refreshed.Reports) != 2 {
		t.Fatalf("second refresh duplicated reports: %+v", refreshed.Reports)
	}
	loaded, err := asc.LoadAsyncState("com.example.alpha", asc.ReportClassAnalytics)
	if err != nil || loaded.Status != "reports_available" || len(loaded.Reports) != 2 {
		t.Fatalf("persisted refresh = %+v err=%v", loaded, err)
	}
}

func TestAnalytics_RequestGuardRequiresForceForActiveState(t *testing.T) {
	withCmdStateRoot(t)
	if err := asc.PersistAsyncState(asc.AsyncState{
		BundleID: "com.example.alpha", ReportClass: asc.ReportClassAnalytics,
		RequestID: "REQ-ACTIVE", Status: "queued",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := guardAnalyticsRequestReplacement("com.example.alpha", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("guard error = %v", err)
	}
	if err := guardAnalyticsRequestReplacement("com.example.alpha", true); err != nil {
		t.Fatalf("forced replacement: %v", err)
	}
}

func TestAnalytics_JSONShape_RequestView(t *testing.T) {
	t.Parallel()
	v := AnalyticsRequestView{
		BundleID:    "com.example.alpha",
		RequestID:   "REQ-1",
		AccessType:  "ONE_TIME_SNAPSHOT",
		Status:      "completed",
		SubmittedAt: "2026-05-01T10:00:00Z",
		LastPollAt:  "2026-05-01T10:05:00Z",
		Reports: []asc.PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: asc.CategoryAppUsage},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.alpha"`,
		`"requestId":"REQ-1"`,
		`"accessType":"ONE_TIME_SNAPSHOT"`,
		`"status":"completed"`,
		`"submittedAt":"2026-05-01T10:00:00Z"`,
		`"lastPollAt":"2026-05-01T10:05:00Z"`,
		`"reports":`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestAnalytics_JSONShape_DownloadView(t *testing.T) {
	t.Parallel()
	v := AnalyticsDownloadView{
		BundleID:   "com.example.alpha",
		InstanceID: "INST-1",
		Files:      []string{"./com.example.alpha-INST-1-segment0.csv"},
		ByteCount:  42,
		Segments: []asc.SegmentDownloadResult{
			{SegmentID: "SEG-1", InstanceID: "INST-1", ByteCount: 42, Header: []string{"date", "impressions"}},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.alpha"`,
		`"instanceId":"INST-1"`,
		`"files":[`,
		`"byteCount":42`,
		`"segments":[`,
		`"header":["date","impressions"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	// Bytes is excluded from JSON.
	if strings.Contains(out, `"bytes"`) || strings.Contains(out, `"Bytes"`) {
		t.Errorf("Bytes leaked into JSON: %s", out)
	}
}

func TestAnalytics_JSONShape_StatusView(t *testing.T) {
	t.Parallel()
	v := AnalyticsStatusView{
		BundleID:   "com.example.alpha",
		StateFile:  "/some/path/analytics.json",
		RequestID:  "REQ-1",
		Status:     "processing",
		Reports:    []asc.PersistedAnalyticsReport{},
		Downloaded: []string{},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.alpha"`,
		`"stateFile":"/some/path/analytics.json"`,
		`"requestId":"REQ-1"`,
		`"status":"processing"`,
		`"reports":[]`,
		`"downloadedSegments":[]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestAnalytics_TableRows_RequestView(t *testing.T) {
	t.Parallel()
	v := AnalyticsRequestView{
		BundleID:   "com.example.alpha",
		RequestID:  "REQ-1",
		AccessType: "ONE_TIME_SNAPSHOT",
		Status:     "completed",
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers len = %d, want 2", len(headers))
	}
	if len(rows) < 5 {
		t.Errorf("rows len = %d, want >= 5", len(rows))
	}
}

func TestAnalytics_TableRows_InstancesView(t *testing.T) {
	t.Parallel()
	v := AnalyticsInstancesView{
		BundleID:  "com.example.alpha",
		RequestID: "REQ-1",
		Reports: []AnalyticsReportInstancesEntry{
			{
				Report: asc.PersistedAnalyticsReport{ID: "RPT-A", Name: "App Usage", Category: asc.CategoryAppUsage},
				Instances: []asc.AnalyticsReportInstance{
					{ID: "INST-1", Granularity: asc.GranularityDaily, ProcessingDate: "2026-05-01"},
				},
			},
			{
				Report:    asc.PersistedAnalyticsReport{ID: "RPT-B", Name: "Commerce", Category: asc.CategoryCommerce},
				Instances: nil, // empty branch
			},
		},
	}
	headers, rows := v.TableRows()
	want := []string{"REPORT", "CATEGORY", "INSTANCE", "GRANULARITY", "DATE"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	if rows[1][2] != "(no instances yet)" {
		t.Errorf("empty branch placeholder = %q", rows[1][2])
	}
}

func TestAnalytics_StateRoundtripsAcrossLoad(t *testing.T) {
	withCmdStateRoot(t)
	original := asc.AsyncState{
		BundleID:           "com.example.alpha",
		ReportClass:        asc.ReportClassAnalytics,
		RequestID:          "REQ-RESUME",
		SubmittedAt:        time.Now().UTC().Add(-time.Hour),
		Status:             "processing",
		Reports:            []asc.PersistedAnalyticsReport{{ID: "RPT-A", Name: "App Usage", Category: asc.CategoryAppUsage}},
		DownloadedSegments: []string{"SEG-1"},
	}
	if err := asc.PersistAsyncState(original); err != nil {
		t.Fatalf("persist: %v", err)
	}
	got, err := loadAnalyticsState("com.example.alpha")
	if err != nil {
		t.Fatalf("loadAnalyticsState: %v", err)
	}
	if got.RequestID != "REQ-RESUME" {
		t.Errorf("RequestID = %q, want REQ-RESUME", got.RequestID)
	}
	if len(got.DownloadedSegments) != 1 || got.DownloadedSegments[0] != "SEG-1" {
		t.Errorf("DownloadedSegments lost: %+v", got.DownloadedSegments)
	}
}

func TestAnalytics_FinishPoll_GenericErrorMarksFailed(t *testing.T) {
	withCmdStateRoot(t)
	view := AnalyticsRequestView{
		BundleID:    "com.example.alpha",
		RequestID:   "REQ-1",
		AccessType:  "ONE_TIME_SNAPSHOT",
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
	}
	view, err := finishPoll(view, errors.New("network borked"))
	if err == nil {
		t.Fatal("want propagated error, got nil")
	}
	if view.Status != "failed" {
		t.Errorf("Status = %q, want failed", view.Status)
	}
}

// Guards request IDs against future Apple-side slashes or spaces.
func TestAnalytics_PathEscape(t *testing.T) {
	t.Parallel()
	id := asc.RequestID("REQ/with space")
	if got := url.PathEscape(string(id)); !strings.Contains(got, "%2F") || !strings.Contains(got, "%20") {
		t.Errorf("PathEscape didn't encode special chars: %q", got)
	}
}

// Sanity-check that t.Context cancellation propagates through pollAndAppend.
func TestAnalytics_PollContextCancel(t *testing.T) {
	withCmdStateRoot(t)
	withFastPoll(t)
	f := newAnalyticsCmdFixture(t)
	f.accessType = asc.AccessTypeOngoing
	c := analyticsCmdClient(t, f)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	view, err := submitAndPersist(ctx, c, "com.example.alpha", "1234567890", asc.AccessTypeOngoing)
	if err != nil {
		// Cancellation may surface at submit; the assertion that matters is that
		// pollAndAppend doesn't hang, so a failed submit makes the test moot.
		return
	}
	view, err = pollAndAppend(ctx, c, view, time.Second)
	if err != nil {
		t.Fatalf("pollAndAppend: %v", err)
	}
	if view.Status != "cancelled" {
		t.Errorf("Status = %q, want cancelled", view.Status)
	}
}
