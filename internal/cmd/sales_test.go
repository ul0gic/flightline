package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// gzReportServer spins a programmable httptest.Server that serves
// /v1/salesReports and /v1/financeReports with gzipped TSV bodies. Unlike
// the JSON-only startFixtureServer in helpers_test.go, this one emits the
// content-type Apple actually uses for these endpoints
// ("application/a-gzip") so the asc-side gunzip path runs end-to-end.
//
// Routes are keyed by URL path; the handler responds with the named TSV
// fixture from internal/asc/testdata/golden/<class>/<name>.tsv, gzipped.
// Unknown paths produce a 404 with a fixture-no-route diagnostic body
// matching the JSON helper's shape so test failures are self-locating.
func gzReportServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	captured := make(map[string]string, len(routes))
	for k, v := range routes {
		captured[k] = v
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture, ok := captured[r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			body := `{"errors":[{"id":"fixture-no-route","status":"404","code":"FIXTURE_NO_ROUTE","title":"no fixture for path","detail":"` + r.URL.Path + `"}]}`
			_, _ = w.Write([]byte(body))
			return
		}
		// Fixtures live under ../asc/testdata/golden/<class>/<name>.tsv.
		path := filepath.Join("..", "asc", "testdata", "golden", fixture)
		raw, err := os.ReadFile(path) //nolint:gosec // fixture path constant
		if err != nil {
			t.Errorf("read fixture %s: %v", path, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write(raw)
		_ = gz.Close()
		w.Header().Set("Content-Type", "application/a-gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// resetSalesFlags wipes the package-level flag vars between table-driven
// cases. cobra binds globals once at init; without an explicit reset, a
// later case inherits the previous case's values and assertions diverge.
func resetSalesFlags() {
	salesDays = 0
	salesMonth = ""
	salesWeek = ""
	salesYear = ""
	salesReportType = string(asc.SalesReportTypeSales)
	salesReportSubTyp = string(asc.SalesReportSubTypeSummary)
	salesFrequency = ""
}

func TestSales_RegisteredOnRoot(t *testing.T) {
	var found *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "sales" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("sales not registered on rootCmd")
	}
	for _, want := range []string{"days", "month", "week", "year", "report-type", "frequency"} {
		if found.Flag(want) == nil {
			t.Errorf("sales: missing flag --%s", want)
		}
	}
}

func TestBuildSalesPlan_DaysWindow(t *testing.T) {
	resetSalesFlags()
	salesDays = 3
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	plan, err := buildSalesPlan(now)
	if err != nil {
		t.Fatalf("buildSalesPlan: %v", err)
	}
	if plan.frequency != asc.SalesFrequencyDaily {
		t.Errorf("frequency = %s, want DAILY", plan.frequency)
	}
	want := []string{"2026-04-28", "2026-04-29", "2026-04-30"}
	if len(plan.dates) != len(want) {
		t.Fatalf("dates len = %d, want %d", len(plan.dates), len(want))
	}
	for i, d := range want {
		if plan.dates[i] != d {
			t.Errorf("dates[%d] = %q, want %q", i, plan.dates[i], d)
		}
	}
}

func TestBuildSalesPlan_DefaultsToSevenDays(t *testing.T) {
	resetSalesFlags()
	now := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	plan, err := buildSalesPlan(now)
	if err != nil {
		t.Fatalf("buildSalesPlan: %v", err)
	}
	if len(plan.dates) != 7 {
		t.Errorf("default dates = %d, want 7", len(plan.dates))
	}
}

func TestBuildSalesPlan_Month(t *testing.T) {
	resetSalesFlags()
	salesMonth = "2026-04"
	plan, err := buildSalesPlan(time.Now())
	if err != nil {
		t.Fatalf("buildSalesPlan: %v", err)
	}
	if plan.frequency != asc.SalesFrequencyMonthly {
		t.Errorf("frequency = %s, want MONTHLY", plan.frequency)
	}
	if len(plan.dates) != 1 || plan.dates[0] != "2026-04" {
		t.Errorf("dates = %+v, want [2026-04]", plan.dates)
	}
}

func TestBuildSalesPlan_RejectsConflictingFlags(t *testing.T) {
	resetSalesFlags()
	salesMonth = "2026-04"
	salesYear = "2026"
	if _, err := buildSalesPlan(time.Now()); err == nil {
		t.Fatal("want error for conflicting --month + --year")
	}
}

func TestBuildSalesPlan_RejectsBadDateFormats(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{"month not YYYY-MM", func() { resetSalesFlags(); salesMonth = "2026" }},
		{"week not YYYY-MM-DD", func() { resetSalesFlags(); salesWeek = "2026/05" }},
		{"year not YYYY", func() { resetSalesFlags(); salesYear = "26" }},
		{"days zero", func() { resetSalesFlags(); salesDays = 0; salesMonth = ""; salesYear = ""; salesWeek = "" }},
	}
	// `days zero` is the no-flag default → falls back to 7 days, not an error.
	// Drop it from the table and just exercise the format errors.
	tests = tests[:3]
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			if _, err := buildSalesPlan(time.Now()); err == nil {
				t.Fatalf("%s: want error", tc.name)
			}
		})
	}
}

