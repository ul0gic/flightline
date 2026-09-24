package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ActiveBasePrice is the current manual price for a schedule's base territory.
// An empty PricePointID means no manual price is active at the requested date.
type ActiveBasePrice struct {
	ScheduleID   string
	TerritoryID  string
	PricePointID string
}

// ManualSchedulePrice is one existing manual price that a replacement schedule
// must carry forward unless it is the active price being changed.
type ManualSchedulePrice struct {
	ID, TerritoryID, PricePointID, StartDate, EndDate string
}

type ManualPriceSchedule struct {
	ID, BaseTerritoryID string
	Prices              []ManualSchedulePrice
}

func (schedule ManualPriceSchedule) ActiveBasePrice(at time.Time) (ActiveBasePrice, error) {
	return activeBasePrice(schedule, at.UTC().Format("2006-01-02"))
}

type PriceWindowResult struct {
	ScheduleID string
	Changed    bool
}

// ReadManualPriceSchedule returns the complete manual-price inventory for a
// schedule. A missing schedule has an empty ID and no prices.
func ReadManualPriceSchedule(ctx context.Context, c *Client, appID string) (ManualPriceSchedule, error) {
	base, err := readBasePriceSchedule(ctx, c, appID)
	if err != nil {
		return ManualPriceSchedule{}, err
	}
	out := ManualPriceSchedule{ID: base.ScheduleID, BaseTerritoryID: base.TerritoryID}
	if out.ID == "" {
		return out, nil
	}
	q := url.Values{
		"fields[appPrices]": {"manual,startDate,endDate,territory,appPricePoint"},
		"include":           {"territory,appPricePoint"},
		"limit":             {"200"},
	}
	for page, err := range Pages[AppPriceAttributes](ctx, c, "/v1/appPriceSchedules/"+out.ID+"/manualPrices", q) {
		if err != nil {
			return ManualPriceSchedule{}, fmt.Errorf("list schedule %s manual prices: %w", out.ID, err)
		}
		for _, row := range page.Data {
			price, err := projectManualSchedulePrice(row)
			if err != nil {
				return ManualPriceSchedule{}, err
			}
			out.Prices = append(out.Prices, price)
		}
	}
	return out, nil
}

func projectManualSchedulePrice(row Resource[AppPriceAttributes]) (ManualSchedulePrice, error) {
	price := ManualSchedulePrice{ID: row.ID, StartDate: row.Attributes.StartDate, EndDate: row.Attributes.EndDate}
	if row.Attributes.Manual != nil && !*row.Attributes.Manual {
		return price, fmt.Errorf("manual price %s is not manual", row.ID)
	}
	var err error
	price.TerritoryID, err = priceRelationshipID(row.Relationships["territory"])
	if err != nil || price.TerritoryID == "" {
		return ManualSchedulePrice{}, fmt.Errorf("manual price %s territory: %w", row.ID, missingPriceRelationship(err))
	}
	price.PricePointID, err = priceRelationshipID(row.Relationships["appPricePoint"])
	if err != nil || price.PricePointID == "" {
		return ManualSchedulePrice{}, fmt.Errorf("manual price %s appPricePoint: %w", row.ID, missingPriceRelationship(err))
	}
	if err := validatePriceDate(price.StartDate); err != nil {
		return ManualSchedulePrice{}, fmt.Errorf("manual price %s startDate: %w", row.ID, err)
	}
	if err := validatePriceDate(price.EndDate); err != nil {
		return ManualSchedulePrice{}, fmt.Errorf("manual price %s endDate: %w", row.ID, err)
	}
	return price, nil
}

func missingPriceRelationship(err error) error {
	if err != nil {
		return err
	}
	return errors.New("relationship has no resource id")
}

// ReadPricePointTerritory checks the point's own territory before a schedule
// write. A price point from another territory cannot satisfy the desired pair.
func ReadPricePointTerritory(ctx context.Context, c *Client, pricePointID string) (string, error) {
	q := url.Values{"fields[appPricePoints]": {"territory"}}
	resp, err := Get[Single[AppPricePointAttributes]](ctx, c, "/v3/appPricePoints/"+pricePointID, q)
	if err != nil {
		return "", fmt.Errorf("read appPricePoint %s: %w", pricePointID, err)
	}
	if resp.Data.ID != pricePointID {
		return "", fmt.Errorf("appPricePoint %s: response id mismatch", pricePointID)
	}
	rel, ok := resp.Data.Relationships["territory"]
	if !ok {
		return "", fmt.Errorf("appPricePoint %s: missing territory relationship", pricePointID)
	}
	terr, err := priceRelationshipID(rel)
	if err != nil || terr == "" {
		return "", fmt.Errorf("appPricePoint %s territory: %w", pricePointID, missingPriceRelationship(err))
	}
	return terr, nil
}

