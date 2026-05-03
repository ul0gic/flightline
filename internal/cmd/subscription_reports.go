package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// SubscriptionReport is the JSON contract for `subscriptions reports`.
// Subscription reports are time-series data on /v1/salesReports with
// reportType ∈ {SUBSCRIPTION, SUBSCRIPTION_EVENT, SUBSCRIBER}, distinct from
// the configuration-level read commands in subscriptions.go (Phase 2.4).
type SubscriptionReport struct {
	BundleID     string               `json:"bundleId"`
	VendorNumber string               `json:"vendorNumber"`
	ReportType   string               `json:"reportType"`
	Frequency    string               `json:"frequency"`
	ReportDates  []string             `json:"reportDates"`
	RowCount     int                  `json:"rowCount"`
	Rows         []asc.SalesReportRow `json:"rows"`
}

// TableRows for SubscriptionReport renders a SKU/units summary.
func (r SubscriptionReport) TableRows() (headers []string, rows [][]string) {
	headers = []string{"SKU", "DATE", "UNITS", "PROCEEDS", "CURRENCY"}
	if len(r.Rows) == 0 {
		return headers, nil
	}
	rows = make([][]string, 0, len(r.Rows))
	for i := range r.Rows {
		row := &r.Rows[i]
		rows = append(rows, []string{
			row.SKU,
			row.BeginDate,
			intCell(row.Units),
			floatCell(row.DeveloperProceeds),
			row.CurrencyOfProceeds,
		})
	}
	return headers, rows
}

// subscriptionsReportsCmd is registered as a subcommand of the existing
// `subscriptions` command (defined in subscriptions.go). The parent var
// `subscriptionsCmd` is package-level and stable.
var subscriptionsReportsCmd = &cobra.Command{
	Use:   "reports <bundleId>",
	Short: "Fetch subscription time-series reports (summary / events / retention)",
	Long: `subscriptions reports pulls time-series subscription data from
/v1/salesReports with reportType set to one of:

  --type summary    → SUBSCRIPTION       (active counts, period, proceeds)
  --type events     → SUBSCRIPTION_EVENT (cancel/upgrade/downgrade events)
  --type retention  → SUBSCRIBER         (subscriber-level retention rows)

Distinct from ` + "`subscriptions list`" + ` and ` + "`subscriptions get`" + `,
which read the configuration of subscription products (Phase 2.4). This
command is the analytical view: how many subscribers, when did they
churn, where they came from.

Frequency is inferred from --range:
  P1D / P7D / P30D / P1Y → DAILY (one call per day, 1..N calls)
  P1M                    → MONTHLY (single call)
  P1Y                    → YEARLY  (single call when --frequency=YEARLY)

Vendor number is read from APP_STORE_CONNECT_VENDOR_NUMBER. The bundleId
argument filters typed output by parent identifier (sales reports are
vendor-wide on the wire).`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSubscriptionsReports,
	Example: `  fline subscriptions reports com.example.myapp --type summary --range P30D
  fline subscriptions reports com.example.myapp --type events --range P7D
  fline subscriptions reports com.example.myapp --type retention --month 2026-04
  fline subscriptions reports com.example.myapp --type summary --range P30D --output json | jq '.rows | length'`,
}

var (
	subscriptionsReportsType      string
	subscriptionsReportsRange     string
	subscriptionsReportsMonth     string
	subscriptionsReportsFrequency string
)

func init() {
	subscriptionsReportsCmd.Flags().StringVar(&subscriptionsReportsType, "type", "summary",
		"report type: summary | events | retention")
	subscriptionsReportsCmd.Flags().StringVar(&subscriptionsReportsRange, "range", "P7D",
		"ISO-8601 duration ending yesterday: P1D, P7D, P30D, P1Y. Daily granularity (one call per day).")
	subscriptionsReportsCmd.Flags().StringVar(&subscriptionsReportsMonth, "month", "",
		"alternative to --range: pull a single MONTHLY report for YYYY-MM")
	subscriptionsReportsCmd.Flags().StringVar(&subscriptionsReportsFrequency, "frequency", "",
		"override the inferred frequency (DAILY/WEEKLY/MONTHLY/YEARLY)")

	subscriptionsCmd.AddCommand(subscriptionsReportsCmd)
}

// subscriptionReportTypeMap normalises the human-friendly --type values to
// Apple's salesReports reportType enum.
var subscriptionReportTypeMap = map[string]asc.SalesReportType{
	"summary":   asc.SalesReportTypeSubscription,
	"events":    asc.SalesReportTypeSubscriptionEvent,
	"retention": asc.SalesReportTypeSubscriber,
}

