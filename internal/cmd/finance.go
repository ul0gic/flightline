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
	RegionCode   string                 `json:"regionCode"`
	RowCount     int                    `json:"rowCount"`
	Rows         []asc.FinanceReportRow `json:"rows"`
	Summary      []FinanceRegionSummary `json:"summary,omitempty"`
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
	headers = []string{"COUNTRY", "CURRENCY", "QTY", "PARTNER_SHARE", "EXT_PARTNER_SHARE"}
	if len(r.Summary) == 0 {
		return headers, nil
	}
	rows = make([][]string, 0, len(r.Summary))
	for _, s := range r.Summary {
		rows = append(rows, []string{
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

Finance reports are MONTHLY or YEARLY (Apple does not produce daily/weekly
finance reports: daily granularity belongs to ` + "`flightline sales`" + `).
Each call is scoped to a region code: "US", "GB", "Z1" (worldwide), etc.
Use --region to override the default ("Z1").

The bundleId argument filters typed output by Vendor Identifier so a single-
vendor multi-app account stays focused. --output tsv streams Apple's raw
wire format unfiltered.

Vendor number is read from APP_STORE_CONNECT_VENDOR_NUMBER.`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runFinance,
	Example: `  flightline finance com.example.myapp --month 2026-04
  flightline finance com.example.myapp --year 2026
  flightline finance com.example.myapp --month 2026-04 --region US
  flightline finance com.example.myapp --month 2026-04 --output json | jq '.summary'
  flightline finance com.example.myapp --month 2026-04 --output tsv > finance.tsv`,
}

var (
	financeMonth      string
	financeYear       string
	financeRegion     string
	financeReportType string
)

func init() {
	financeCmd.Flags().StringVar(&financeMonth, "month", "", "MONTHLY report for YYYY-MM (mutually exclusive with --year)")
	financeCmd.Flags().StringVar(&financeYear, "year", "", "YEARLY report for YYYY (mutually exclusive with --month)")
	financeCmd.Flags().StringVar(&financeRegion, "region", "Z1", "ISO region code (Z1 = worldwide)")
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
	region := strings.TrimSpace(financeRegion)
	if region == "" {
		return errors.New("finance: --region is required (use Z1 for worldwide)")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	mode := outputMode()
	rawMode := mode == "tsv"

	rows, raw, err := fetchFinanceReport(cmd.Context(), c, financeFetchOpts{
		vendor:      vendor,
		reportType:  asc.FinanceReportType(strings.TrimSpace(financeReportType)),
		region:      region,
		reportDate:  reportDate,
		bundleID:    bundleID,
		captureRaw:  rawMode,
		captureRows: !rawMode,
	})
	if err != nil {
		return err
	}

	if rawMode {
		if _, err := os.Stdout.Write(raw); err != nil {
			return fmt.Errorf("finance: write tsv: %w", err)
		}
		return nil
	}

	report := FinanceReport{
		BundleID:     bundleID,
		VendorNumber: vendor,
		ReportType:   financeReportType,
		Frequency:    freq,
		ReportDate:   reportDate,
		RegionCode:   region,
		RowCount:     len(rows),
		Rows:         rows,
		Summary:      summarizeFinanceByRegion(rows),
	}
	return Render(report, mode)
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
func fetchFinanceReport(ctx context.Context, c *asc.Client, opts financeFetchOpts) ([]asc.FinanceReportRow, []byte, error) {
	body, err := c.FetchFinanceReport(ctx, asc.FinanceReportParams{
		VendorNumber: opts.vendor,
		ReportType:   opts.reportType,
		RegionCode:   opts.region,
		ReportDate:   opts.reportDate,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("finance: fetch %s/%s: %w", opts.region, opts.reportDate, err)
	}
	if opts.captureRaw {
		return nil, body, nil
	}
	if !opts.captureRows {
		return nil, nil, nil
	}
	decoded, err := asc.DecodeFinanceTSV(body)
	if err != nil {
		return nil, nil, fmt.Errorf("finance: decode %s/%s: %w", opts.region, opts.reportDate, err)
	}
	rows := make([]asc.FinanceReportRow, 0, len(decoded))
	for i := range decoded {
		if financeRowMatchesBundle(&decoded[i], opts.bundleID) {
			rows = append(rows, decoded[i])
		}
	}
	return rows, nil, nil
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

// buildFinanceDate returns reportDate + frequency; exactly one of --month / --year must be set.
func buildFinanceDate() (reportDate, frequency string, err error) {
	month := strings.TrimSpace(financeMonth)
	year := strings.TrimSpace(financeYear)
	switch {
	case month != "" && year != "":
		return "", "", errors.New("finance: --month and --year are mutually exclusive")
	case month != "":
		if _, err := time.Parse("2006-01", month); err != nil {
			return "", "", fmt.Errorf("finance: --month must be YYYY-MM, got %q", financeMonth)
		}
		return month, "MONTHLY", nil
	case year != "":
		if _, err := time.Parse("2006", year); err != nil {
			return "", "", fmt.Errorf("finance: --year must be YYYY, got %q", financeYear)
		}
		return year, "YEARLY", nil
	default:
		return "", "", errors.New("finance: one of --month YYYY-MM or --year YYYY is required")
	}
}
