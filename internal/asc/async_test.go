package asc

// Async-poll wrapper tests.
//
// The wrapper is fixture-driven via a programmable httptest.Server (defined
// inline below — the route table needs to mutate response bodies between
// polls, which the JSON-file-backed fixtureServer in fixture_test.go can't
// express). State-persistence tests use t.TempDir() + FLINE_STATE_HOME to
// keep them hermetic.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Programmable async-server harness
// ---------------------------------------------------------------------------

// asyncFixture is a programmable httptest.Server that lets a test step
// through the analytics request lifecycle (queued → processing → completed
// → failed) by mutating the in-memory model between polls.
type asyncFixture struct {
	srv *httptest.Server

	mu sync.Mutex
	// stoppedDueToInactivity drives the parent-request "stopped" branch.
	stoppedDueToInactivity bool
	// reports is the current set of analyticsReports under the request.
	// Mutate to simulate "more reports just landed".
	reports []reportRow
	// instances are keyed by reportID.
	instances map[ReportID][]instanceRow
	// segments are keyed by instanceID.
	segments map[InstanceID][]segmentRow
	// segmentBlobs are gzipped CSV bodies keyed by signed-URL path.
	segmentBlobs map[string][]byte
	// observedSignedURLAuth records whether the inbound request to the
	// "signed URL" path included an Authorization header — Apple's CDN
	// rejects bearer tokens, so we assert this stays false.
	observedSignedURLAuth atomic.Bool

	// reportListCalls counts GET /v1/analyticsReportRequests/{id}/reports
	// hits so the resume test can assert continuation rather than restart.
	reportListCalls atomic.Int32
	// accessType is echoed on the parent-request response.
	accessType AnalyticsAccessType
	// requestID is the only request the fixture knows about.
	requestID RequestID
}

type reportRow struct {
	ID       ReportID
	Name     string
	Category AnalyticsCategory
}

type instanceRow struct {
	ID             InstanceID
	Granularity    AnalyticsGranularity
	ProcessingDate string
}

type segmentRow struct {
	ID  string
	URL string
}

