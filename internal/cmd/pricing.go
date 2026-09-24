package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// PricingView is the read-side view for `pricing get`.
type PricingView struct {
	BundleID     string               `json:"bundleId"`
	Schedule     PriceScheduleSummary `json:"schedule"`
	Availability AvailabilitySummary  `json:"availability"`
	BasePrice    *PricePointSummary   `json:"basePrice,omitempty"`
}

// PriceScheduleSummary is the trimmed view of an app's AppPriceSchedule.
type PriceScheduleSummary struct {
	ID                  string `json:"id,omitempty"`
	BaseTerritoryID     string `json:"baseTerritoryId,omitempty"`
	BaseCurrency        string `json:"baseCurrency,omitempty"`
	ManualPriceCount    int    `json:"manualPriceCount"`
	AutomaticPriceCount int    `json:"automaticPriceCount"`
}

// PricePointSummary is the customer price + proceeds at a territory. Prices stay
// as Apple's decimal strings to avoid float precision drift across currencies.
type PricePointSummary struct {
	TerritoryID   string `json:"territoryId,omitempty"`
	Currency      string `json:"currency,omitempty"`
	CustomerPrice string `json:"customerPrice,omitempty"`
	Proceeds      string `json:"proceeds,omitempty"`
	StartDate     string `json:"startDate,omitempty"`
	EndDate       string `json:"endDate,omitempty"`
}

// AvailabilitySummary covers the per-app availability resource.
type AvailabilitySummary struct {
	ID                        string `json:"id,omitempty"`
	AvailableTotal            int    `json:"availableTotal"`
	AvailableCount            int    `json:"availableCount"`
	AvailableInNewTerritories *bool  `json:"availableInNewTerritories,omitempty"`
}

func (v *PricingView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"SCHEDULE_ID", v.Schedule.ID},
		{"BASE_TERRITORY", v.Schedule.BaseTerritoryID},
		{"BASE_CURRENCY", v.Schedule.BaseCurrency},
		{"MANUAL_PRICES", strconv.Itoa(v.Schedule.ManualPriceCount)},
		{"AUTOMATIC_PRICES", strconv.Itoa(v.Schedule.AutomaticPriceCount)},
	}
	if v.BasePrice != nil {
		rows = append(rows,
			[]string{"BASE_PRICE", fmt.Sprintf("%s %s (proceeds %s)", v.BasePrice.Currency, v.BasePrice.CustomerPrice, v.BasePrice.Proceeds)},
			[]string{"BASE_PRICE_WINDOW", priceWindow(v.BasePrice.StartDate, v.BasePrice.EndDate)},
		)
	} else {
		rows = append(rows, []string{"BASE_PRICE", "(no manual price; auto-equalized)"})
	}
	rows = append(rows,
		[]string{"AVAILABILITY_ID", v.Availability.ID},
		[]string{"AVAILABLE_TOTAL", strconv.Itoa(v.Availability.AvailableTotal)},
		[]string{"AVAILABLE_COUNT", strconv.Itoa(v.Availability.AvailableCount)},
		[]string{"AVAILABLE_IN_NEW", boolPtrStr(v.Availability.AvailableInNewTerritories)},
	)
	return headers, rows
}

// priceWindow renders a start/end date pair; empty endDate is "indefinite".
func priceWindow(start, end string) string {
	switch {
	case start == "" && end == "":
		return ""
	case end == "":
		return start + " → indefinite"
	case start == "":
		return "until " + end
	default:
		return start + " → " + end
	}
}

var pricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Inspect App Store pricing and availability",
	Long: `pricing groups read commands over the /v1/appPriceSchedules and
/v1/apps/{id}/appAvailabilityV2 resources.

Apple's pricing model uses AppPriceSchedule (one per app) carrying
manual/automatic price windows that link to AppPricePointV3 entries
(customerPrice + proceeds per territory). AppPriceTier is deprecated.

Availability lives in a separate resource: a flag for new-territory
auto-release plus the per-territory availability set.`,
}

var pricingGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Show the price schedule and availability summary for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPricingGet,
	Example: `  flightline pricing get com.example.myapp
  flightline pricing get com.example.myapp --output json | jq .basePrice
  flightline pricing get com.example.myapp --output json | jq '.availability.availableCount'`,
}

// pricingSetCmd publishes a single-base-territory price schedule (one manual
// price = base territory + appPricePoint id).
var pricingSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Apply a base-territory price schedule (idempotent against current schedule)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPricingSet,
	Long: `pricing set creates a new AppPriceSchedule for the app. Apple's pricing
model is replace-by-create: the new schedule supersedes any prior one.

L1 supports a single base-territory + appPricePoint pairing. Pass:
  --base-territory <code>   ISO-3 territory code (e.g. USA, GBR, JPN)
  --tier <pricePointId>     AppPricePointV3 id

Preserves unrelated manual prices and scheduled windows. Optional dates
select a price window; conflicting future schedules fail before writing.
An already matching requested window sends no POST and reports changed=false.`,
	Example: `  flightline pricing set com.example.myapp --base-territory USA --tier PP-USA-999
  flightline pricing set com.example.myapp --base-territory USA --tier PP-USA-999 --output json`,
}