// CreatePreservingPriceSchedule replaces the active price in targetTerritory
// and carries every other manual window into the new schedule request.
// expected is the pair observed during planning; nil expects no schedule.
func CreatePreservingPriceSchedule(ctx context.Context, c *Client, appID, targetTerritory, targetPoint string, expected *ActiveBasePrice, at time.Time) error {
	_, err := CreatePreservingPriceWindow(ctx, c, appID, targetTerritory, targetPoint, "", "", expected, at)
	return err
}

// CreatePreservingPriceWindow writes a price window while retaining every
// existing manual interval outside that window. Empty startDate means today.
// Empty endDate means indefinite unless an implicit current change must stop
// at an already scheduled future price.
func CreatePreservingPriceWindow(ctx context.Context, c *Client, appID, targetTerritory, targetPoint, startDate, endDate string, expected *ActiveBasePrice, at time.Time) (PriceWindowResult, error) {
	today := at.UTC().Format("2006-01-02")
	start, end, err := requestedPriceWindow(startDate, endDate, today)
	if err != nil {
		return PriceWindowResult{}, err
	}
	schedule, err := ReadManualPriceSchedule(ctx, c, appID)
	if err != nil {
		return PriceWindowResult{}, err
	}
	current, err := schedule.ActiveBasePrice(at)
	if err != nil {
		return PriceWindowResult{}, err
	}
	if err := validateExpectedBasePrice(current, expected); err != nil {
		return PriceWindowResult{}, err
	}
	pointTerritory, err := ReadPricePointTerritory(ctx, c, targetPoint)
	if err != nil {
		return PriceWindowResult{}, err
	}
	if pointTerritory != targetTerritory {
		return PriceWindowResult{}, fmt.Errorf("appPricePoint %s belongs to %s, not base territory %s", targetPoint, pointTerritory, targetTerritory)
	}
	if priceWindowMatches(schedule, targetTerritory, targetPoint, start, end, startDate == "") {
		return PriceWindowResult{ScheduleID: schedule.ID}, nil
	}
	body, err := buildPreservingPriceWindow(appID, targetTerritory, targetPoint, schedule, today, startDate, endDate)
	if err != nil {
		return PriceWindowResult{}, err
	}
	resp, err := Post[Single[AppPriceScheduleAttributes]](ctx, c, "/v1/appPriceSchedules", nil, body)
	if err != nil {
		return PriceWindowResult{}, fmt.Errorf("create app price schedule: %w", err)
	}
	if resp.Data.ID == "" {
		return PriceWindowResult{}, errors.New("created app price schedule response missing resource id")
	}
	return PriceWindowResult{ScheduleID: resp.Data.ID, Changed: true}, nil
}

func priceWindowMatches(schedule ManualPriceSchedule, territory, point, start, end string, implicitStart bool) bool {
	if schedule.BaseTerritoryID != territory {
		return false
	}
	for _, price := range schedule.Prices {
		if price.TerritoryID != territory || price.PricePointID != point {
			continue
		}
		if (implicitStart && end == "" && priceCovers(start, price)) || (!implicitStart && price.StartDate == start && price.EndDate == end) {
			return true
		}
	}
	return false
}

func validateExpectedBasePrice(current ActiveBasePrice, expected *ActiveBasePrice) error {
	if expected == nil {
		if current.ScheduleID != "" {
			return errors.New("app price schedule changed since planning: expected no schedule")
		}
		return nil
	}
	if current.TerritoryID != expected.TerritoryID || current.PricePointID != expected.PricePointID {
		return fmt.Errorf("app price schedule changed since planning: expected %s/%s, found %s/%s", expected.TerritoryID, expected.PricePointID, current.TerritoryID, current.PricePointID)
	}
	return nil
}

func buildPreservingPriceSchedule(appID, territory, point string, schedule ManualPriceSchedule, today string) (map[string]any, error) {
	return buildPreservingPriceWindow(appID, territory, point, schedule, today, "", "")
}

