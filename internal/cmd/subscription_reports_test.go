package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func resetSubscriptionReportFlags() {
	subscriptionsReportsType = "summary"
	subscriptionsReportsRange = "P7D"
	subscriptionsReportsMonth = ""
	subscriptionsReportsFrequency = ""
}

func TestSubscriptionsReports_RegisteredOnSubscriptions(t *testing.T) {
	var subsCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "subscriptions" {
			subsCmd = c
			break
		}
	}
	if subsCmd == nil {
		t.Fatal("subscriptions not registered on rootCmd")
	}
	var found *cobra.Command
	for _, c := range subsCmd.Commands() {
		if c.Name() == "reports" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("subscriptions reports not registered on subscriptionsCmd")
	}
	for _, want := range []string{"type", "range", "month", "frequency"} {
		if found.Flag(want) == nil {
			t.Errorf("subscriptions reports: missing flag --%s", want)
		}
	}
}

func TestResolveSubscriptionReportType(t *testing.T) {
	tests := []struct {
		in   string
		want asc.SalesReportType
		err  bool
	}{
		{"summary", asc.SalesReportTypeSubscription, false},
		{"events", asc.SalesReportTypeSubscriptionEvent, false},
		{"retention", asc.SalesReportTypeSubscriber, false},
		{"SUMMARY", asc.SalesReportTypeSubscription, false},
		{"  events  ", asc.SalesReportTypeSubscriptionEvent, false},
		{"unknown", "", true},
		{"", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := resolveSubscriptionReportType(tc.in)
			if tc.err {
				if err == nil {
					t.Errorf("got %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParseDurationDays(t *testing.T) {
	tests := []struct {
		in   string
		want int
		err  bool
	}{
		{"P1D", 1, false},
		{"P7D", 7, false},
		{"P30D", 30, false},
		{"P1M", 30, false},
		{"P1Y", 365, false},
		{"p7d", 7, false}, // case-insensitive
		{"", 7, false},    // empty defaults to 7
		{"7d", 0, true},
		{"PXD", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseDurationDays(tc.in)
			if tc.err {
				if err == nil {
					t.Errorf("got %d, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBuildSubscriptionPlan_DefaultRange(t *testing.T) {
	resetSubscriptionReportFlags()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	plan, err := buildSubscriptionPlan(now)
	if err != nil {
		t.Fatalf("buildSubscriptionPlan: %v", err)
	}
	if plan.frequency != asc.SalesFrequencyDaily {
		t.Errorf("frequency = %s, want DAILY", plan.frequency)
	}
	if len(plan.dates) != 7 {
		t.Errorf("dates len = %d, want 7", len(plan.dates))
	}
	// Last date is yesterday.
	want := "2026-04-30"
	if plan.dates[len(plan.dates)-1] != want {
		t.Errorf("dates[last] = %q, want %q", plan.dates[len(plan.dates)-1], want)
	}
}

func TestBuildSubscriptionPlan_Month(t *testing.T) {
	resetSubscriptionReportFlags()
	subscriptionsReportsMonth = "2026-04"
	subscriptionsReportsRange = "P7D" // default — must not collide

	plan, err := buildSubscriptionPlan(time.Now())
	if err != nil {
		t.Fatalf("buildSubscriptionPlan: %v", err)
	}
	if plan.frequency != asc.SalesFrequencyMonthly {
		t.Errorf("frequency = %s, want MONTHLY", plan.frequency)
	}
	if len(plan.dates) != 1 || plan.dates[0] != "2026-04" {
		t.Errorf("dates = %+v, want [2026-04]", plan.dates)
	}
}

func TestBuildSubscriptionPlan_RejectsConflictingMonthAndExplicitRange(t *testing.T) {
	resetSubscriptionReportFlags()
	subscriptionsReportsMonth = "2026-04"
	subscriptionsReportsRange = "P30D" // explicit override
	if _, err := buildSubscriptionPlan(time.Now()); err == nil {
		t.Fatal("want error when --month + non-default --range")
	}
}

func TestBuildSubscriptionPlan_RejectsBadMonth(t *testing.T) {
	resetSubscriptionReportFlags()
	subscriptionsReportsMonth = "2026"
	subscriptionsReportsRange = "P7D"
	if _, err := buildSubscriptionPlan(time.Now()); err == nil {
		t.Fatal("want error for malformed --month")
	}
}

func TestSubscriptionReport_TableRowsAndJSON(t *testing.T) {
	rep := SubscriptionReport{
		BundleID:     "com.example.testapp",
		VendorNumber: "99999999",
		ReportType:   "SUBSCRIPTION",
		Frequency:    "DAILY",
		ReportDates:  []string{"2026-05-01"},
		RowCount:     1,
		Rows: []asc.SalesReportRow{
			{
				SKU:                "com.example.testapp.subscription.monthly",
				BeginDate:          "2026-05-01",
				Units:              120,
				DeveloperProceeds:  419.40,
				CurrencyOfProceeds: "USD",
			},
		},
	}
	headers, rows := rep.TableRows()
	want := []string{"SKU", "DATE", "UNITS", "PROCEEDS", "CURRENCY"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("header[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 || rows[0][2] != "120" {
		t.Errorf("rows = %+v", rows)
	}

	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"reportType":"SUBSCRIPTION"`,
		`"frequency":"DAILY"`,
		`"rowCount":1`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %q: %s", want, b)
		}
	}
}

func TestSubscriptionsReports_FetchesViaSalesEndpoint(t *testing.T) {
	srv := gzReportServer(t, map[string]string{
		"/v1/salesReports": "sales/subscription_summary.tsv",
	})
	c := fixtureASCClient(t, srv)

	rows, _, err := fetchSalesAcrossDates(context.Background(), c, salesFetchOpts{
		vendor:      "99999999",
		reportType:  asc.SalesReportTypeSubscription,
		reportSub:   asc.SalesReportSubTypeSummary,
		frequency:   asc.SalesFrequencyDaily,
		dates:       []string{"2026-05-01"},
		bundleID:    "com.example.testapp",
		captureRows: true,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("rows = %d, want >= 3 from subscription_summary fixture", len(rows))
	}
	if rows[0].SKU != "com.example.testapp.subscription.monthly" {
		t.Errorf("rows[0].SKU = %q", rows[0].SKU)
	}
	if rows[0].Units != 120 {
		t.Errorf("rows[0].Units = %d, want 120", rows[0].Units)
	}
}