var (
	pricingSetBaseTerritory string
	pricingSetTier          string
	pricingSetStartDate     string
	pricingSetEndDate       string
)

func init() {
	pricingSetCmd.Flags().StringVar(&pricingSetBaseTerritory, "base-territory", "", "ISO-3 territory code (e.g. USA)")
	pricingSetCmd.Flags().StringVar(&pricingSetTier, "tier", "", "AppPricePointV3 id")
	pricingSetCmd.Flags().StringVar(&pricingSetStartDate, "start-date", "", "manual-price start date (YYYY-MM-DD); empty = no lower bound")
	pricingSetCmd.Flags().StringVar(&pricingSetEndDate, "end-date", "", "manual-price end date (YYYY-MM-DD); empty = indefinite")
	_ = pricingSetCmd.MarkFlagRequired("base-territory")
	_ = pricingSetCmd.MarkFlagRequired("tier")

	pricingCmd.AddCommand(pricingGetCmd)
	pricingCmd.AddCommand(pricingSetCmd)
	rootCmd.AddCommand(pricingCmd)
}

func runPricingGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	view := &PricingView{BundleID: bundleID}

	if sched, basePrice, err := fetchPriceSchedule(cmd.Context(), c, appID); err != nil {
		// 404 = free / never-priced app; not fatal, continue to availability.
		var apiErr *asc.APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 404 {
			return err
		}
	} else {
		view.Schedule = sched
		view.BasePrice = basePrice
	}

	avail, err := fetchAppAvailability(cmd.Context(), c, appID)
	if err != nil {
		var apiErr *asc.APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 404 {
			return err
		}
	} else {
		view.Availability = avail
	}

	return Render(view, outputMode())
}

// fetchPriceSchedule sideloads manualPrices, automaticPrices, and baseTerritory
// in one request, then resolves base currency and the active manual price.
func fetchPriceSchedule(ctx context.Context, c *asc.Client, appID string) (PriceScheduleSummary, *PricePointSummary, error) {
	inventory, err := asc.ReadManualPriceSchedule(ctx, c, appID)
	if err != nil {
		return PriceScheduleSummary{}, nil, err
	}
	summary := PriceScheduleSummary{ID: inventory.ID, BaseTerritoryID: inventory.BaseTerritoryID, ManualPriceCount: len(inventory.Prices)}
	if inventory.ID == "" {
		return summary, nil, nil
	}
	summary.AutomaticPriceCount, err = countSchedulePrices(ctx, c, inventory.ID, "automaticPrices")
	if err != nil {
		return PriceScheduleSummary{}, nil, err
	}
	territory, err := asc.Get[asc.Single[asc.TerritoryAttributes]](ctx, c, "/v1/appPriceSchedules/"+inventory.ID+"/baseTerritory", nil)
	if err != nil {
		return PriceScheduleSummary{}, nil, err
	}
	summary.BaseCurrency = territory.Data.Attributes.Currency
	active, err := inventory.ActiveBasePrice(time.Now())
	if err != nil {
		return PriceScheduleSummary{}, nil, err
	}
	if active.PricePointID == "" {
		return summary, nil, nil
	}
	point, err := asc.Get[asc.Single[asc.AppPricePointAttributes]](ctx, c, "/v3/appPricePoints/"+active.PricePointID, nil)
	if err != nil {
		return PriceScheduleSummary{}, nil, err
	}
	price := &PricePointSummary{TerritoryID: active.TerritoryID, Currency: summary.BaseCurrency, CustomerPrice: point.Data.Attributes.CustomerPrice, Proceeds: point.Data.Attributes.Proceeds}
	today := time.Now().UTC().Format("2006-01-02")
	for _, row := range inventory.Prices {
		if row.TerritoryID == active.TerritoryID && row.PricePointID == active.PricePointID && windowCovers(today, row.StartDate, row.EndDate) {
			price.StartDate, price.EndDate = row.StartDate, row.EndDate
			break
		}
	}
	return summary, price, nil
}

func countSchedulePrices(ctx context.Context, c *asc.Client, scheduleID, relationship string) (int, error) {
	q := url.Values{
		"fields[appPrices]": {"manual"},
		"limit":             {"200"},
	}
	total := 0
	path := "/v1/appPriceSchedules/" + scheduleID + "/" + relationship
	for page, err := range asc.Pages[struct{}](ctx, c, path, q) {
		if err != nil {
			return 0, fmt.Errorf("counting %s: %w", relationship, err)
		}
		total += len(page.Data)
	}
	return total, nil
}

// pickActiveBasePrice finds the base-territory manual price covering today
// (else the first). pricePointID feeds the /v3/appPricePoints fetch, not JSON.