func buildPreservingPriceWindow(appID, territory, point string, schedule ManualPriceSchedule, today, startDate, endDate string) (map[string]any, error) {
	start, end, err := requestedPriceWindow(startDate, endDate, today)
	if err != nil {
		return nil, err
	}
	prices, replacement, err := spliceManualPrices(schedule.Prices, territory, point, start, end, today, startDate == "")
	if err != nil {
		return nil, err
	}
	prices = append(prices, replacement)
	links := make([]map[string]any, 0, len(prices))
	included := make([]map[string]any, 0, len(prices))
	for i, price := range prices {
		localID := fmt.Sprintf("${newprice-%d}", i)
		links = append(links, map[string]any{"type": "appPrices", "id": localID})
		attrs := map[string]any{}
		if price.StartDate != "" {
			attrs["startDate"] = price.StartDate
		}
		if price.EndDate != "" {
			attrs["endDate"] = price.EndDate
		}
		included = append(included, map[string]any{
			"type": "appPrices", "id": localID, "attributes": attrs,
			"relationships": map[string]any{"appPricePoint": map[string]any{"data": map[string]any{"type": "appPricePoints", "id": price.PricePointID}}},
		})
	}
	return map[string]any{
		"data": map[string]any{"type": "appPriceSchedules", "relationships": map[string]any{
			"app":           map[string]any{"data": map[string]any{"type": "apps", "id": appID}},
			"baseTerritory": map[string]any{"data": map[string]any{"type": "territories", "id": territory}},
			"manualPrices":  map[string]any{"data": links},
		}},
		"included": included,
	}, nil
}

func requestedPriceWindow(startDate, endDate, today string) (start, end string, err error) {
	if err := validatePriceDate(startDate); err != nil {
		return "", "", fmt.Errorf("invalid startDate: %w", err)
	}
	if err := validatePriceDate(endDate); err != nil {
		return "", "", fmt.Errorf("invalid endDate: %w", err)
	}
	start = startDate
	if start == "" {
		start = today
	}
	if start < today {
		return "", "", fmt.Errorf("startDate %s is in the past", start)
	}
	if endDate != "" && endDate <= start {
		return "", "", fmt.Errorf("endDate %s must follow startDate %s", endDate, start)
	}
	return start, endDate, nil
}

func spliceManualPrices(existing []ManualSchedulePrice, territory, point, start, end, today string, implicitStart bool) (prices []ManualSchedulePrice, replacement ManualSchedulePrice, err error) {
	prices = make([]ManualSchedulePrice, 0, len(existing)+2)
	replacement = ManualSchedulePrice{TerritoryID: territory, PricePointID: point, StartDate: start, EndDate: end}
	covering := 0
	for _, price := range existing {
		if price.TerritoryID != territory {
			prices = append(prices, price)
			continue
		}
		parts, newEnd, replaces, err := spliceTargetPrice(price, start, replacement.EndDate, today, implicitStart)
		if err != nil {
			return nil, ManualSchedulePrice{}, err
		}
		covering += replaces
		if covering > 1 {
			return nil, ManualSchedulePrice{}, errors.New("multiple active manual prices in target territory")
		}
		prices = append(prices, parts...)
		replacement.EndDate = newEnd
	}
	if replacement.EndDate != "" && replacement.EndDate <= start {
		return nil, ManualSchedulePrice{}, errors.New("replacement window has no duration")
	}
	return prices, replacement, nil
}

func spliceTargetPrice(price ManualSchedulePrice, start, end, today string, implicitStart bool) (parts []ManualSchedulePrice, replacementEnd string, replaced int, err error) {
	if price.EndDate != "" && price.StartDate != "" && price.EndDate <= price.StartDate {
		return nil, "", 0, fmt.Errorf("manual price %s has invalid window", price.ID)
	}
	if price.EndDate != "" && price.EndDate <= start {
		return []ManualSchedulePrice{price}, end, 0, nil
	}
	if futurePriceStartsAtOrAfter(price.StartDate, start, today, implicitStart) {
		if end == "" || price.StartDate < end {
			if !implicitStart {
				return nil, "", 0, fmt.Errorf("requested price window overlaps scheduled price %s", price.ID)
			}
			end = earlierPriceDate(end, price.StartDate)
		}
		return []ManualSchedulePrice{price}, end, 0, nil
	}
	if !implicitStart && start > today && price.StartDate > today {
		return nil, "", 0, fmt.Errorf("requested price window overlaps scheduled price %s", price.ID)
	}
	parts, end = splitCoveringPrice(price, start, end, implicitStart)
	return parts, end, 1, nil
}