func newAsyncFixture(t *testing.T) *asyncFixture {
	t.Helper()
	f := &asyncFixture{
		instances:    make(map[ReportID][]instanceRow),
		segments:     make(map[InstanceID][]segmentRow),
		segmentBlobs: make(map[string][]byte),
		accessType:   AccessTypeOneTimeSnapshot,
		requestID:    "REQ-1234",
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// serve is the request router. Each branch takes f.mu briefly to read the
// current model, then writes the JSON envelope. Signed-URL fetches go
// through serveSignedURL and explicitly inspect the inbound Authorization
// header.
func (f *asyncFixture) serve(w http.ResponseWriter, r *http.Request) {
	// Signed-URL CDN path comes back through this same httptest.Server in
	// tests so we can observe the absence of an Authorization header.
	if strings.HasPrefix(r.URL.Path, "/cdn/segments/") {
		f.serveSignedURL(w, r)
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/analyticsReportRequests":
		f.serveCreateRequest(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/analyticsReportRequests/"+string(f.requestID):
		f.serveGetRequest(w)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/analyticsReportRequests/"+string(f.requestID)+"/reports":
		f.reportListCalls.Add(1)
		f.serveListReports(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReports/") && strings.HasSuffix(r.URL.Path, "/instances"):
		f.serveListInstances(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReportInstances/") && strings.HasSuffix(r.URL.Path, "/segments"):
		f.serveListSegments(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/analyticsReportSegments/"):
		f.serveGetSegment(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/salesReports":
		f.serveSalesOrFinance(w)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/financeReports":
		f.serveSalesOrFinance(w)
	default:
		http.Error(w, "no fixture route: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func (f *asyncFixture) serveCreateRequest(w http.ResponseWriter, _ *http.Request) {
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

func (f *asyncFixture) serveGetRequest(w http.ResponseWriter) {
	f.mu.Lock()
	stopped := f.stoppedDueToInactivity
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

func (f *asyncFixture) serveListReports(w http.ResponseWriter) {
	f.mu.Lock()
	rows := append([]reportRow(nil), f.reports...)
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
	body := map[string]any{
		"data":  data,
		"links": map[string]any{},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *asyncFixture) serveListInstances(w http.ResponseWriter, r *http.Request) {
	// /v1/analyticsReports/{id}/instances → extract {id}
	parts := strings.Split(r.URL.Path, "/")
	reportID := ReportID(parts[3])
	f.mu.Lock()
	rows := append([]instanceRow(nil), f.instances[reportID]...)
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
	body := map[string]any{
		"data":  data,
		"links": map[string]any{},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *asyncFixture) serveListSegments(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	instanceID := InstanceID(parts[3])
	f.mu.Lock()
	rows := append([]segmentRow(nil), f.segments[instanceID]...)
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
	body := map[string]any{
		"data":  data,
		"links": map[string]any{},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *asyncFixture) serveGetSegment(w http.ResponseWriter, r *http.Request) {
	// /v1/analyticsReportSegments/{id}
	parts := strings.Split(r.URL.Path, "/")
	segmentID := parts[3]
	// Build a signed URL pointing at the fixture's own /cdn/segments/<id>
	// path so the no-Authorization-header test runs against this server.
	signed := f.srv.URL + "/cdn/segments/" + segmentID
	body := map[string]any{
		"data": map[string]any{
			"type": "analyticsReportSegments",
			"id":   segmentID,
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

// serveSignedURL is the CDN handler. It records whether the inbound request
// carried an Authorization header (it must NOT) and returns a gzipped CSV
// blob keyed by the URL path.
func (f *asyncFixture) serveSignedURL(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		f.observedSignedURLAuth.Store(true)
	}
	f.mu.Lock()
	blob, ok := f.segmentBlobs[r.URL.Path]
	f.mu.Unlock()
	if !ok {
		// Default blob: a gzipped CSV with a single line.
		blob = gzipBytes([]byte("date,impressions\n2026-05-01,42\n"))
	}
	w.Header().Set("Content-Type", "application/a-gzip")
	_, _ = w.Write(blob)
}

func (f *asyncFixture) serveSalesOrFinance(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/a-gzip")
	_, _ = w.Write(gzipBytes([]byte("Provider\tProvider Country\tSKU\nAPPLE\tUS\twidget\n")))
}

func gzipBytes(in []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(in)
	_ = gz.Close()
	return buf.Bytes()
}

// asyncFixtureClient is the equivalent of fixtureClient but pointed at the
// asyncFixture's server. The signed-URL handler runs on the same httptest
// host so we can verify the Authorization-header invariant in-process.
func asyncFixtureClient(t *testing.T, f *asyncFixture) *Client {
	t.Helper()
	keyPath := writeFixtureKey(t)
	c, err := New(Options{
		KeyID:      "TEST123ABC",
		IssuerID:   "11111111-2222-3333-4444-555555555555",
		KeyPath:    keyPath,
		HTTPClient: f.srv.Client(),
		UserAgent:  "flightline-test/1.0",
	})
	if err != nil {
		t.Fatalf("asyncFixtureClient: %v", err)
	}
	c.SetBaseURL(f.srv.URL)
	return c
}

// ---------------------------------------------------------------------------
// RequestAnalyticsReport — happy path + validation
// ---------------------------------------------------------------------------

func TestRequestAnalyticsReport_HappyPath(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	id, err := c.RequestAnalyticsReport(t.Context(), AnalyticsReportRequestParams{
		AppID:      "1234567890",
		AccessType: AccessTypeOneTimeSnapshot,
	})
	if err != nil {
		t.Fatalf("RequestAnalyticsReport: %v", err)
	}
	if id != f.requestID {
		t.Fatalf("got id=%q, want %q", id, f.requestID)
	}
}

func TestRequestAnalyticsReport_Validation(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	tests := []struct {
		name   string
		params AnalyticsReportRequestParams
		want   string
	}{
		{"empty appID", AnalyticsReportRequestParams{AccessType: AccessTypeOneTimeSnapshot}, "AppID is required"},
		{"bogus accessType", AnalyticsReportRequestParams{AppID: "1", AccessType: "garbage"}, "AccessType must be"},
		{"empty accessType", AnalyticsReportRequestParams{AppID: "1"}, "AccessType must be"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.RequestAnalyticsReport(t.Context(), tc.params)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PollAnalyticsReport — lifecycle scenarios
// ---------------------------------------------------------------------------

// fastPoll is the test poll cadence: small enough to keep tests sub-second,
// but non-zero so the backoff branch still exercises select / time.After.
var fastPoll = PollOpts{
	InitialBackoff: 1 * time.Millisecond,
	MaxBackoff:     2 * time.Millisecond,
	Multiplier:     1.0,
	MaxAttempts:    20,
}

func TestPollAnalyticsReport_QueuedThenCompleted(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	// Initially: queued (no reports). After two poll cycles, reports land.
	go func() {
		time.Sleep(3 * time.Millisecond)
		f.mu.Lock()
		f.reports = []reportRow{
			{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
			{ID: "RPT-B", Name: "Commerce", Category: CategoryCommerce},
		}
		f.mu.Unlock()
	}()

	var got []AnalyticsReport
	for r, err := range c.PollAnalyticsReport(t.Context(), f.requestID, fastPoll) {
		if err != nil {
			t.Fatalf("unexpected poll error: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("got %d reports, want 2: %+v", len(got), got)
	}
	if got[0].ID != "RPT-A" || got[1].ID != "RPT-B" {
		t.Fatalf("unexpected report order: %+v", got)
	}
	for _, r := range got {
		if r.RequestID != f.requestID {
			t.Errorf("report %q: RequestID=%q, want %q", r.ID, r.RequestID, f.requestID)
		}
	}
}

func TestPollAnalyticsReport_StoppedDueToInactivity(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	f.accessType = AccessTypeOngoing
	c := asyncFixtureClient(t, f)

	// Flip the stopped flag mid-poll.
	go func() {
		time.Sleep(3 * time.Millisecond)
		f.mu.Lock()
		f.stoppedDueToInactivity = true
		f.mu.Unlock()
	}()

	var sawErr error
	var count int
	for r, err := range c.PollAnalyticsReport(t.Context(), f.requestID, fastPoll) {
		_ = r
		if err != nil {
			sawErr = err
			break
		}
		count++
	}
	if !errors.Is(sawErr, ErrAnalyticsRequestStopped) {
		t.Fatalf("got err=%v, want ErrAnalyticsRequestStopped", sawErr)
	}
	if count != 0 {
		t.Fatalf("yielded %d reports before stop, want 0", count)
	}
}

func TestPollAnalyticsReport_ContextCancel(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before polling starts

	var sawErr error
	for r, err := range c.PollAnalyticsReport(ctx, f.requestID, fastPoll) {
		_ = r
		if err != nil {
			sawErr = err
			break
		}
	}
	if !errors.Is(sawErr, context.Canceled) {
		t.Fatalf("got err=%v, want context.Canceled", sawErr)
	}
}

func TestPollAnalyticsReport_EmptyRequestID(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	var sawErr error
	for r, err := range c.PollAnalyticsReport(t.Context(), "", fastPoll) {
		_ = r
		if err != nil {
			sawErr = err
		}
		break
	}
	if sawErr == nil || !strings.Contains(sawErr.Error(), "empty RequestID") {
		t.Fatalf("got err=%v, want empty-RequestID error", sawErr)
	}
}

func TestPollAnalyticsReport_DedupAcrossPolls(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	f.accessType = AccessTypeOngoing // never auto-terminates
	c := asyncFixtureClient(t, f)

	f.reports = []reportRow{{ID: "RPT-X", Name: "X", Category: CategoryAppUsage}}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	var got []AnalyticsReport
	for r, err := range c.PollAnalyticsReport(ctx, f.requestID, fastPoll) {
		if err != nil {
			break
		}
		got = append(got, r)
	}
	// Same report seen across several polls but the iterator yields it once.
	if len(got) != 1 {
		t.Fatalf("got %d reports, want exactly 1 (dedup): %+v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// ListAnalyticsInstances / ListAnalyticsSegments
// ---------------------------------------------------------------------------

func TestListAnalyticsInstances(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)
	f.instances["RPT-A"] = []instanceRow{
		{ID: "INST-1", Granularity: GranularityDaily, ProcessingDate: "2026-05-01"},
		{ID: "INST-2", Granularity: GranularityWeekly, ProcessingDate: "2026-05-08"},
	}

	got, err := c.ListAnalyticsInstances(t.Context(), "RPT-A")
	if err != nil {
		t.Fatalf("ListAnalyticsInstances: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].ID != "INST-1" || got[0].Granularity != GranularityDaily {
		t.Errorf("instance[0] = %+v", got[0])
	}
	if got[0].ReportID != "RPT-A" {
		t.Errorf("ReportID not back-filled: %q", got[0].ReportID)
	}
}

func TestListAnalyticsInstances_EmptyReportID(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)
	if _, err := c.ListAnalyticsInstances(t.Context(), ""); err == nil {
		t.Fatal("want error for empty ReportID")
	}
}

func TestListAnalyticsSegments(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)
	f.segments["INST-1"] = []segmentRow{
		{ID: "SEG-1", URL: f.srv.URL + "/cdn/segments/SEG-1"},
	}

	got, err := c.ListAnalyticsSegments(t.Context(), "INST-1")
	if err != nil {
		t.Fatalf("ListAnalyticsSegments: %v", err)
	}
	if len(got) != 1 || got[0].ID != "SEG-1" {
		t.Fatalf("unexpected segments: %+v", got)
	}
	if got[0].InstanceID != "INST-1" {
		t.Errorf("InstanceID not back-filled: %q", got[0].InstanceID)
	}
}

// ---------------------------------------------------------------------------
// DownloadAnalyticsSegment — gunzip + no-Authorization-header invariant
// ---------------------------------------------------------------------------

func TestDownloadAnalyticsSegment_NoAuthHeaderOnSignedURL(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	want := []byte("date,impressions\n2026-05-01,42\n")
	f.segmentBlobs["/cdn/segments/SEG-42"] = gzipBytes(want)

	got, err := c.DownloadAnalyticsSegment(t.Context(), "SEG-42")
	if err != nil {
		t.Fatalf("DownloadAnalyticsSegment: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("body: got %q, want %q", got, want)
	}
	if f.observedSignedURLAuth.Load() {
		t.Fatal("signed-URL request carried an Authorization header — must NOT (Apple's CDN rejects)")
	}
}

func TestDownloadAnalyticsSegment_EmptyID(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)
	if _, err := c.DownloadAnalyticsSegment(t.Context(), ""); err == nil {
		t.Fatal("want error for empty segmentID")
	}
}

// ---------------------------------------------------------------------------
// FetchSalesReport / FetchFinanceReport — synchronous gzipped CSV
// ---------------------------------------------------------------------------

func TestFetchSalesReport_GunzipsCSV(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	body, err := c.FetchSalesReport(t.Context(), SalesReportParams{
		VendorNumber:  "12345678",
		ReportType:    SalesReportTypeSales,
		ReportSubType: SalesReportSubTypeSummary,
		Frequency:     SalesFrequencyDaily,
		ReportDate:    "2026-05-01",
	})
	if err != nil {
		t.Fatalf("FetchSalesReport: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("Provider\t")) {
		t.Fatalf("expected TSV header, got %q", body)
	}
}

func TestFetchSalesReport_RejectsMissingFilters(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	tests := []struct {
		name string
		p    SalesReportParams
		want string
	}{
		{"vendor", SalesReportParams{ReportType: SalesReportTypeSales, ReportSubType: SalesReportSubTypeSummary, Frequency: SalesFrequencyDaily}, "VendorNumber"},
		{"reportType", SalesReportParams{VendorNumber: "1", ReportSubType: SalesReportSubTypeSummary, Frequency: SalesFrequencyDaily}, "ReportType"},
		{"reportSubType", SalesReportParams{VendorNumber: "1", ReportType: SalesReportTypeSales, Frequency: SalesFrequencyDaily}, "ReportSubType"},
		{"frequency", SalesReportParams{VendorNumber: "1", ReportType: SalesReportTypeSales, ReportSubType: SalesReportSubTypeSummary}, "Frequency"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.FetchSalesReport(t.Context(), tc.p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestFetchFinanceReport_GunzipsCSV(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	body, err := c.FetchFinanceReport(t.Context(), FinanceReportParams{
		VendorNumber: "12345678",
		ReportType:   FinanceReportTypeFinancial,
		RegionCode:   "US",
		ReportDate:   "2026-04",
	})
	if err != nil {
		t.Fatalf("FetchFinanceReport: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("Provider\t")) {
		t.Fatalf("expected TSV header, got %q", body)
	}
}

func TestFetchFinanceReport_RejectsMissingFilters(t *testing.T) {
	t.Parallel()
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)

	tests := []struct {
		name string
		p    FinanceReportParams
		want string
	}{
		{"vendor", FinanceReportParams{ReportType: FinanceReportTypeFinancial, RegionCode: "US", ReportDate: "2026-04"}, "VendorNumber"},
		{"reportType", FinanceReportParams{VendorNumber: "1", RegionCode: "US", ReportDate: "2026-04"}, "ReportType"},
		{"region", FinanceReportParams{VendorNumber: "1", ReportType: FinanceReportTypeFinancial, ReportDate: "2026-04"}, "RegionCode"},
		{"reportDate", FinanceReportParams{VendorNumber: "1", ReportType: FinanceReportTypeFinancial, RegionCode: "US"}, "ReportDate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.FetchFinanceReport(t.Context(), tc.p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want substring %q", err, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AsyncState — round-trip, atomic write, corruption rejection, schema gate
// ---------------------------------------------------------------------------

// withStateRoot points stateRoot() at t.TempDir() for the duration of the
// test via the FLINE_STATE_HOME escape hatch.
func withStateRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FLINE_STATE_HOME", dir)
	return dir
}

func TestAsyncState_RoundTrip(t *testing.T) {
	root := withStateRoot(t)

	in := AsyncState{
		BundleID:    "com.example.app",
		ReportClass: ReportClassAnalytics,
		RequestID:   "REQ-1",
		SubmittedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		LastPollAt:  time.Date(2026, 5, 1, 12, 5, 0, 0, time.UTC),
		Status:      "processing",
		Reports: []PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		},
		DownloadedSegments: []string{"SEG-1", "SEG-2"},
	}
	if err := PersistAsyncState(in); err != nil {
		t.Fatalf("PersistAsyncState: %v", err)
	}

	got, err := LoadAsyncState("com.example.app", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}

	// SchemaVersion is forced by Persist regardless of input.
	if got.SchemaVersion != AsyncStateSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, AsyncStateSchemaVersion)
	}
	if got.RequestID != in.RequestID {
		t.Errorf("RequestID = %q, want %q", got.RequestID, in.RequestID)
	}
	if !got.SubmittedAt.Equal(in.SubmittedAt) {
		t.Errorf("SubmittedAt = %v, want %v", got.SubmittedAt, in.SubmittedAt)
	}
	if len(got.Reports) != 1 || got.Reports[0].ID != "RPT-A" {
		t.Errorf("Reports = %+v", got.Reports)
	}
	if len(got.DownloadedSegments) != 2 {
		t.Errorf("DownloadedSegments = %+v", got.DownloadedSegments)
	}

	// File is at the expected location and mode 0600.
	expectedPath := filepath.Join(root, "com.example.app", "analytics.json")
	info, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("expected state file at %s: %v", expectedPath, err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("state file mode = %#o, want 0600", mode)
		}
	}
}

func TestAsyncState_LoadMissingReturnsErrNotExist(t *testing.T) {
	withStateRoot(t)
	_, err := LoadAsyncState("com.no.state", ReportClassAnalytics)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got err=%v, want os.ErrNotExist", err)
	}
}

func TestAsyncState_LoadCorruptedRejects(t *testing.T) {
	root := withStateRoot(t)
	dir := filepath.Join(root, "com.example.app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "analytics.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	_, err := LoadAsyncState("com.example.app", ReportClassAnalytics)
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("got err=%v, want ErrStateCorrupt", err)
	}
}

func TestAsyncState_LoadFutureSchemaRejects(t *testing.T) {
	root := withStateRoot(t)
	dir := filepath.Join(root, "com.example.app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	future := fmt.Sprintf(`{"schemaVersion":%d,"bundleId":"com.example.app","reportClass":"analytics"}`, AsyncStateSchemaVersion+1)
	if err := os.WriteFile(filepath.Join(dir, "analytics.json"), []byte(future), 0o600); err != nil {
		t.Fatalf("write future state: %v", err)
	}

	_, err := LoadAsyncState("com.example.app", ReportClassAnalytics)
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("got err=%v, want ErrStateCorrupt", err)
	}
}

func TestAsyncState_PersistRejectsBadBundleID(t *testing.T) {
	withStateRoot(t)
	bad := []string{"", "../escape", "with/slash", "with\\backslash", "..", "."}
	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			err := PersistAsyncState(AsyncState{
				BundleID:    id,
				ReportClass: ReportClassAnalytics,
				SubmittedAt: time.Now().UTC(),
			})
			if err == nil {
				t.Fatalf("PersistAsyncState accepted bundleID=%q", id)
			}
		})
	}
}

func TestAsyncState_PersistRejectsBadReportClass(t *testing.T) {
	withStateRoot(t)
	err := PersistAsyncState(AsyncState{
		BundleID:    "com.example.app",
		ReportClass: "garbage",
		SubmittedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("want error for unknown report class")
	}
}

func TestAsyncState_AtomicRenamePreservesOriginalOnFailure(t *testing.T) {
	root := withStateRoot(t)
	original := AsyncState{
		BundleID:    "com.example.app",
		ReportClass: ReportClassAnalytics,
		Status:      "completed",
		SubmittedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := PersistAsyncState(original); err != nil {
		t.Fatalf("seed Persist: %v", err)
	}

	// Simulate a mid-write Ctrl-C by leaving a stale .tmp-* file in the
	// directory before retrying — the next Persist should clean up after
	// itself and the original file should be unaffected.
	dir := filepath.Join(root, "com.example.app")
	stale, err := os.CreateTemp(dir, "analytics.json.tmp-stale-*")
	if err != nil {
		t.Fatalf("create stale: %v", err)
	}
	_, _ = stale.WriteString("partial garbage")
	_ = stale.Close()

	// Re-load should still succeed — the stale temp file does NOT shadow
	// the canonical analytics.json.
	got, err := LoadAsyncState("com.example.app", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState after stale temp: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want \"completed\" — original was clobbered", got.Status)
	}
}

func TestAsyncState_ResumeAfterCtrlC(t *testing.T) {
	withStateRoot(t)

	// 1. First run: persist progress mid-flow with two reports already
	//    yielded and one segment downloaded.
	saved := AsyncState{
		BundleID:    "com.example.app",
		ReportClass: ReportClassAnalytics,
		RequestID:   "REQ-1234",
		SubmittedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		LastPollAt:  time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC),
		Status:      "processing",
		Reports: []PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
			{ID: "RPT-B", Name: "Commerce", Category: CategoryCommerce},
		},
		DownloadedSegments: []string{"SEG-1"},
	}
	if err := PersistAsyncState(saved); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// 2. Ctrl-C, fresh process: load the state and verify the resume info
	//    is intact so the caller can skip already-fetched work.
	loaded, err := LoadAsyncState("com.example.app", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("Load after Ctrl-C: %v", err)
	}
	if loaded.RequestID != "REQ-1234" {
		t.Errorf("RequestID lost across resume: %q", loaded.RequestID)
	}
	if len(loaded.Reports) != 2 {
		t.Errorf("Reports lost: %+v", loaded.Reports)
	}
	already := map[string]struct{}{}
	for _, s := range loaded.DownloadedSegments {
		already[s] = struct{}{}
	}
	if _, ok := already["SEG-1"]; !ok {
		t.Errorf("DownloadedSegments missing SEG-1: %+v", loaded.DownloadedSegments)
	}
}

func TestStateFilePath_Composition(t *testing.T) {
	withStateRoot(t)
	got, err := StateFilePath("com.example.app", ReportClassSales)
	if err != nil {
		t.Fatalf("StateFilePath: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("com.example.app", "sales.json")) {
		t.Errorf("path = %q, want suffix com.example.app/sales.json", got)
	}
}

// ---------------------------------------------------------------------------
// Resume continuation: prove a fresh client + saved state continues from
// where the previous client stopped, rather than restarting from zero.
// ---------------------------------------------------------------------------

func TestPollAnalyticsReport_ResumeContinuesFromSavedReports(t *testing.T) {
	// No t.Parallel — withStateRoot uses t.Setenv which is incompatible
	// with parallel tests in the same scope.

	// First "session": persist state with RPT-A already yielded.
	withStateRoot(t)
	if err := PersistAsyncState(AsyncState{
		BundleID:    "com.example.app",
		ReportClass: ReportClassAnalytics,
		RequestID:   "REQ-1234",
		SubmittedAt: time.Now().UTC().Add(-time.Hour),
		Status:      "processing",
		Reports: []PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		},
		DownloadedSegments: []string{"SEG-1"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Second "session": fresh fixture + fresh client. The fixture exposes
	// RPT-A and RPT-B; the resume path should treat RPT-A as already-seen
	// and yield only RPT-B, then complete.
	f := newAsyncFixture(t)
	c := asyncFixtureClient(t, f)
	f.reports = []reportRow{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "Commerce", Category: CategoryCommerce},
	}

	saved, err := LoadAsyncState("com.example.app", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	already := map[ReportID]struct{}{}
	for _, r := range saved.Reports {
		already[r.ID] = struct{}{}
	}

	var fresh []AnalyticsReport
	for r, perr := range c.PollAnalyticsReport(t.Context(), saved.RequestID, fastPoll) {
		if perr != nil {
			t.Fatalf("poll: %v", perr)
		}
		if _, dup := already[r.ID]; dup {
			continue
		}
		fresh = append(fresh, r)
	}

	if len(fresh) != 1 || fresh[0].ID != "RPT-B" {
		t.Fatalf("resume yielded %+v, want only RPT-B", fresh)
	}
}

// ---------------------------------------------------------------------------
// Verify that DownloadAnalyticsSegment surfaces a typed APIError when the
// initial GET fails (defensive — keep the error envelope contract).
// ---------------------------------------------------------------------------

func TestDownloadAnalyticsSegment_PropagatesAPIError(t *testing.T) {
	t.Parallel()
	// A bare httptest.Server that always returns 403 with an Apple-shaped
	// errors[] body so we can assert the typed envelope flows through.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"errors":[{"id":"e1","status":"403","code":"FORBIDDEN","title":"Forbidden","detail":"Insufficient permission"}]}`)
	}))
	t.Cleanup(srv.Close)

	keyPath := writeFixtureKey(t)
	c, err := New(Options{
		KeyID: "TEST123ABC", IssuerID: "11111111-2222-3333-4444-555555555555",
		KeyPath: keyPath, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetBaseURL(srv.URL)

	_, err = c.DownloadAnalyticsSegment(t.Context(), "SEG-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got err=%v, want *APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusForbidden {
		t.Errorf("HTTPStatus=%d, want 403", apiErr.HTTPStatus)
	}
}
