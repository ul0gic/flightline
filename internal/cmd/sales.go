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

// SalesReport is the cmd-layer view emitted as JSON, carrying the fetch params
// alongside the typed rows so consumers can correlate output with input.
type SalesReport struct {
	BundleID     string               `json:"bundleId"`
	VendorNumber string               `json:"vendorNumber"`
	ReportType   string               `json:"reportType"`
	Frequency    string               `json:"frequency"`
	ReportDates  []string             `json:"reportDates"`
	RowCount     int                  `json:"rowCount"`
	Rows         []asc.SalesReportRow `json:"rows"`
	Summary      []SalesDailySummary  `json:"summary,omitempty"`
}

// SalesDailySummary collapses the per-row sales numbers into a date+platform
// view that's useful for the table renderer (and for jq-style ad hoc queries).
type SalesDailySummary struct {
	Date              string  `json:"date"`
	Units             int     `json:"units"`
	DeveloperProceeds float64 `json:"developerProceeds"`
	Currency          string  `json:"currency"`
}

// TableRows renders the compact daily summary; the full per-row payload only goes to JSON.
func (r SalesReport) TableRows() (headers []string, rows [][]string) {
	if len(r.Summary) == 0 {
		// Header-only output is more useful than empty for a no-sales window.
		return []string{"DATE", "UNITS", "PROCEEDS", "CURRENCY"}, nil
	}
	headers = []string{"DATE", "UNITS", "PROCEEDS", "CURRENCY"}
	rows = make([][]string, 0, len(r.Summary))
	for _, s := range r.Summary {
		rows = append(rows, []string{
			s.Date,
			strconv.Itoa(s.Units),
			strconv.FormatFloat(s.DeveloperProceeds, 'f', 2, 64),
			s.Currency,
		})
	}
	return headers, rows
}

var salesCmd = &cobra.Command{
	Use:   "sales <bundleId>",
	Short: "Fetch App Store Connect sales reports (TSV-backed, vendor-wide)",
	Long: `sales pulls Sales and Trends reports from /v1/salesReports.

Reports are vendor-wide: Apple does not filter by app on the wire. The
bundleId argument scopes the typed table/JSON output by parent identifier
so a single-vendor multi-app account stays focused. Use --output tsv to
stream Apple's raw (gunzipped) wire format unfiltered for downstream tools.

Frequency is inferred from the date flag (--days → DAILY, --week → WEEKLY,
--month → MONTHLY, --year → YEARLY) and can be overridden with --frequency.
Reports are fetched per-day for daily windows so a 30-day pull = 30 API
calls; budget against Apple's 500 req/hr cap accordingly.

Vendor number is read from APP_STORE_CONNECT_VENDOR_NUMBER. Refuses to run
without one rather than erroring on the wire.`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSales,
	Example: `  flightline sales com.example.myapp --days 7
  flightline sales com.example.myapp --month 2026-04
  flightline sales com.example.myapp --week 2026-04-29
  flightline sales com.example.myapp --year 2026
  flightline sales com.example.myapp --report-type SUBSCRIPTION --month 2026-04
  flightline sales com.example.myapp --days 30 --output json | jq '.summary'
  flightline sales com.example.myapp --days 1 --output tsv > today.tsv`,
}

var (
	salesDays         int
	salesMonth        string
	salesWeek         string
	salesYear         string
	salesReportType   string
	salesFrequency    string
	salesReportSubTyp string
)

func init() {
	salesCmd.Flags().IntVar(&salesDays, "days", 0, "fetch the last N days of DAILY reports (mutually exclusive with --month/--week/--year)")
	salesCmd.Flags().StringVar(&salesMonth, "month", "", "MONTHLY report for YYYY-MM")
	salesCmd.Flags().StringVar(&salesWeek, "week", "", "WEEKLY report for the week containing YYYY-MM-DD (Apple aligns to Sunday)")
	salesCmd.Flags().StringVar(&salesYear, "year", "", "YEARLY report for YYYY")
	salesCmd.Flags().StringVar(&salesReportType, "report-type", string(asc.SalesReportTypeSales),
		"reportType filter (SALES, SUBSCRIPTION, SUBSCRIPTION_EVENT, SUBSCRIBER, INSTALLS, ...)")
	salesCmd.Flags().StringVar(&salesReportSubTyp, "report-sub-type", string(asc.SalesReportSubTypeSummary),
		"reportSubType filter (SUMMARY, DETAILED, SUMMARY_INSTALL_TYPE, SUMMARY_TERRITORY, SUMMARY_CHANNEL)")
	salesCmd.Flags().StringVar(&salesFrequency, "frequency", "",
		"override the inferred frequency (DAILY/WEEKLY/MONTHLY/YEARLY)")

	rootCmd.AddCommand(salesCmd)
}