func futurePriceStartsAtOrAfter(priceStart, start, today string, implicitStart bool) bool {
	return priceStart > start || (priceStart == start && start > today && !implicitStart)
}

func splitCoveringPrice(price ManualSchedulePrice, start, end string, implicitStart bool) (parts []ManualSchedulePrice, replacementEnd string) {
	parts = make([]ManualSchedulePrice, 0, 2)
	if price.StartDate < start {
		prefix := price
		prefix.EndDate = start
		parts = append(parts, prefix)
	}
	if !implicitStart && end != "" && (price.EndDate == "" || end < price.EndDate) {
		suffix := price
		suffix.StartDate = end
		parts = append(parts, suffix)
	}
	if implicitStart && price.EndDate != "" {
		end = earlierPriceDate(end, price.EndDate)
	}
	return parts, end
}

func earlierPriceDate(a, b string) string {
	if a == "" || b < a {
		return b
	}
	return a
}

// ReadActiveBasePrice reads all manual-price pages instead of relying on the
// capped sideload in the schedule response. at is injectable for date tests.
func ReadActiveBasePrice(ctx context.Context, c *Client, appID string, at time.Time) (ActiveBasePrice, error) {
	schedule, err := ReadManualPriceSchedule(ctx, c, appID)
	if err != nil {
		return ActiveBasePrice{}, err
	}
	return schedule.ActiveBasePrice(at)
}

func readBasePriceSchedule(ctx context.Context, c *Client, appID string) (ActiveBasePrice, error) {
	q := url.Values{"fields[appPriceSchedules]": {"baseTerritory,manualPrices"}}
	schedule, err := Get[struct {
		Data *Resource[AppPriceScheduleAttributes] `json:"data"`
	}](ctx, c, "/v1/apps/"+appID+"/appPriceSchedule", q)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound {
			return ActiveBasePrice{}, nil
		}
		return ActiveBasePrice{}, fmt.Errorf("read app price schedule: %w", err)
	}
	if schedule.Data == nil {
		return ActiveBasePrice{}, nil
	}
	if schedule.Data.ID == "" {
		return ActiveBasePrice{}, errors.New("app price schedule response missing resource id")
	}
	out := ActiveBasePrice{ScheduleID: schedule.Data.ID}
	rel, ok := schedule.Data.Relationships["baseTerritory"]
	if !ok {
		return ActiveBasePrice{}, fmt.Errorf("app price schedule %s: missing baseTerritory relationship", out.ScheduleID)
	}
	out.TerritoryID, err = priceRelationshipID(rel)
	if err != nil {
		return ActiveBasePrice{}, fmt.Errorf("app price schedule %s baseTerritory: %w", out.ScheduleID, err)
	}
	if out.TerritoryID == "" {
		return out, nil
	}
	return out, nil
}

func activeBasePrice(schedule ManualPriceSchedule, day string) (ActiveBasePrice, error) {
	out := ActiveBasePrice{ScheduleID: schedule.ID, TerritoryID: schedule.BaseTerritoryID}
	for _, price := range schedule.Prices {
		if price.TerritoryID != out.TerritoryID || !priceCovers(day, price) {
			continue
		}
		if out.PricePointID != "" {
			return ActiveBasePrice{}, fmt.Errorf("schedule %s: ambiguous active base prices", schedule.ID)
		}
		out.PricePointID = price.PricePointID
	}
	return out, nil
}

func priceCovers(day string, price ManualSchedulePrice) bool {
	return (price.StartDate == "" || day >= price.StartDate) && (price.EndDate == "" || day < price.EndDate)
}

func priceRelationshipID(rel Relationship) (string, error) {
	if len(rel.Data) == 0 {
		return "", errors.New("relationship missing data")
	}
	if string(rel.Data) == "null" {
		return "", nil
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rel.Data, &data); err != nil {
		return "", err
	}
	if data.ID == "" {
		return "", errors.New("relationship resource missing id")
	}
	return data.ID, nil
}

func validatePriceDate(value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return err
	}
	return nil
}