func runSubscriptionsReports(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	vendor, err := requireVendorNumber()
	if err != nil {
		return err
	}

	reportType, err := resolveSubscriptionReportType(subscriptionsReportsType)
	if err != nil {
		return err
	}

	plan, err := buildSubscriptionPlan(time.Now().UTC())
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
		reportType:  reportType,
		reportSub:   asc.SalesReportSubTypeSummary,
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
			return fmt.Errorf("subscriptions reports: write tsv: %w", err)
		}
		return nil
	}

	report := SubscriptionReport{
		BundleID:     bundleID,
		VendorNumber: vendor,
		ReportType:   string(reportType),
		Frequency:    string(plan.frequency),
		ReportDates:  plan.dates,
		RowCount:     len(rows),
		Rows:         rows,
	}
	return Render(report, mode)
}

// resolveSubscriptionReportType maps the human-friendly --type to the Apple
// enum value, returning an actionable error for typos.
func resolveSubscriptionReportType(raw string) (asc.SalesReportType, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	rt, ok := subscriptionReportTypeMap[v]
	if !ok {
		return "", fmt.Errorf("subscriptions reports: --type must be one of: summary, events, retention; got %q", raw)
	}
	return rt, nil
}

// buildSubscriptionPlan turns the flag inputs into a date list + frequency,
// reusing the salesPlan shape.
func buildSubscriptionPlan(now time.Time) (salesPlan, error) {
	month := strings.TrimSpace(subscriptionsReportsMonth)
	dur := strings.TrimSpace(subscriptionsReportsRange)

	switch {
	case month != "" && dur != "" && dur != "P7D":
		// "P7D" is the flag default; users explicitly setting --month should
		// be allowed without the default value triggering a conflict error.
		return salesPlan{}, fmt.Errorf("subscriptions reports: --month and --range are mutually exclusive")
	case month != "":
		if _, err := time.Parse("2006-01", month); err != nil {
			return salesPlan{}, fmt.Errorf("subscriptions reports: --month must be YYYY-MM, got %q", subscriptionsReportsMonth)
		}
		freq := asc.SalesFrequencyMonthly
		if override := strings.TrimSpace(subscriptionsReportsFrequency); override != "" {
			freq = asc.SalesFrequency(strings.ToUpper(override))
		}
		return salesPlan{frequency: freq, dates: []string{month}}, nil
	default:
		days, err := parseDurationDays(dur)
		if err != nil {
			return salesPlan{}, err
		}
		return planDailySubscriptionWindow(now, days)
	}
}

// parseDurationDays handles a small subset of ISO-8601 durations: P1D, P7D,
// P30D, P1M (≈30 days), P1Y (≈365 days). Apple's reportDate API works in
// dates not durations, so we approximate the month/year shorthands; users
// who want exact months should use --month YYYY-MM.
func parseDurationDays(dur string) (int, error) {
	d := strings.ToUpper(strings.TrimSpace(dur))
	switch d {
	case "":
		return 7, nil
	case "P1D":
		return 1, nil
	case "P7D":
		return 7, nil
	case "P14D":
		return 14, nil
	case "P30D":
		return 30, nil
	case "P1M":
		return 30, nil
	case "P90D":
		return 90, nil
	case "P1Y":
		return 365, nil
	default:
		return 0, fmt.Errorf("subscriptions reports: --range must be one of P1D/P7D/P14D/P30D/P1M/P90D/P1Y, got %q", dur)
	}
}

// planDailySubscriptionWindow builds a daily date list for the last `n`
// days ending yesterday. Apple lags by ~1 day so today's not available yet.
func planDailySubscriptionWindow(now time.Time, n int) (salesPlan, error) {
	if n <= 0 {
		return salesPlan{}, fmt.Errorf("subscriptions reports: range produced 0 days")
	}
	freq := asc.SalesFrequencyDaily
	if override := strings.TrimSpace(subscriptionsReportsFrequency); override != "" {
		freq = asc.SalesFrequency(strings.ToUpper(override))
	}
	dates := make([]string, 0, n)
	end := now.AddDate(0, 0, -1)
	for i := n - 1; i >= 0; i-- {
		dates = append(dates, end.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	return salesPlan{frequency: freq, dates: dates}, nil
}

// intCell formats an int for table output.
func intCell(n int) string {
	return fmt.Sprintf("%d", n)
}

// floatCell formats a float for table output (2 decimals).
func floatCell(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
