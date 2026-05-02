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
	"github.com/ul0gic/skipper/internal/asc"
)

// PricingView is the read-side view for `pricing get`. Combines a slice of
// the per-app price schedule with the per-territory availability summary.
//
// Field shape is intentionally flat at the top level so JSON consumers can
// reach every key with a single dot lookup. Nested schedule/availability
// objects keep their own fields stable so adding new top-level keys later
// doesn't break consumers parsing existing ones.
type PricingView struct {
	BundleID     string               `json:"bundleId"`
	Schedule     PriceScheduleSummary `json:"schedule"`
	Availability AvailabilitySummary  `json:"availability"`
	BasePrice    *PricePointSummary   `json:"basePrice,omitempty"`
}

// PriceScheduleSummary is the trimmed view of an app's AppPriceSchedule:
// scheduleId, base territory + its currency, and the count of manual /
// automatic price entries on the schedule. Detail beyond that is reachable
// via the JSON output's id pointers.
type PriceScheduleSummary struct {
	ID                  string `json:"id,omitempty"`
	BaseTerritoryID     string `json:"baseTerritoryId,omitempty"`
	BaseCurrency        string `json:"baseCurrency,omitempty"`
	ManualPriceCount    int    `json:"manualPriceCount"`
	AutomaticPriceCount int    `json:"automaticPriceCount"`
}

// PricePointSummary is the customer price + proceeds at a specific territory
// (typically the base territory). Both values are decimal strings — Apple's
// wire shape — to dodge float precision drift across currencies.
type PricePointSummary struct {
	TerritoryID   string `json:"territoryId,omitempty"`
	Currency      string `json:"currency,omitempty"`
	CustomerPrice string `json:"customerPrice,omitempty"`
	Proceeds      string `json:"proceeds,omitempty"`
	StartDate     string `json:"startDate,omitempty"`
	EndDate       string `json:"endDate,omitempty"`
}

// AvailabilitySummary covers the per-app availability resource. Counts are
// derived from the territoryAvailabilities collection; AvailableTotal is the
// number of entries; AvailableCount is the subset where available=true.
// AvailableInNewTerritories surfaces Apple's auto-release flag.
type AvailabilitySummary struct {
	ID                        string `json:"id,omitempty"`
	AvailableTotal            int    `json:"availableTotal"`
	AvailableCount            int    `json:"availableCount"`
	AvailableInNewTerritories *bool  `json:"availableInNewTerritories,omitempty"`
}

// TableRows for the pricing view. One row per scalar field; flatten the
// nested summaries for grep-friendliness. Unknown values render as empty
// rather than "(unknown)" because pricing has stable defaults that authors
// don't need a visual prompt for.
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

// priceWindow renders a start/end date pair. Empty endDate is "indefinite";
// empty startDate (rare) renders as just the end.
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
	Example: `  skipper pricing get com.example.myapp
  skipper pricing get com.example.myapp --output json | jq .basePrice
  skipper pricing get com.example.myapp --output json | jq '.availability.availableCount'`,
}

// pricingSetCmd publishes a single-base-territory price schedule via POST
// /v1/appPriceSchedules. The L1 verb keeps the surface narrow (one manual
// price = base territory + appPricePoint id). Multi-territory manual price
// schedules will live in L2 state-as-code.
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
	Example: `  skipper pricing set com.example.myapp --base-territory USA --tier PP-USA-999
  skipper pricing set com.example.myapp --base-territory USA --tier PP-USA-999 --output json`,
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
		// 404 on appPriceSchedule = app is free / never priced. Not fatal;
		// continue on to availability so users still get a partial view.
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

