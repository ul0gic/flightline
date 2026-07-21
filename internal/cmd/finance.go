package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// FinanceReport is the JSON contract for `finance`.
type FinanceReport struct {
	BundleID     string                 `json:"bundleId"`
	VendorNumber string                 `json:"vendorNumber"`
	ReportType   string                 `json:"reportType"`
	Frequency    string                 `json:"frequency"`
	ReportDate   string                 `json:"reportDate"`
	PeriodStart  string                 `json:"periodStart,omitempty"`
	PeriodEnd    string                 `json:"periodEnd,omitempty"`
	RegionCode   string                 `json:"regionCode"`
	RowCount     int                    `json:"rowCount"`
	Rows         []asc.FinanceReportRow `json:"rows"`
	Summary      []FinanceRegionSummary `json:"summary"`
	Note         string                 `json:"note,omitempty"`
}

// FinanceRegionSummary collapses rows by (CountryOfSale, PartnerShareCurrency),
// the unit Apple settles in a single currency.
type FinanceRegionSummary struct {
	CountryOfSale        string  `json:"countryOfSale"`
	Currency             string  `json:"currency"`
	Quantity             int     `json:"quantity"`
	PartnerShare         float64 `json:"partnerShare"`
	ExtendedPartnerShare float64 `json:"extendedPartnerShare"`
}

func (r FinanceReport) TableRows() (headers []string, rows [][]string) {
	headers = []string{"PERIOD", "COUNTRY", "CURRENCY", "QTY", "PARTNER_SHARE", "EXT_PARTNER_SHARE"}
	if len(r.Summary) == 0 {
		return headers, nil
	}
	rows = make([][]string, 0, len(r.Summary))
	for _, s := range r.Summary {
		rows = append(rows, []string{
			financePeriodLabel(r.PeriodStart, r.PeriodEnd),
			s.CountryOfSale,
			s.Currency,
			strconv.Itoa(s.Quantity),
			strconv.FormatFloat(s.PartnerShare, 'f', 2, 64),
			strconv.FormatFloat(s.ExtendedPartnerShare, 'f', 2, 64),
		})
	}
	return headers, rows
}

var financeCmd = &cobra.Command{
	Use:   "finance <bundleId>",
	Short: "Fetch App Store Connect finance (settlement) reports",
	Long: `finance pulls finance/settlement reports from /v1/financeReports.

Apple indexes finance reports by fiscal year/month, not calendar month.
Daily granularity belongs to ` + "`flightline sales`" + `. FINANCIAL defaults
to the consolidated region ZZ. FINANCE_DETAIL defaults to Z1.

The bundleId argument filters typed output by Vendor Identifier so a single-
vendor multi-app account stays focused. --output tsv streams Apple's raw
wire format unfiltered.

Vendor number is read from APP_STORE_CONNECT_VENDOR_NUMBER.`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runFinance,
	Example: `  flightline finance com.example.myapp --month 2026-04
	  flightline finance com.example.myapp --month 2026-04 --region US
	  flightline finance com.example.myapp --month 2026-04 --report-type FINANCE_DETAIL
  flightline finance com.example.myapp --month 2026-04 --output json | jq '.summary'
  flightline finance com.example.myapp --month 2026-04 --output tsv > finance.tsv`,
}

var (
	financeMonth      string
	financeRegion     string
	financeReportType string
)

func init() {
	financeCmd.Flags().StringVar(&financeMonth, "month", "", "Apple fiscal year/month in YYYY-MM format")
	financeCmd.Flags().StringVar(&financeRegion, "region", "", "financial region code (default: ZZ for FINANCIAL, Z1 for FINANCE_DETAIL)")
	financeCmd.Flags().StringVar(&financeReportType, "report-type", string(asc.FinanceReportTypeFinancial),
		"reportType filter (FINANCIAL or FINANCE_DETAIL)")

	rootCmd.AddCommand(financeCmd)
}

func runFinance(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	vendor, err := requireVendorNumber()
	if err != nil {
		return err
	}
	reportDate, freq, err := buildFinanceDate()
	if err != nil {
		return err
	}
	reportType, region, err := resolveFinanceScope(financeReportType, financeRegion)
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	app, err := resolveApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	mode := outputMode()
	rawMode := mode == "tsv"

	fetched, err := fetchFinanceReport(cmd.Context(), c, financeFetchOpts{
		vendor:      vendor,
		reportType:  reportType,
		region:      region,
		reportDate:  reportDate,
		bundleID:    app.Attributes.BundleID,
		captureRaw:  rawMode,
		captureRows: !rawMode,
	})
	if err != nil {
		return err
	}

	if rawMode {
		if _, err := os.Stdout.Write(fetched.raw); err != nil {
			return fmt.Errorf("finance: write tsv: %w", err)
		}
		return nil
	}

	periodStart, periodEnd := financeReportPeriod(fetched.rows)
	report := FinanceReport{
		BundleID:     app.Attributes.BundleID,
		VendorNumber: vendor,
		ReportType:   string(reportType),
		Frequency:    freq,
		ReportDate:   reportDate,
		RegionCode:   region,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
		RowCount:     len(fetched.rows),
		Rows:         fetched.rows,
		Summary:      summarizeFinanceByRegion(fetched.rows),
	}
	if fetched.unavailable {
		report.Note = "Apple has no financial report for this fiscal period"
	}
	return Render(report, mode)
}

type financeFetchResult struct {
	rows        []asc.FinanceReportRow
	raw         []byte
	unavailable bool
}

