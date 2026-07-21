package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// Time-series salesReports data, distinct from the config-level reads in
// subscriptions.go.
type SubscriptionReport struct {
	BundleID     string               `json:"bundleId"`
	VendorNumber string               `json:"vendorNumber"`
	ReportType   string               `json:"reportType"`
	Frequency    string               `json:"frequency"`
	ReportDates  []string             `json:"reportDates"`
	Unavailable  []string             `json:"unavailableDates"`
	RowCount     int                  `json:"rowCount"`
	Rows         []asc.SalesReportRow `json:"rows"`
	Note         string               `json:"note,omitempty"`
}

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

var subscriptionsReportsCmd = &cobra.Command{
	Use:   "reports <bundleId>",
	Short: "Fetch subscription time-series reports (summary / events / retention)",
	Long: `subscriptions reports pulls time-series subscription data from
/v1/salesReports with reportType set to one of:

  --type summary    → SUBSCRIPTION       (active counts, period, proceeds)
  --type events     → SUBSCRIPTION_EVENT (cancel/upgrade/downgrade events)
  --type retention  → SUBSCRIBER         (subscriber-level retention rows)

Distinct from ` + "`subscriptions list`" + ` and ` + "`subscriptions get`" + `,
which read the configuration of subscription products. This command is the
analytical view: how many subscribers, when did they churn, where they
came from.

Frequency is inferred from --range:
  P1D / P7D / P30D / P1Y → DAILY (one call per day, 1..N calls)
  P1M                    → MONTHLY (single call)
  P1Y                    → YEARLY  (single call when --frequency=YEARLY)

Vendor number is read from APP_STORE_CONNECT_VENDOR_NUMBER. The bundleId
argument resolves the app and filters typed output by its bundle ID, SKU,
and Apple ID (sales reports are vendor-wide on the wire).`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runSubscriptionsReports,
	Example: `  flightline subscriptions reports com.example.myapp --type summary --range P30D
  flightline subscriptions reports com.example.myapp --type events --range P7D
  flightline subscriptions reports com.example.myapp --type retention --month 2026-04
  flightline subscriptions reports com.example.myapp --type summary --range P30D --output json | jq '.rows | length'`,
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
	app, err := resolveApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	mode := outputMode()
	rawMode := mode == "tsv"

	fetched, err := fetchSalesAcrossDates(cmd.Context(), c, salesFetchOpts{
		vendor:     vendor,
		reportType: reportType,
		reportSub:  asc.SalesReportSubTypeSummary,
		frequency:  plan.frequency,
		dates:      plan.dates,
		app: salesAppIdentity{
			BundleID: app.Attributes.BundleID,
			SKU:      app.Attributes.SKU,
			AppleID:  app.ID,
		},
		captureRaw:  rawMode,
		captureRows: !rawMode,
	})
	if err != nil {
		return err
	}

	if rawMode {
		if _, err := os.Stdout.Write(fetched.raw); err != nil {
			return fmt.Errorf("subscriptions reports: write tsv: %w", err)
		}
		return nil
	}

	report := SubscriptionReport{
		BundleID:     app.Attributes.BundleID,
		VendorNumber: vendor,
		ReportType:   string(reportType),
		Frequency:    string(plan.frequency),
		ReportDates:  plan.dates,
		Unavailable:  fetched.unavailableDates,
		RowCount:     len(fetched.rows),
		Rows:         fetched.rows,
	}
	if len(fetched.unavailableDates) > 0 {
		report.Note = fmt.Sprintf("Apple had no report for %d requested date(s); available dates were still processed", len(fetched.unavailableDates))
	}
	return Render(report, mode)
}

func resolveSubscriptionReportType(raw string) (asc.SalesReportType, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	rt, ok := subscriptionReportTypeMap[v]
	if !ok {
		return "", fmt.Errorf("subscriptions reports: --type must be one of: summary, events, retention; got %q", raw)
	}
	return rt, nil
}

func buildSubscriptionPlan(now time.Time) (salesPlan, error) {
	month := strings.TrimSpace(subscriptionsReportsMonth)
	dur := strings.TrimSpace(subscriptionsReportsRange)

	switch {
	case month != "" && dur != "" && dur != "P7D":
		// "P7D" is the flag default; users explicitly setting --month should
		// be allowed without the default value triggering a conflict error.
		return salesPlan{}, errors.New("subscriptions reports: --month and --range are mutually exclusive")
	case month != "":
		if _, err := time.Parse("2006-01", month); err != nil {
			return salesPlan{}, fmt.Errorf("subscriptions reports: --month must be YYYY-MM, got %q", subscriptionsReportsMonth)
		}
		freq, err := reportFrequencyFor(subscriptionsReportsFrequency, asc.SalesFrequencyMonthly, "subscriptions reports")
		if err != nil {
			return salesPlan{}, err
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

// Apple's reportDate API works in dates not durations, so the month/year
// shorthands are approximations; exact months require --month YYYY-MM.
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

// Window ends yesterday: Apple lags by ~1 day, today isn't available yet.
func planDailySubscriptionWindow(now time.Time, n int) (salesPlan, error) {
	if n <= 0 {
		return salesPlan{}, errors.New("subscriptions reports: range produced 0 days")
	}
	freq, err := reportFrequencyFor(subscriptionsReportsFrequency, asc.SalesFrequencyDaily, "subscriptions reports")
	if err != nil {
		return salesPlan{}, err
	}
	dates := make([]string, 0, n)
	end := now.AddDate(0, 0, -1)
	for i := n - 1; i >= 0; i-- {
		dates = append(dates, end.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	return salesPlan{frequency: freq, dates: dates}, nil
}

func intCell(n int) string {
	return strconv.Itoa(n)
}

func floatCell(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
