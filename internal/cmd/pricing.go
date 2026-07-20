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

Idempotent: if the current schedule already has the requested
(baseTerritory, appPricePoint) pairing, no POST is issued and the
result reports changed=false.`,
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
	q := url.Values{
		"include":                   {"manualPrices,automaticPrices,baseTerritory"},
		"fields[appPriceSchedules]": {"baseTerritory,manualPrices,automaticPrices"},
		"fields[appPrices]":         {"manual,startDate,endDate,territory,appPricePoint"},
		"fields[territories]":       {"currency"},
		"limit[manualPrices]":       {"50"},
		"limit[automaticPrices]":    {"50"},
	}
	resp, err := asc.Get[priceScheduleSingle](ctx, c, "/v1/apps/"+appID+"/appPriceSchedule", q)
	if err != nil {
		return PriceScheduleSummary{}, nil, err
	}

	sched := PriceScheduleSummary{ID: resp.Data.ID}
	if resp.Data.Relationships.BaseTerritory != nil && resp.Data.Relationships.BaseTerritory.Data != nil {
		sched.BaseTerritoryID = resp.Data.Relationships.BaseTerritory.Data.ID
	}
	if resp.Data.Relationships.ManualPrices != nil {
		sched.ManualPriceCount = len(resp.Data.Relationships.ManualPrices.Data)
	}
	if resp.Data.Relationships.AutomaticPrices != nil {
		sched.AutomaticPriceCount = len(resp.Data.Relationships.AutomaticPrices.Data)
	}

	included := decodeIncluded(resp.Included)

	if cur, ok := included.territories[sched.BaseTerritoryID]; ok {
		sched.BaseCurrency = cur
	}

	var basePrice *PricePointSummary
	if sched.BaseTerritoryID != "" {
		summary, pricePointID := pickActiveBasePrice(included, sched.BaseTerritoryID)
		if summary != nil {
			basePrice = summary
			// customerPrice + proceeds live on AppPricePointV3, not the
			// included AppPriceV2, so fetch the price point separately.
			if pricePointID != "" {
				pt, perr := asc.Get[asc.Single[asc.AppPricePointAttributes]](
					ctx, c, "/v3/appPricePoints/"+pricePointID, nil,
				)
				if perr == nil {
					basePrice.CustomerPrice = pt.Data.Attributes.CustomerPrice
					basePrice.Proceeds = pt.Data.Attributes.Proceeds
				}
			}
			if cur, ok := included.territories[basePrice.TerritoryID]; ok {
				basePrice.Currency = cur
			}
		}
	}

	return sched, basePrice, nil
}

// pickActiveBasePrice finds the base-territory manual price covering today
// (else the first). pricePointID feeds the /v3/appPricePoints fetch, not JSON.
func pickActiveBasePrice(inc includedSet, baseTerritoryID string) (summary *PricePointSummary, pricePointID string) {
	today := time.Now().UTC().Format("2006-01-02")
	var fallback *PricePointSummary
	var fallbackPricePoint string
	for _, p := range inc.appPrices {
		if p.territoryID != baseTerritoryID {
			continue
		}
		// Automatic prices are equalized from the manual base entry, so
		// surfacing one as the current price would be circular.
		if p.manual == nil || !*p.manual {
			continue
		}
		row := &PricePointSummary{
			TerritoryID: p.territoryID,
			StartDate:   p.startDate,
			EndDate:     p.endDate,
		}
		if windowCovers(today, p.startDate, p.endDate) {
			return row, p.appPricePointID
		}
		if fallback == nil {
			fallback = row
			fallbackPricePoint = p.appPricePointID
		}
	}
	return fallback, fallbackPricePoint
}

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

// priceScheduleSingle is the typed shape for /v1/apps/{id}/appPriceSchedule,
// modelling only the fields Flightline reads.
type priceScheduleSingle struct {
	Data struct {
		ID            string `json:"id"`
		Type          string `json:"type"`
		Attributes    asc.AppPriceScheduleAttributes
		Relationships struct {
			BaseTerritory   *toOneRel  `json:"baseTerritory,omitempty"`
			ManualPrices    *toManyRel `json:"manualPrices,omitempty"`
			AutomaticPrices *toManyRel `json:"automaticPrices,omitempty"`
		} `json:"relationships"`
	} `json:"data"`
	Included []json.RawMessage `json:"included,omitempty"`
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

// toOneRel matches Apple's to-one relationship envelope.
type toOneRel struct {
	Data *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"data"`
}