// salesPlan describes one fetch round: how many calls to make, what dates,
// what frequency. Built once from flag inputs so the runner stays simple.
type salesPlan struct {
	frequency asc.SalesFrequency
	dates     []string
}

func runSales(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	vendor, err := requireVendorNumber()
	if err != nil {
		return err
	}
	plan, err := buildSalesPlan(time.Now().UTC())
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	mode := outputMode()
	rawMode := mode == "tsv"

	rows, raw, err := fetchSalesAcrossDates(cmd.Context(), c, salesFetchOpts{
		vendor:      vendor,
		reportType:  asc.SalesReportType(strings.TrimSpace(salesReportType)),
		reportSub:   asc.SalesReportSubType(strings.TrimSpace(salesReportSubTyp)),
		frequency:   plan.frequency,
		dates:       plan.dates,
		bundleID:    bundleID,
		captureRaw:  rawMode,
		captureRows: !rawMode,
	})
	if err != nil {
		return err
	}

	if rawMode {
		if _, err := os.Stdout.Write(raw); err != nil {
			return fmt.Errorf("sales: write tsv: %w", err)
		}
		return nil
	}

	report := SalesReport{
		BundleID:     bundleID,
		VendorNumber: vendor,
		ReportType:   salesReportType,
		Frequency:    string(plan.frequency),
		ReportDates:  plan.dates,
		RowCount:     len(rows),
		Rows:         rows,
		Summary:      summarizeSalesByDate(rows),
	}
	return Render(report, mode)
}

type salesFetchOpts struct {
	vendor      string
	reportType  asc.SalesReportType
	reportSub   asc.SalesReportSubType
	frequency   asc.SalesFrequency
	dates       []string
	bundleID    string
	captureRaw  bool
	captureRows bool
}

// fetchSalesAcrossDates makes one /v1/salesReports call per date: Apple returns one date per call,
// so a 30-day window is 30 calls. Returns typed rows or raw TSV per captureRaw.
func fetchSalesAcrossDates(ctx context.Context, c *asc.Client, opts salesFetchOpts) ([]asc.SalesReportRow, []byte, error) {
	var (
		rows []asc.SalesReportRow
		raw  []byte
	)
	for _, date := range opts.dates {
		body, err := c.FetchSalesReport(ctx, asc.SalesReportParams{
			VendorNumber:  opts.vendor,
			ReportType:    opts.reportType,
			ReportSubType: opts.reportSub,
			Frequency:     opts.frequency,
			ReportDate:    date,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("sales: fetch %s: %w", date, err)
		}
		if opts.captureRaw {
			raw = append(raw, body...)
			continue
		}
		if !opts.captureRows {
			continue
		}
		decoded, err := asc.DecodeSalesTSV(body)
		if err != nil {
			return nil, nil, fmt.Errorf("sales: decode %s: %w", date, err)
		}
		for i := range decoded {
			if salesRowMatchesBundle(&decoded[i], opts.bundleID) {
				rows = append(rows, decoded[i])
			}
		}
	}
	return rows, raw, nil
}

// salesRowMatchesBundle matches a row to an app, falling back to SKU-prefix when the row has no parent.
func salesRowMatchesBundle(r *asc.SalesReportRow, bundleID string) bool {
	if bundleID == "" {
		return true
	}
	if r.ParentIdentifier == bundleID {
		return true
	}
	if r.ParentIdentifier == "" && strings.HasPrefix(r.SKU, bundleID) {
		return true
	}
	if r.SKU == bundleID {
		return true
	}
	return false
}

// summarizeSalesByDate folds rows by (date, currency); currencies are never summed across.
func summarizeSalesByDate(rows []asc.SalesReportRow) []SalesDailySummary {
	type key struct{ date, ccy string }
	agg := make(map[key]*SalesDailySummary)
	for i := range rows {
		r := &rows[i]
		k := key{date: r.BeginDate, ccy: r.CurrencyOfProceeds}
		s, ok := agg[k]
		if !ok {
			s = &SalesDailySummary{Date: k.date, Currency: k.ccy}
			agg[k] = s
		}
		s.Units += r.Units
		s.DeveloperProceeds += r.DeveloperProceeds
	}
	out := make([]SalesDailySummary, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Currency < out[j].Currency
	})
	return out
}

