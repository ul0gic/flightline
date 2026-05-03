package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func resetFinanceFlags() {
	financeMonth = ""
	financeYear = ""
	financeRegion = "Z1"
	financeReportType = string(asc.FinanceReportTypeFinancial)
}

func TestFinance_RegisteredOnRoot(t *testing.T) {
	var found *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "finance" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("finance not registered on rootCmd")
	}
	for _, want := range []string{"month", "year", "region", "report-type"} {
		if found.Flag(want) == nil {
			t.Errorf("finance: missing flag --%s", want)
		}
	}
}

func TestBuildFinanceDate_Month(t *testing.T) {
	resetFinanceFlags()
	financeMonth = "2026-04"
	d, freq, err := buildFinanceDate()
	if err != nil {
		t.Fatalf("buildFinanceDate: %v", err)
	}
	if d != "2026-04" || freq != "MONTHLY" {
		t.Errorf("got date=%q freq=%q, want 2026-04 MONTHLY", d, freq)
	}
}

func TestBuildFinanceDate_Year(t *testing.T) {
	resetFinanceFlags()
	financeYear = "2026"
	d, freq, err := buildFinanceDate()
	if err != nil {
		t.Fatalf("buildFinanceDate: %v", err)
	}
	if d != "2026" || freq != "YEARLY" {
		t.Errorf("got date=%q freq=%q, want 2026 YEARLY", d, freq)
	}
}

func TestBuildFinanceDate_RejectsConflict(t *testing.T) {
	resetFinanceFlags()
	financeMonth = "2026-04"
	financeYear = "2026"
	if _, _, err := buildFinanceDate(); err == nil {
		t.Fatal("want error for --month + --year")
	}
}

func TestBuildFinanceDate_RejectsMissing(t *testing.T) {
	resetFinanceFlags()
	if _, _, err := buildFinanceDate(); err == nil {
		t.Fatal("want error when neither --month nor --year set")
	}
}

func TestBuildFinanceDate_RejectsBadFormats(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{"month not YYYY-MM", func() { resetFinanceFlags(); financeMonth = "2026" }},
		{"year not YYYY", func() { resetFinanceFlags(); financeYear = "26" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			if _, _, err := buildFinanceDate(); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestFinanceRowMatchesBundle(t *testing.T) {
	tests := []struct {
		name   string
		row    asc.FinanceReportRow
		bundle string
		want   bool
	}{
		{"vendor matches exactly", asc.FinanceReportRow{VendorIdentifier: "com.example.app"}, "com.example.app", true},
		{"vendor prefix match", asc.FinanceReportRow{VendorIdentifier: "com.example.app.iap1"}, "com.example.app", true},
		{"different vendor", asc.FinanceReportRow{VendorIdentifier: "com.other.app"}, "com.example.app", false},
		{"empty bundle matches all", asc.FinanceReportRow{VendorIdentifier: "anything"}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := financeRowMatchesBundle(&tc.row, tc.bundle)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSummarizeFinanceByRegion(t *testing.T) {
	rows := []asc.FinanceReportRow{
		{CountryOfSale: "US", PartnerShareCurrency: "USD", Quantity: 5, PartnerShare: 2.10, ExtendedPartnerShare: 10.50},
		{CountryOfSale: "US", PartnerShareCurrency: "USD", Quantity: 3, PartnerShare: 1.26, ExtendedPartnerShare: 3.78},
		{CountryOfSale: "GB", PartnerShareCurrency: "USD", Quantity: 2, PartnerShare: 0.99, ExtendedPartnerShare: 1.98},
	}
	got := summarizeFinanceByRegion(rows)
	if len(got) != 2 {
		t.Fatalf("summary rows = %d, want 2 (2 distinct countries)", len(got))
	}
	if got[0].CountryOfSale != "GB" {
		t.Errorf("got[0].CountryOfSale = %q, want GB (sorted)", got[0].CountryOfSale)
	}
	if got[1].CountryOfSale != "US" || got[1].Quantity != 8 {
		t.Errorf("got[1] = %+v, want US/qty=8", got[1])
	}
}

func TestFinanceReport_TableRowsAndJSON(t *testing.T) {
	rep := FinanceReport{
		BundleID:     "com.example.testapp",
		VendorNumber: "99999999",
		ReportType:   "FINANCIAL",
		Frequency:    "MONTHLY",
		ReportDate:   "2026-04",
		RegionCode:   "US",
		RowCount:     1,
		Rows: []asc.FinanceReportRow{
			{VendorIdentifier: "com.example.testapp.sku1", Quantity: 5, PartnerShare: 2.10, CountryOfSale: "US", PartnerShareCurrency: "USD"},
		},
		Summary: []FinanceRegionSummary{
			{CountryOfSale: "US", Currency: "USD", Quantity: 5, PartnerShare: 2.10, ExtendedPartnerShare: 10.50},
		},
	}
	headers, rows := rep.TableRows()
	if headers[0] != "COUNTRY" {
		t.Errorf("headers[0] = %q, want COUNTRY", headers[0])
	}
	if len(rows) != 1 || rows[0][0] != "US" {
		t.Errorf("rows = %+v", rows)
	}

	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"reportType":"FINANCIAL"`,
		`"frequency":"MONTHLY"`,
		`"regionCode":"US"`,
		`"summary":`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %q: %s", want, b)
		}
	}
}

func TestFetchFinanceReport_DecodesAndFilters(t *testing.T) {
	srv := gzReportServer(t, map[string]string{
		"/v1/financeReports": "finance/monthly_basic.tsv",
	})
	c := fixtureASCClient(t, srv)

	rows, raw, err := fetchFinanceReport(context.Background(), c, financeFetchOpts{
		vendor:      "99999999",
		reportType:  asc.FinanceReportTypeFinancial,
		region:      "Z1",
		reportDate:  "2026-04",
		bundleID:    "com.example.testapp",
		captureRows: true,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("raw = %d bytes, want 0 when captureRows", len(raw))
	}
	// Fixture has 4 rows, all matching the bundleId via VendorIdentifier prefix.
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
}

func TestFetchFinanceReport_RawCapturePassthrough(t *testing.T) {
	srv := gzReportServer(t, map[string]string{
		"/v1/financeReports": "finance/monthly_basic.tsv",
	})
	c := fixtureASCClient(t, srv)

	_, raw, err := fetchFinanceReport(context.Background(), c, financeFetchOpts{
		vendor:     "99999999",
		reportType: asc.FinanceReportTypeFinancial,
		region:     "Z1",
		reportDate: "2026-04",
		bundleID:   "com.example.testapp",
		captureRaw: true,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("Start Date\t")) {
		t.Errorf("raw passthrough does not start with Apple finance TSV header: %q", raw[:min(40, len(raw))])
	}
}

func TestFetchFinanceReport_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"id":"e1","status":"401","code":"NOT_AUTHORIZED","title":"Unauthorized","detail":"bad jwt"}]}`))
	}))
	t.Cleanup(srv.Close)
	c := fixtureASCClient(t, srv)

	_, _, err := fetchFinanceReport(context.Background(), c, financeFetchOpts{
		vendor:      "99999999",
		reportType:  asc.FinanceReportTypeFinancial,
		region:      "Z1",
		reportDate:  "2026-04",
		bundleID:    "com.example.testapp",
		captureRows: true,
	})
	if err == nil {
		t.Fatal("want error for 401 from Apple")
	}
	if !strings.Contains(err.Error(), "NOT_AUTHORIZED") {
		t.Errorf("err = %v, want to contain Apple's NOT_AUTHORIZED", err)
	}
}
