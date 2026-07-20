package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPricingView_JSONShape(t *testing.T) {
	yes := true
	v := PricingView{
		BundleID: "com.example.alpha",
		Schedule: PriceScheduleSummary{
			ID:                  "1234567890",
			BaseTerritoryID:     "USA",
			BaseCurrency:        "USD",
			ManualPriceCount:    2,
			AutomaticPriceCount: 3,
		},
		Availability: AvailabilitySummary{
			ID:                        "AVAIL-1",
			AvailableTotal:            4,
			AvailableCount:            3,
			AvailableInNewTerritories: &yes,
		},
		BasePrice: &PricePointSummary{
			TerritoryID:   "USA",
			Currency:      "USD",
			CustomerPrice: "9.99",
			Proceeds:      "6.99",
			StartDate:     "2020-01-01",
			EndDate:       "2099-12-31",
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"bundleId":"com.example.alpha"`,
		`"baseTerritoryId":"USA"`,
		`"baseCurrency":"USD"`,
		`"manualPriceCount":2`,
		`"availableInNewTerritories":true`,
		`"availableCount":3`,
		`"customerPrice":"9.99"`,
		`"proceeds":"6.99"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestPricingView_TableRows_VerticalLayout(t *testing.T) {
	v := &PricingView{
		BundleID: "com.example.alpha",
		Schedule: PriceScheduleSummary{
			ID:               "1234567890",
			BaseTerritoryID:  "USA",
			BaseCurrency:     "USD",
			ManualPriceCount: 1,
		},
		Availability: AvailabilitySummary{AvailableTotal: 1, AvailableCount: 1},
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 9 {
		t.Errorf("rows = %d, want >= 9", len(rows))
	}
	foundAutoEqualized := false
	for _, r := range rows {
		if strings.Contains(r[1], "auto-equalized") {
			foundAutoEqualized = true
			break
		}
	}
	if !foundAutoEqualized {
		t.Errorf("expected auto-equalized cell when basePrice is nil")
	}
}

func TestPricingCommand_RegisteredOnRoot(t *testing.T) {
	var pr *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "pricing" {
			pr = c
			break
		}
	}
	if pr == nil {
		t.Fatal("pricing not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range pr.Commands() {
		subs[sc.Name()] = true
	}
	if !subs["get"] {
		t.Errorf("pricing get subcommand missing")
	}
}

func TestPricing_JSONOutputStability(t *testing.T) {
	v := &PricingView{
		BundleID:     "com.example.alpha",
		Schedule:     PriceScheduleSummary{ID: "S", BaseTerritoryID: "USA", ManualPriceCount: 1},
		Availability: AvailabilitySummary{ID: "A", AvailableTotal: 1, AvailableCount: 1},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "schedule", "availability"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q: JSON contract drift. Got: %v", key, mapKeys(decoded))
		}
	}
	sched, ok := decoded["schedule"].(map[string]any)
	if !ok {
		t.Fatalf("schedule not an object: %T", decoded["schedule"])
	}
	for _, key := range []string{"baseTerritoryId", "manualPriceCount", "automaticPriceCount"} {
		if _, ok := sched[key]; !ok {
			t.Errorf("schedule missing key %q: JSON contract drift", key)
		}
	}
	avail, ok := decoded["availability"].(map[string]any)
	if !ok {
		t.Fatalf("availability not an object: %T", decoded["availability"])
	}
	for _, key := range []string{"availableTotal", "availableCount"} {
		if _, ok := avail[key]; !ok {
			t.Errorf("availability missing key %q: JSON contract drift", key)
		}
	}
}

func TestPriceWindow_FormattingCases(t *testing.T) {
	cases := []struct {
		start, end, want string
	}{
		{"2020-01-01", "2099-12-31", "2020-01-01 → 2099-12-31"},
		{"2020-01-01", "", "2020-01-01 → indefinite"},
		{"", "2099-12-31", "until 2099-12-31"},
		{"", "", ""},
	}
	for _, c := range cases {
		t.Run(c.start+"|"+c.end, func(t *testing.T) {
			got := priceWindow(c.start, c.end)
			if got != c.want {
				t.Errorf("priceWindow(%q,%q) = %q, want %q", c.start, c.end, got, c.want)
			}
		})
	}
}

func TestWindowCovers_Boundaries(t *testing.T) {
	cases := []struct {
		today, start, end string
		want              bool
	}{
		{"2024-06-01", "2024-01-01", "2024-12-31", true},
		{"2024-06-01", "2024-12-31", "", false},           // future window
		{"2024-06-01", "", "2024-12-31", true},            // open start, in-range
		{"2024-06-01", "", "", true},                      // wide-open
		{"2024-12-31", "2024-01-01", "2024-12-31", false}, // end is exclusive
	}
	for _, c := range cases {
		got := windowCovers(c.today, c.start, c.end)
		if got != c.want {
			t.Errorf("windowCovers(%q,%q,%q) = %v, want %v", c.today, c.start, c.end, got, c.want)
		}
	}
}

func TestPricing_FixtureReplay_ScheduleAndPricePoint(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appPriceSchedule": {File: "pricing_get"},
		"GET /v3/appPricePoints/PP-USA-999":        {File: "pricing_price_point"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	sched, base, err := fetchPriceSchedule(ctx, c, appID)
	if err != nil {
		t.Fatalf("fetchPriceSchedule: %v", err)
	}
	if sched.ID != "1234567890" {
		t.Errorf("schedule ID = %q, want 1234567890", sched.ID)
	}
	if sched.BaseTerritoryID != "USA" {
		t.Errorf("baseTerritoryID = %q, want USA", sched.BaseTerritoryID)
	}
	if sched.BaseCurrency != "USD" {
		t.Errorf("baseCurrency = %q, want USD", sched.BaseCurrency)
	}
	if sched.ManualPriceCount != 2 {
		t.Errorf("manualPriceCount = %d, want 2", sched.ManualPriceCount)
	}
	if sched.AutomaticPriceCount != 3 {
		t.Errorf("automaticPriceCount = %d, want 3", sched.AutomaticPriceCount)
	}
	if base == nil {
		t.Fatal("expected basePrice, got nil")
	}
	if base.TerritoryID != "USA" {
		t.Errorf("base.TerritoryID = %q, want USA", base.TerritoryID)
	}
	if base.CustomerPrice != "9.99" {
		t.Errorf("base.CustomerPrice = %q, want 9.99", base.CustomerPrice)
	}
	if base.Proceeds != "6.99" {
		t.Errorf("base.Proceeds = %q, want 6.99", base.Proceeds)
	}
	if base.Currency != "USD" {
		t.Errorf("base.Currency = %q, want USD", base.Currency)
	}
}

func TestPricing_FixtureReplay_Availability(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appAvailabilityV2":                          {File: "pricing_availability"},
		"GET /v2/appAvailabilities/AVAIL-1234567890/territoryAvailabilities": {File: "pricing_territory_availabilities"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	avail, err := fetchAppAvailability(ctx, c, appID)
	if err != nil {
		t.Fatalf("fetchAppAvailability: %v", err)
	}
	if avail.AvailableTotal != 4 {
		t.Errorf("availableTotal = %d, want 4", avail.AvailableTotal)
	}
	if avail.AvailableCount != 3 {
		t.Errorf("availableCount = %d, want 3 (one MISSING_RATING is unavailable)", avail.AvailableCount)
	}
	if avail.AvailableInNewTerritories == nil || !*avail.AvailableInNewTerritories {
		t.Errorf("availableInNewTerritories = %v, want true", avail.AvailableInNewTerritories)
	}
}

func TestPricing_FixtureReplay_AppNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_notFound"},
	})
	c := fixtureASCClient(t, srv)
	_, err := resolveAppID(context.Background(), c, "com.unknown.app")
	if err == nil {
		t.Fatal("resolveAppID: want error, got nil")
	}
	if !strings.Contains(err.Error(), `"com.unknown.app"`) {
		t.Errorf("error %q does not name the bundleId", err.Error())
	}
}

func TestPricingSet_RegisteredOnGroup(t *testing.T) {
	var pcmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "pricing" {
			pcmd = c
			break
		}
	}
	if pcmd == nil {
		t.Fatal("pricing not on root")
	}
	var set *cobra.Command
	for _, sc := range pcmd.Commands() {
		if sc.Name() == "set" {
			set = sc
			break
		}
	}
	if set == nil {
		t.Fatal("pricing set not registered")
	}
	for _, want := range []string{"base-territory", "tier", "start-date", "end-date"} {
		if set.Flags().Lookup(want) == nil {
			t.Errorf("pricing set missing --%s", want)
		}
	}
}

// TestBuildPricingScheduleCreate_Shape: local id "${TIER}" wires the
// manualPrices linkage to the inline appPrice that carries the relationships.
func TestBuildPricingScheduleCreate_Shape(t *testing.T) {
	body := buildPricingScheduleCreate("APP-1", "USA", "PP-USA-999", "2025-01-01", "")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)
	for _, want := range []string{
		`"type":"appPriceSchedules"`,
		`"app":{"data":{"id":"APP-1","type":"apps"}}`,
		`"baseTerritory":{"data":{"id":"USA","type":"territories"}}`,
		`"manualPrices":{"data":[{"id":"${TIER}","type":"appPrices"}]}`,
		`"included":[{`,
		`"id":"${TIER}","relationships":{"appPricePoint":{"data":{"id":"PP-USA-999","type":"appPricePoints"}}`,
		`"territory":{"data":{"id":"USA","type":"territories"}}`,
		`"startDate":"2025-01-01"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q\nfull body: %s", want, out)
		}
	}
	if strings.Contains(out, `"endDate"`) {
		t.Errorf("body should omit endDate when empty: %s", out)
	}
}

func TestBuildPricingScheduleCreate_OmitsEmptyDates(t *testing.T) {
	body := buildPricingScheduleCreate("APP-1", "USA", "PP-USA-999", "", "")
	raw, _ := json.Marshal(body)
	out := string(raw)
	for _, leak := range []string{`"startDate"`, `"endDate"`} {
		if strings.Contains(out, leak) {
			t.Errorf("body should omit %s when not provided: %s", leak, out)
		}
	}
}

func TestPricingSetResult_TableRows_NoChange(t *testing.T) {
	r := &PricingSetResult{
		BundleID:      "com.example.alpha",
		AppID:         "1234567890",
		Changed:       false,
		BaseTerritory: "USA",
		PricePointID:  "PP-USA-999",
		ScheduleID:    "1234567890",
		Note:          "no change (idempotent): current schedule already matches",
	}
	_, rows := r.TableRows()
	foundNote := false
	for _, row := range rows {
		if row[0] == "NOTE" {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected NOTE row, rows=%v", rows)
	}
}

func TestPricingSetResult_JSONShape(t *testing.T) {
	r := &PricingSetResult{
		BundleID: "com.example.alpha", AppID: "1234567890",
		Changed: true, BaseTerritory: "USA", PricePointID: "PP-USA-999",
		ScheduleID: "9999999999", PreviousScheduleID: "1234567890",
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "appId", "changed", "baseTerritory", "pricePointId", "scheduleId", "previousScheduleId"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing key %q. Got: %v", key, mapKeys(decoded))
		}
	}
}

// TestFetchCurrentBaseSchedule_FixtureReplay: the active manual price covering
// today is MP-USA-1 → PP-USA-999.
func TestFetchCurrentBaseSchedule_FixtureReplay(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/appPriceSchedule": {File: "pricing_get"},
	})
	c := fixtureASCClient(t, srv)
	schedID, baseTerr, pricePoint, err := fetchCurrentBaseSchedule(context.Background(), c, "1234567890")
	if err != nil {
		t.Fatalf("fetchCurrentBaseSchedule: %v", err)
	}
	if schedID != "1234567890" {
		t.Errorf("schedID = %q, want 1234567890", schedID)
	}
	if baseTerr != "USA" {
		t.Errorf("baseTerr = %q, want USA", baseTerr)
	}
	if pricePoint != "PP-USA-999" {
		t.Errorf("pricePoint = %q, want PP-USA-999 (active 2020 → 2099)", pricePoint)
	}
}