type financeFetchOpts struct {
	vendor      string
	reportType  asc.FinanceReportType
	region      string
	reportDate  string
	bundleID    string
	captureRaw  bool
	captureRows bool
}

// fetchFinanceReport returns either typed rows (filtered by VendorIdentifier
// prefix) or raw TSV bytes, per the capture flags.
func fetchFinanceReport(ctx context.Context, c *asc.Client, opts financeFetchOpts) (financeFetchResult, error) {
	body, err := c.FetchFinanceReport(ctx, asc.FinanceReportParams{
		VendorNumber: opts.vendor,
		ReportType:   opts.reportType,
		RegionCode:   opts.region,
		ReportDate:   opts.reportDate,
	})
	if err != nil {
		if isExpectedNoReport(err) {
			return financeFetchResult{rows: []asc.FinanceReportRow{}, raw: []byte{}, unavailable: true}, nil
		}
		return financeFetchResult{}, fmt.Errorf("finance: fetch %s/%s: %w", opts.region, opts.reportDate, err)
	}
	if opts.captureRaw {
		return financeFetchResult{rows: []asc.FinanceReportRow{}, raw: body}, nil
	}
	if !opts.captureRows {
		return financeFetchResult{rows: []asc.FinanceReportRow{}, raw: []byte{}}, nil
	}
	decoded, err := asc.DecodeFinanceTSV(body)
	if err != nil {
		return financeFetchResult{}, fmt.Errorf("finance: decode %s/%s: %w", opts.region, opts.reportDate, err)
	}
	rows := make([]asc.FinanceReportRow, 0, len(decoded))
	for i := range decoded {
		if financeRowMatchesBundle(&decoded[i], opts.bundleID) {
			rows = append(rows, decoded[i])
		}
	}
	return financeFetchResult{rows: rows, raw: []byte{}}, nil
}

// financeRowMatchesBundle matches against the Vendor Identifier column (the
// SKU/bundle in finance reports). Empty bundleId matches everything.
func financeRowMatchesBundle(r *asc.FinanceReportRow, bundleID string) bool {
	if bundleID == "" {
		return true
	}
	if r.VendorIdentifier == bundleID {
		return true
	}
	if strings.HasPrefix(r.VendorIdentifier, bundleID) {
		return true
	}
	return false
}

// summarizeFinanceByRegion folds rows by (CountryOfSale, PartnerShareCurrency),
// sorted for stable output.
func summarizeFinanceByRegion(rows []asc.FinanceReportRow) []FinanceRegionSummary {
	type key struct{ country, ccy string }
	agg := make(map[key]*FinanceRegionSummary)
	for i := range rows {
		r := &rows[i]
		k := key{country: r.CountryOfSale, ccy: r.PartnerShareCurrency}
		s, ok := agg[k]
		if !ok {
			s = &FinanceRegionSummary{CountryOfSale: k.country, Currency: k.ccy}
			agg[k] = s
		}
		s.Quantity += r.Quantity
		s.PartnerShare += r.PartnerShare
		s.ExtendedPartnerShare += r.ExtendedPartnerShare
	}
	out := make([]FinanceRegionSummary, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CountryOfSale != out[j].CountryOfSale {
			return out[i].CountryOfSale < out[j].CountryOfSale
		}
		return out[i].Currency < out[j].Currency
	})
	return out
}

// buildFinanceDate returns Apple's fiscal reportDate. The API accepts YYYY-MM only.
func buildFinanceDate() (reportDate, frequency string, err error) {
	month := strings.TrimSpace(financeMonth)
	if month == "" {
		return "", "", errors.New("finance: --month YYYY-MM is required (Apple fiscal year/month)")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		return "", "", fmt.Errorf("finance: --month must be YYYY-MM, got %q", financeMonth)
	}
	return month, "MONTHLY", nil
}

func resolveFinanceScope(rawType, rawRegion string) (asc.FinanceReportType, string, error) {
	reportType := asc.FinanceReportType(strings.ToUpper(strings.TrimSpace(rawType)))
	region := strings.ToUpper(strings.TrimSpace(rawRegion))
	switch reportType {
	case asc.FinanceReportTypeFinancial:
		if region == "" {
			region = "ZZ"
		}
		if region == "Z1" {
			return "", "", errors.New("finance: FINANCIAL does not use Z1; omit --region for consolidated ZZ or choose a supported financial region")
		}
	case asc.FinanceReportTypeFinanceDetail:
		if region == "" {
			region = "Z1"
		}
		if region != "Z1" {
			return "", "", fmt.Errorf("finance: FINANCE_DETAIL requires region Z1, got %q", rawRegion)
		}
	default:
		return "", "", fmt.Errorf("finance: --report-type must be FINANCIAL or FINANCE_DETAIL; got %q", rawType)
	}
	return reportType, region, nil
}

func financeReportPeriod(rows []asc.FinanceReportRow) (start, end string) {
	if len(rows) == 0 {
		return "", ""
	}
	start, end = rows[0].StartDate, rows[0].EndDate
	for i := 1; i < len(rows); i++ {
		if rows[i].StartDate != "" && (start == "" || rows[i].StartDate < start) {
			start = rows[i].StartDate
		}
		if rows[i].EndDate > end {
			end = rows[i].EndDate
		}
	}
	return start, end
}

func financePeriodLabel(start, end string) string {
	if start == "" && end == "" {
		return ""
	}
	return start + ".." + end
}