// fetchPriceSchedule pulls the schedule resource with manualPrices,
// automaticPrices, and baseTerritory all sideloaded in one request, then
// resolves the base territory's currency and the active manual price (if
// any) by walking the included resources. Returns 404 when the app has no
// schedule (rare; Apple usually creates one on first publish).
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

	// Resolve base currency from the included territory entry.
	if cur, ok := included.territories[sched.BaseTerritoryID]; ok {
		sched.BaseCurrency = cur
	}

	// Find the manual price for the base territory whose window covers
	// today (or the first manual price for the base territory if none
	// covers today).
	var basePrice *PricePointSummary
	if sched.BaseTerritoryID != "" {
		summary, pricePointID := pickActiveBasePrice(included, sched.BaseTerritoryID)
		if summary != nil {
			basePrice = summary
			// If a basePrice was found, fetch its appPricePoint for
			// customerPrice + proceeds. Apple does not expose those on the
			// included AppPriceV2 — they live on AppPricePointV3.
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

// pickActiveBasePrice scans the included AppPriceV2 entries for one whose
// territory == baseTerritoryID. Prefers a manual entry whose window covers
// today; falls back to the first manual entry; returns (nil, "") if none
// exists (the schedule has only automatic prices).
//
// Returns the public summary plus the linked appPricePoint id; the latter
// is needed for the follow-up /v3/appPricePoints/{id} fetch but should not
// appear in the JSON output, hence the side-channel return.
func pickActiveBasePrice(inc includedSet, baseTerritoryID string) (summary *PricePointSummary, pricePointID string) {
	today := time.Now().UTC().Format("2006-01-02")
	var fallback *PricePointSummary
	var fallbackPricePoint string
	for _, p := range inc.appPrices {
		if p.territoryID != baseTerritoryID {
			continue
		}
		// Only consider manual prices for the "current price" — automatic
		// prices are equalized from the base territory's manual entry, so
		// surfacing one would be circular.
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

// fetchAppAvailability pulls /v1/apps/{id}/appAvailabilityV2 with
// territoryAvailabilities sideloaded; counts available=true and totals.
func fetchAppAvailability(ctx context.Context, c *asc.Client, appID string) (AvailabilitySummary, error) {
	q := url.Values{
		"include":                         {"territoryAvailabilities"},
		"fields[appAvailabilities]":       {"availableInNewTerritories,territoryAvailabilities"},
		"fields[territoryAvailabilities]": {"available,releaseDate,preOrderEnabled,preOrderPublishDate,contentStatuses,territory"},
		"limit[territoryAvailabilities]":  {"200"},
	}
	resp, err := asc.Get[availabilitySingle](ctx, c, "/v1/apps/"+appID+"/appAvailabilityV2", q)
	if err != nil {
		return AvailabilitySummary{}, err
	}
	out := AvailabilitySummary{
		ID:                        resp.Data.ID,
		AvailableInNewTerritories: resp.Data.Attributes.AvailableInNewTerritories,
	}
	for _, raw := range resp.Included {
		var probe struct {
			Type       string                              `json:"type"`
			Attributes asc.TerritoryAvailabilityAttributes `json:"attributes"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.Type != "territoryAvailabilities" {
			continue
		}
		out.AvailableTotal++
		if probe.Attributes.Available != nil && *probe.Attributes.Available {
			out.AvailableCount++
		}
	}
	return out, nil
}

// priceScheduleSingle is the typed shape for /v1/apps/{id}/appPriceSchedule.
// Apple's response is a JSON:API single-resource envelope; we model only the
// fields Skipper reads. relationships.baseTerritory is to-one, the price
// arrays are to-many.
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

// availabilitySingle is the typed shape for
// /v1/apps/{id}/appAvailabilityV2.
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

// includedSet is the decoded view of an `included` array on a schedule
// response. We pre-extract the few fields Skipper needs so the schedule
// walker doesn't re-parse RawMessage on every lookup.
type includedSet struct {
	territories       map[string]string // id → currency
	appPrices         []includedPrice   // every appPrice entry, in order
	priceToPricePoint map[string]string // appPrice id → appPricePoint id
}

// includedPrice mirrors AppPriceV2 with just the fields the schedule walker
// reads. Decoded from the `included` slice once per fetch.
type includedPrice struct {
	id              string
	manual          *bool
	startDate       string
	endDate         string
	territoryID     string
	appPricePointID string
}

// decodeIncluded walks the `included` raw-message slice and pulls out the
// territory currencies plus appPriceV2 entries the schedule walker needs.
// Unknown types are silently skipped — Apple may add more includes per spec
// version and we don't want to hard-fail on them.
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

// PricingSetResult is the structured outcome of `pricing set`. Surfaces
// whether a POST was issued vs idempotent no-op so plan/apply consumers
// can detect zero-cost runs without re-querying.
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

// boolStrPricing renders a bool as "true"/"false". Local to this file to
// dodge cross-file helper collisions.
func boolStrPricing(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// runPricingSet implements the `pricing set` write. Pattern: resolve app,
// fetch current (baseTerritory, basePricePoint) from the live schedule,
// short-circuit when the requested pairing matches, otherwise POST a new
// schedule with one inline manual appPrice.
func runPricingSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	baseTerr := strings.TrimSpace(pricingSetBaseTerritory)
	tier := strings.TrimSpace(pricingSetTier)
	if baseTerr == "" || tier == "" {
		return fmt.Errorf("pricing: --base-territory and --tier are required")
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
			Note:               "no change (idempotent) — current schedule already matches",
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

// fetchCurrentBaseSchedule pulls the app's current schedule and returns
// (scheduleID, baseTerritoryID, baseAppPricePointID). 404 = no schedule
// yet (free app, freshly created); empty strings + nil error.
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

// buildPricingScheduleCreate crafts the JSON:API POST body for
// /v1/appPriceSchedules carrying one inline manual appPrice for the base
// territory. Apple matches the inline appPrice's local id against the
// schedule's manualPrices linkage; we use a deterministic literal so the
// body shape stays readable in fixture diffs.
//
// startDate / endDate are optional; empty omits the field (Apple defaults
// to "now" / "indefinite").
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