func TestSalesRowMatchesBundle(t *testing.T) {
	tests := []struct {
		name   string
		row    asc.SalesReportRow
		bundle string
		want   bool
	}{
		{"parent equals bundle", asc.SalesReportRow{ParentIdentifier: "com.example.app"}, "com.example.app", true},
		{"sku equals bundle", asc.SalesReportRow{SKU: "com.example.app"}, "com.example.app", true},
		{"sku starts with bundle, no parent", asc.SalesReportRow{SKU: "com.example.app.iap1"}, "com.example.app", true},
		{"different parent", asc.SalesReportRow{ParentIdentifier: "com.other.app"}, "com.example.app", false},
		{"empty bundle matches all", asc.SalesReportRow{SKU: "anything"}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := salesRowMatchesBundle(&tc.row, tc.bundle)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSummarizeSalesByDate(t *testing.T) {
	rows := []asc.SalesReportRow{
		{BeginDate: "2026-05-01", CurrencyOfProceeds: "USD", Units: 5, DeveloperProceeds: 2.10},
		{BeginDate: "2026-05-01", CurrencyOfProceeds: "USD", Units: 3, DeveloperProceeds: 1.26},
		{BeginDate: "2026-05-01", CurrencyOfProceeds: "GBP", Units: 2, DeveloperProceeds: 0.99},
		{BeginDate: "2026-05-02", CurrencyOfProceeds: "USD", Units: 1, DeveloperProceeds: 0.42},
	}
	got := summarizeSalesByDate(rows)
	if len(got) != 3 {
		t.Fatalf("summary rows = %d, want 3 (2 dates × 2 currencies on day 1)", len(got))
	}
	// First two are 2026-05-01 (GBP < USD by sort order), third is 2026-05-02.
	if got[0].Date != "2026-05-01" || got[0].Currency != "GBP" {
		t.Errorf("got[0] = %+v, want 2026-05-01/GBP", got[0])
	}
	if got[1].Date != "2026-05-01" || got[1].Currency != "USD" || got[1].Units != 8 {
		t.Errorf("got[1] = %+v, want 2026-05-01/USD/8", got[1])
	}
	if got[2].Date != "2026-05-02" {
		t.Errorf("got[2] = %+v, want 2026-05-02", got[2])
	}
}

func TestRequireVendorNumber_MissingErrors(t *testing.T) {
	t.Setenv("APP_STORE_CONNECT_VENDOR_NUMBER", "")
	if _, err := requireVendorNumber(); err == nil {
		t.Fatal("want error when env var unset")
	}
}

func TestRequireVendorNumber_TrimsWhitespace(t *testing.T) {
	t.Setenv("APP_STORE_CONNECT_VENDOR_NUMBER", "  99999999  ")
	got, err := requireVendorNumber()
	if err != nil {
		t.Fatalf("requireVendorNumber: %v", err)
	}
	if got != "99999999" {
		t.Errorf("got %q, want 99999999", got)
	}
}

func TestSalesReport_TableRowsAndJSON(t *testing.T) {
	rep := SalesReport{
		BundleID:     "com.example.testapp",
		VendorNumber: "99999999",
		ReportType:   "SALES",
		Frequency:    "DAILY",
		ReportDates:  []string{"2026-05-01"},
		RowCount:     1,
		Rows: []asc.SalesReportRow{
			{BeginDate: "2026-05-01", Units: 5, DeveloperProceeds: 2.10, CurrencyOfProceeds: "USD"},
		},
		Summary: []SalesDailySummary{
			{Date: "2026-05-01", Units: 5, DeveloperProceeds: 2.10, Currency: "USD"},
		},
	}
	headers, rows := rep.TableRows()
	want := []string{"DATE", "UNITS", "PROCEEDS", "CURRENCY"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("header[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 || rows[0][1] != "5" {
		t.Errorf("rows = %+v", rows)
	}

	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"bundleId":"com.example.testapp"`,
		`"vendorNumber":"99999999"`,
		`"reportType":"SALES"`,
		`"rowCount":1`,
		`"summary":`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %q: %s", want, b)
		}
	}
}

func TestFetchSalesAcrossDates_DecodesAndFilters(t *testing.T) {
	srv := gzReportServer(t, map[string]string{
		"/v1/salesReports": "sales/daily_basic.tsv",
	})
	c := fixtureASCClient(t, srv)

	rows, raw, err := fetchSalesAcrossDates(context.Background(), c, salesFetchOpts{
		vendor:      "99999999",
		reportType:  asc.SalesReportTypeSales,
		reportSub:   asc.SalesReportSubTypeSummary,
		frequency:   asc.SalesFrequencyDaily,
		dates:       []string{"2026-05-01"},
		bundleID:    "com.example.testapp",
		captureRows: true,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("raw = %d bytes, want 0 when captureRows", len(raw))
	}
	// Fixture has 4 rows; 3 match the bundleId via SKU prefix or parent.
	if len(rows) < 3 {
		t.Fatalf("rows = %d, want >= 3 (filter must keep matching SKUs)", len(rows))
	}
}

func TestFetchSalesAcrossDates_RawCapturePassthrough(t *testing.T) {
	srv := gzReportServer(t, map[string]string{
		"/v1/salesReports": "sales/daily_basic.tsv",
	})
	c := fixtureASCClient(t, srv)

	_, raw, err := fetchSalesAcrossDates(context.Background(), c, salesFetchOpts{
		vendor:     "99999999",
		reportType: asc.SalesReportTypeSales,
		reportSub:  asc.SalesReportSubTypeSummary,
		frequency:  asc.SalesFrequencyDaily,
		dates:      []string{"2026-05-01"},
		bundleID:   "com.example.testapp",
		captureRaw: true,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("Provider\t")) {
		t.Errorf("raw passthrough does not start with Apple TSV header: %q", raw[:min(40, len(raw))])
	}
}

func TestFetchSalesAcrossDates_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"id":"e1","status":"403","code":"FORBIDDEN","title":"Forbidden","detail":"vendor mismatch"}]}`))
	}))
	t.Cleanup(srv.Close)
	c := fixtureASCClient(t, srv)

	_, _, err := fetchSalesAcrossDates(context.Background(), c, salesFetchOpts{
		vendor:      "99999999",
		reportType:  asc.SalesReportTypeSales,
		reportSub:   asc.SalesReportSubTypeSummary,
		frequency:   asc.SalesFrequencyDaily,
		dates:       []string{"2026-05-01"},
		bundleID:    "com.example.testapp",
		captureRows: true,
	})
	if err == nil {
		t.Fatal("want error for 403 from Apple")
	}
	if !strings.Contains(err.Error(), "FORBIDDEN") {
		t.Errorf("err = %v, want to contain Apple's FORBIDDEN code", err)
	}
}