// requireVendorNumber returns the vendor number from env or an actionable error.
func requireVendorNumber() (string, error) {
	v := strings.TrimSpace(os.Getenv("APP_STORE_CONNECT_VENDOR_NUMBER"))
	if v == "" {
		return "", errors.New("sales: APP_STORE_CONNECT_VENDOR_NUMBER is required (find it at App Store Connect → Payments and Financial Reports)")
	}
	return v, nil
}

// buildSalesPlan turns flag inputs into a date list + frequency.
// `now` is injected so tests can pin a "today" reference.
func buildSalesPlan(now time.Time) (salesPlan, error) {
	chosen, err := pickSalesDateFlag()
	if err != nil {
		return salesPlan{}, err
	}
	switch chosen {
	case "days":
		return planDailyWindow(now, salesDays)
	case "month":
		return planMonth()
	case "week":
		return planWeek()
	case "year":
		return planYear()
	default:
		// No date flag: default to last 7 days of DAILY.
		return planDailyWindow(now, 7)
	}
}

// pickSalesDateFlag enforces mutual exclusivity across --days/--month/--week/--year.
// Returns the name of the chosen flag ("" if none).
func pickSalesDateFlag() (string, error) {
	count := 0
	chosen := ""
	if salesDays > 0 {
		count++
		chosen = "days"
	}
	if strings.TrimSpace(salesMonth) != "" {
		count++
		chosen = "month"
	}
	if strings.TrimSpace(salesWeek) != "" {
		count++
		chosen = "week"
	}
	if strings.TrimSpace(salesYear) != "" {
		count++
		chosen = "year"
	}
	if count > 1 {
		return "", errors.New("sales: --days, --month, --week, --year are mutually exclusive")
	}
	return chosen, nil
}

// planDailyWindow lists the last N days ending yesterday; Apple's sales data lags ~1 day.
func planDailyWindow(now time.Time, n int) (salesPlan, error) {
	if n <= 0 {
		return salesPlan{}, fmt.Errorf("sales: --days must be > 0, got %d", n)
	}
	freq := asc.SalesFrequencyDaily
	if override := strings.TrimSpace(salesFrequency); override != "" {
		freq = asc.SalesFrequency(strings.ToUpper(override))
	}
	dates := make([]string, 0, n)
	end := now.AddDate(0, 0, -1)
	for i := n - 1; i >= 0; i-- {
		d := end.AddDate(0, 0, -i)
		dates = append(dates, d.Format("2006-01-02"))
	}
	return salesPlan{frequency: freq, dates: dates}, nil
}

// planMonth validates --month YYYY-MM and returns a single-date plan.
func planMonth() (salesPlan, error) {
	m := strings.TrimSpace(salesMonth)
	if _, err := time.Parse("2006-01", m); err != nil {
		return salesPlan{}, fmt.Errorf("sales: --month must be YYYY-MM, got %q", salesMonth)
	}
	freq := asc.SalesFrequencyMonthly
	if override := strings.TrimSpace(salesFrequency); override != "" {
		freq = asc.SalesFrequency(strings.ToUpper(override))
	}
	return salesPlan{frequency: freq, dates: []string{m}}, nil
}

// planWeek validates --week YYYY-MM-DD; Apple aligns any in-week date to its Sunday-starting week.
func planWeek() (salesPlan, error) {
	w := strings.TrimSpace(salesWeek)
	t, err := time.Parse("2006-01-02", w)
	if err != nil {
		return salesPlan{}, fmt.Errorf("sales: --week must be YYYY-MM-DD, got %q", salesWeek)
	}
	freq := asc.SalesFrequencyWeekly
	if override := strings.TrimSpace(salesFrequency); override != "" {
		freq = asc.SalesFrequency(strings.ToUpper(override))
	}
	return salesPlan{frequency: freq, dates: []string{t.Format("2006-01-02")}}, nil
}

// planYear validates --year YYYY and returns a single-date plan.
func planYear() (salesPlan, error) {
	y := strings.TrimSpace(salesYear)
	if _, err := time.Parse("2006", y); err != nil {
		return salesPlan{}, fmt.Errorf("sales: --year must be YYYY, got %q", salesYear)
	}
	freq := asc.SalesFrequencyYearly
	if override := strings.TrimSpace(salesFrequency); override != "" {
		freq = asc.SalesFrequency(strings.ToUpper(override))
	}
	return salesPlan{frequency: freq, dates: []string{y}}, nil
}