// toManyRel matches Apple's to-many relationship envelope.
type toManyRel struct {
	Data []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"data"`
}

// includedSet pre-extracts the fields Flightline needs from a schedule's
// `included` array so the walker doesn't re-parse RawMessage per lookup.
type includedSet struct {
	territories       map[string]string // id → currency
	appPrices         []includedPrice   // every appPrice entry, in order
	priceToPricePoint map[string]string // appPrice id → appPricePoint id
}

// includedPrice mirrors AppPriceV2 with just the fields the schedule walker reads.
type includedPrice struct {
	id              string
	manual          *bool
	startDate       string
	endDate         string
	territoryID     string
	appPricePointID string
}

// decodeIncluded extracts territory currencies and appPriceV2 entries from the
// `included` slice. Unknown types are skipped: Apple may add includes per spec version.
func decodeIncluded(raw []json.RawMessage) includedSet {
	out := includedSet{
		territories:       make(map[string]string),
		priceToPricePoint: make(map[string]string),
	}
	for _, msg := range raw {
		var probe struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(msg, &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "territories":
			var t struct {
				Attributes asc.TerritoryAttributes `json:"attributes"`
			}
			if err := json.Unmarshal(msg, &t); err == nil {
				out.territories[probe.ID] = t.Attributes.Currency
			}
		case "appPrices":
			var p struct {
				Attributes    asc.AppPriceAttributes `json:"attributes"`
				Relationships struct {
					Territory     *toOneRel `json:"territory,omitempty"`
					AppPricePoint *toOneRel `json:"appPricePoint,omitempty"`
				} `json:"relationships"`
			}
			if err := json.Unmarshal(msg, &p); err != nil {
				continue
			}
			ip := includedPrice{
				id:        probe.ID,
				manual:    p.Attributes.Manual,
				startDate: p.Attributes.StartDate,
				endDate:   p.Attributes.EndDate,
			}
			if p.Relationships.Territory != nil && p.Relationships.Territory.Data != nil {
				ip.territoryID = p.Relationships.Territory.Data.ID
			}
			if p.Relationships.AppPricePoint != nil && p.Relationships.AppPricePoint.Data != nil {
				ip.appPricePointID = p.Relationships.AppPricePoint.Data.ID
				out.priceToPricePoint[probe.ID] = ip.appPricePointID
			}
			out.appPrices = append(out.appPrices, ip)
		}
	}
	return out
}

// PricingSetResult is the outcome of `pricing set`; Changed reports whether a
// POST was issued so consumers can detect idempotent no-ops.
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
	bundleID := args[0]
	baseTerr := strings.TrimSpace(pricingSetBaseTerritory)
	tier := strings.TrimSpace(pricingSetTier)
	if baseTerr == "" || tier == "" {
		return errors.New("pricing: --base-territory and --tier are required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	curSchedID, curBaseTerr, curPricePoint, err := fetchCurrentBaseSchedule(cmd.Context(), c, appID)
	if err != nil {
		return err
	}

	if curBaseTerr == baseTerr && curPricePoint == tier {
		return Render(&PricingSetResult{
			BundleID:           bundleID,
			AppID:              appID,
			Changed:            false,
			BaseTerritory:      baseTerr,
			PricePointID:       tier,
			ScheduleID:         curSchedID,
			PreviousScheduleID: curSchedID,
			Note:               "no change (idempotent): current schedule already matches",
		}, outputMode())
	}

	body := buildPricingScheduleCreate(appID, baseTerr, tier, pricingSetStartDate, pricingSetEndDate)
	resp, err := asc.Post[asc.Single[asc.AppPriceScheduleAttributes]](
		cmd.Context(), c, "/v1/appPriceSchedules", nil, body,
	)
	if err != nil {
		return err
	}

	return Render(&PricingSetResult{
		BundleID:           bundleID,
		AppID:              appID,
		Changed:            true,
		BaseTerritory:      baseTerr,
		PricePointID:       tier,
		ScheduleID:         resp.Data.ID,
		PreviousScheduleID: curSchedID,
	}, outputMode())
}

// fetchCurrentBaseSchedule returns (scheduleID, baseTerritoryID,
// baseAppPricePointID). 404 (no schedule yet) yields empty strings + nil error.
func fetchCurrentBaseSchedule(ctx context.Context, c *asc.Client, appID string) (schedID, baseTerritory, basePricePoint string, err error) {
	q := url.Values{
		"include":                   {"manualPrices,baseTerritory"},
		"fields[appPriceSchedules]": {"baseTerritory,manualPrices"},
		"fields[appPrices]":         {"manual,startDate,endDate,territory,appPricePoint"},
		"limit[manualPrices]":       {"50"},
	}
	resp, err := asc.Get[priceScheduleSingle](ctx, c, "/v1/apps/"+appID+"/appPriceSchedule", q)
	if err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return "", "", "", nil
		}
		return "", "", "", err
	}

	schedID = resp.Data.ID
	if resp.Data.Relationships.BaseTerritory != nil && resp.Data.Relationships.BaseTerritory.Data != nil {
		baseTerritory = resp.Data.Relationships.BaseTerritory.Data.ID
	}
	included := decodeIncluded(resp.Included)
	today := time.Now().UTC().Format("2006-01-02")
	for _, p := range included.appPrices {
		if p.territoryID != baseTerritory {
			continue
		}
		if p.manual == nil || !*p.manual {
			continue
		}
		if windowCovers(today, p.startDate, p.endDate) {
			basePricePoint = p.appPricePointID
			return schedID, baseTerritory, basePricePoint, nil
		}
		if basePricePoint == "" {
			basePricePoint = p.appPricePointID
		}
	}
	return schedID, baseTerritory, basePricePoint, nil
}

// buildPricingScheduleCreate crafts the POST body with one inline manual
// appPrice; empty startDate/endDate omit the fields (Apple defaults now/indefinite).
func buildPricingScheduleCreate(appID, baseTerritory, pricePointID, startDate, endDate string) map[string]any {
	const localPriceID = "${TIER}"

	priceAttrs := map[string]any{"manual": true}
	if startDate != "" {
		priceAttrs["startDate"] = startDate
	}
	if endDate != "" {
		priceAttrs["endDate"] = endDate
	}

	inlinePrice := map[string]any{
		"type":       "appPrices",
		"id":         localPriceID,
		"attributes": priceAttrs,
		"relationships": map[string]any{
			"territory": map[string]any{
				"data": map[string]any{"type": "territories", "id": baseTerritory},
			},
			"appPricePoint": map[string]any{
				"data": map[string]any{"type": "appPricePoints", "id": pricePointID},
			},
		},
	}

	return map[string]any{
		"data": map[string]any{
			"type": "appPriceSchedules",
			"relationships": map[string]any{
				"app": map[string]any{
					"data": map[string]any{"type": "apps", "id": appID},
				},
				"baseTerritory": map[string]any{
					"data": map[string]any{"type": "territories", "id": baseTerritory},
				},
				"manualPrices": map[string]any{
					"data": []map[string]any{
						{"type": "appPrices", "id": localPriceID},
					},
				},
			},
		},
		"included": []map[string]any{inlinePrice},
	}
}