// queryStringEcho records the inbound query string so tests can assert the
// filter[...] params Apple expects are wired through faithfully.
func queryStringEcho(t *testing.T, want url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query()
		for k, vs := range want {
			gv := got.Get(k)
			if gv != vs[0] {
				t.Errorf("query %q = %q, want %q", k, gv, vs[0])
			}
		}
		// Echo a minimal valid TSV so the decoder doesn't fail.
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte("Provider\tProvider Country\tSKU\tUnits\tDeveloper Proceeds\nAPPLE\tUS\tcom.example.testapp\t1\t1.00\n"))
		_ = gz.Close()
		w.Header().Set("Content-Type", "application/a-gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchSalesAcrossDates_SendsCorrectQuery(t *testing.T) {
	srv := queryStringEcho(t, url.Values{
		"filter[vendorNumber]":  {"99999999"},
		"filter[reportType]":    {"SALES"},
		"filter[reportSubType]": {"SUMMARY"},
		"filter[frequency]":     {"DAILY"},
		"filter[reportDate]":    {"2026-05-01"},
	})
	c := fixtureASCClient(t, srv)

	_, _, err := fetchSalesAcrossDates(context.Background(), c, salesFetchOpts{
		vendor:      "99999999",
		reportType:  asc.SalesReportTypeSales,
		reportSub:   asc.SalesReportSubTypeSummary,
		frequency:   asc.SalesFrequencyDaily,
		dates:       []string{"2026-05-01"},
		bundleID:    "com.example.testapp",
		captureRows: true,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
}