// windowCovers reports whether `today` (YYYY-MM-DD) falls inside [start, end).
// Empty start = no lower bound; empty end = no upper bound.
func windowCovers(today, start, end string) bool {
	if start != "" && today < start {
		return false
	}
	if end != "" && today >= end {
		return false
	}
	return true
}

// fetchAppAvailability sideloads territoryAvailabilities and counts
// available=true against the total.
func fetchAppAvailability(ctx context.Context, c *asc.Client, appID string) (AvailabilitySummary, error) {
	q := url.Values{
		"fields[appAvailabilities]": {"availableInNewTerritories"},
	}
	resp, err := asc.Get[availabilitySingle](ctx, c, "/v1/apps/"+appID+"/appAvailabilityV2", q)
	if err != nil {
		return AvailabilitySummary{}, err
	}
	out := AvailabilitySummary{
		ID:                        resp.Data.ID,
		AvailableInNewTerritories: resp.Data.Attributes.AvailableInNewTerritories,
	}
	tq := url.Values{
		"fields[territoryAvailabilities]": {"available,releaseDate,preOrderEnabled,preOrderPublishDate,contentStatuses"},
		"limit":                           {"200"},
	}
	path := "/v2/appAvailabilities/" + resp.Data.ID + "/territoryAvailabilities"
	for page, err := range asc.Pages[asc.TerritoryAvailabilityAttributes](ctx, c, path, tq) {
		if err != nil {
			return AvailabilitySummary{}, err
		}
		for _, ta := range page.Data {
			out.AvailableTotal++
			if ta.Attributes.Available != nil && *ta.Attributes.Available {
				out.AvailableCount++
			}
		}
	}
	return out, nil
}

// availabilitySingle is the typed shape for /v1/apps/{id}/appAvailabilityV2.
type availabilitySingle struct {
	Data struct {
		ID         string                        `json:"id"`
		Type       string                        `json:"type"`
		Attributes asc.AppAvailabilityAttributes `json:"attributes"`
	} `json:"data"`
	Included []json.RawMessage `json:"included,omitempty"`
}

type PricingSetResult struct {
	BundleID           string `json:"bundleId"`
	AppID              string `json:"appId"`
	Changed            bool   `json:"changed"`
	BaseTerritory      string `json:"baseTerritory"`
	PricePointID       string `json:"pricePointId"`
	ScheduleID         string `json:"scheduleId,omitempty"`
	PreviousScheduleID string `json:"previousScheduleId,omitempty"`
	Note               string `json:"note,omitempty"`
}

// TableRows for a pricing set result.
func (r *PricingSetResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", r.BundleID},
		{"APP_ID", r.AppID},
		{"CHANGED", boolStrPricing(r.Changed)},
		{"BASE_TERRITORY", r.BaseTerritory},
		{"PRICE_POINT_ID", r.PricePointID},
		{"SCHEDULE_ID", r.ScheduleID},
		{"PREVIOUS_SCHEDULE_ID", r.PreviousScheduleID},
	}
	if r.Note != "" {
		rows = append(rows, []string{"NOTE", r.Note})
	}
	return headers, rows
}

func boolStrPricing(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func runPricingSet(cmd *cobra.Command, args []string) error {
	baseTerr, point := strings.TrimSpace(pricingSetBaseTerritory), strings.TrimSpace(pricingSetTier)
	if baseTerr == "" || point == "" {
		return errors.New("pricing: --base-territory and --tier are required")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	result, err := setPricing(cmd.Context(), c, args[0], baseTerr, point, pricingSetStartDate, pricingSetEndDate)
	if err != nil {
		return err
	}
	return Render(result, outputMode())
}

func setPricing(ctx context.Context, c *asc.Client, bundleID, territory, point, start, end string) (*PricingSetResult, error) {
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return nil, err
	}
	current, err := asc.ReadActiveBasePrice(ctx, c, appID, time.Now())
	if err != nil {
		return nil, err
	}
	var expected *asc.ActiveBasePrice
	if current.ScheduleID != "" {
		expected = &current
	}
	written, err := asc.CreatePreservingPriceWindow(ctx, c, appID, territory, point, start, end, expected, time.Now())
	if err != nil {
		return nil, err
	}
	result := &PricingSetResult{BundleID: bundleID, AppID: appID, Changed: written.Changed, BaseTerritory: territory, PricePointID: point, ScheduleID: written.ScheduleID, PreviousScheduleID: current.ScheduleID}
	if !written.Changed {
		result.Note = "no change (idempotent): requested price window already matches"
	}
	return result, nil
}

// fetchCurrentBaseSchedule returns (scheduleID, baseTerritoryID,
// baseAppPricePointID). 404 (no schedule yet) yields empty strings + nil error.
func fetchCurrentBaseSchedule(ctx context.Context, c *asc.Client, appID string) (schedID, baseTerritory, basePricePoint string, err error) {
	current, err := asc.ReadActiveBasePrice(ctx, c, appID, time.Now())
	return current.ScheduleID, current.TerritoryID, current.PricePointID, err
}
