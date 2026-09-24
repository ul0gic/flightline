package asc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type IAPPricePoint struct {
	ID            string `json:"id"`
	TerritoryID   string `json:"territoryId"`
	CustomerPrice string `json:"customerPrice"`
	Proceeds      string `json:"proceeds"`
}

type IAPManualPrice struct {
	ID           string `json:"id"`
	TerritoryID  string `json:"territoryId"`
	PricePointID string `json:"pricePointId"`
	StartDate    string `json:"startDate,omitempty"`
	EndDate      string `json:"endDate,omitempty"`
}

type IAPPriceSchedule struct {
	ID              string           `json:"id"`
	BaseTerritoryID string           `json:"baseTerritoryId"`
	ManualPrices    []IAPManualPrice `json:"manualPrices"`
}

type IAPAvailability struct {
	ID                        string   `json:"id"`
	AvailableInNewTerritories *bool    `json:"availableInNewTerritories,omitempty"`
	TerritoryIDs              []string `json:"territoryIds"`
}

type IAPBasePrice struct {
	ScheduleID, BaseTerritoryID, PricePointID string
}

type IAPCommerceWriteResult struct {
	ID      string
	Changed bool
}

func (schedule IAPPriceSchedule) ActiveBasePrice(at time.Time) (IAPBasePrice, error) {
	today := at.UTC().Format("2006-01-02")
	out := IAPBasePrice{ScheduleID: schedule.ID, BaseTerritoryID: schedule.BaseTerritoryID}
	for _, price := range schedule.ManualPrices {
		if price.TerritoryID != out.BaseTerritoryID || (price.StartDate != "" && price.StartDate > today) || (price.EndDate != "" && price.EndDate <= today) {
			continue
		}
		if out.PricePointID != "" {
			return IAPBasePrice{}, fmt.Errorf("IAP price schedule %s has overlapping active base prices", schedule.ID)
		}
		out.PricePointID = price.PricePointID
	}
	return out, nil
}

type iapPricePointAttributes struct {
	CustomerPrice string `json:"customerPrice"`
	Proceeds      string `json:"proceeds"`
}

type iapManualPriceAttributes struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Manual    *bool  `json:"manual"`
}

type iapAvailabilityAttributes struct {
	AvailableInNewTerritories *bool `json:"availableInNewTerritories"`
}

func ListIAPPricePoints(ctx context.Context, c *Client, iapID, territoryID string) ([]IAPPricePoint, error) {
	if iapID == "" {
		return nil, errors.New("IAP ID is required")
	}
	q := url.Values{"limit": {"200"}, "fields[inAppPurchasePricePoints]": {"customerPrice,proceeds,territory"}}
	if territoryID != "" {
		q.Set("filter[territory]", territoryID)
	}
	points := make([]IAPPricePoint, 0)
	for page, err := range Pages[iapPricePointAttributes](ctx, c, "/v2/inAppPurchases/"+url.PathEscape(iapID)+"/pricePoints", q) {
		if err != nil {
			return nil, fmt.Errorf("list IAP price points: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "inAppPurchasePricePoints" || row.ID == "" {
				return nil, errors.New("IAP price point missing type or ID")
			}
			territory, err := iapToOneID(row.Relationships["territory"], "territories")
			if err != nil {
				return nil, fmt.Errorf("IAP price point %s territory: %w", row.ID, err)
			}
			points = append(points, IAPPricePoint{ID: row.ID, TerritoryID: territory, CustomerPrice: row.Attributes.CustomerPrice, Proceeds: row.Attributes.Proceeds})
		}
	}
	return points, nil
}

func ReadIAPPriceSchedule(ctx context.Context, c *Client, iapID string) (IAPPriceSchedule, error) {
	if iapID == "" {
		return IAPPriceSchedule{}, errors.New("IAP ID is required")
	}
	path := "/v2/inAppPurchases/" + url.PathEscape(iapID) + "/iapPriceSchedule"
	row, err := readIAPOptionalSingleton[EmptyAttributes](ctx, c, path)
	if err != nil {
		return IAPPriceSchedule{}, fmt.Errorf("read IAP price schedule: %w", err)
	}
	if row == nil {
		return IAPPriceSchedule{}, nil
	}
	if row.Type != "inAppPurchasePriceSchedules" || row.ID == "" {
		return IAPPriceSchedule{}, errors.New("IAP price schedule missing type or ID")
	}
	base, err := iapToOneID(row.Relationships["baseTerritory"], "territories")
	if err != nil {
		return IAPPriceSchedule{}, fmt.Errorf("IAP price schedule base territory: %w", err)
	}
	schedule := IAPPriceSchedule{ID: row.ID, BaseTerritoryID: base, ManualPrices: make([]IAPManualPrice, 0)}
	seenPrices := make(map[string]bool)
	q := url.Values{"limit": {"200"}, "fields[inAppPurchasePrices]": {"startDate,endDate,manual,territory,inAppPurchasePricePoint"}}
	for page, err := range Pages[iapManualPriceAttributes](ctx, c, "/v1/inAppPurchasePriceSchedules/"+url.PathEscape(row.ID)+"/manualPrices", q) {
		if err != nil {
			return IAPPriceSchedule{}, fmt.Errorf("list IAP manual prices: %w", err)
		}
		for _, price := range page.Data {
			manual, err := projectIAPManualPrice(price)
			if err != nil {
				return IAPPriceSchedule{}, err
			}
			if seenPrices[manual.ID] {
				return IAPPriceSchedule{}, fmt.Errorf("duplicate IAP manual price %s", manual.ID)
			}
			seenPrices[manual.ID] = true
			schedule.ManualPrices = append(schedule.ManualPrices, manual)
		}
	}
	return schedule, nil
}

func projectIAPManualPrice(row Resource[iapManualPriceAttributes]) (IAPManualPrice, error) {
	if row.Type != "inAppPurchasePrices" || row.ID == "" || (row.Attributes.Manual != nil && !*row.Attributes.Manual) {
		return IAPManualPrice{}, fmt.Errorf("IAP manual price %q has invalid type, ID, or manual flag", row.ID)
	}
	territory, err := iapToOneID(row.Relationships["territory"], "territories")
	if err != nil {
		return IAPManualPrice{}, fmt.Errorf("IAP manual price %s territory: %w", row.ID, err)
	}
	point, err := iapToOneID(row.Relationships["inAppPurchasePricePoint"], "inAppPurchasePricePoints")
	if err != nil {
		return IAPManualPrice{}, fmt.Errorf("IAP manual price %s price point: %w", row.ID, err)
	}
	for _, date := range []string{row.Attributes.StartDate, row.Attributes.EndDate} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return IAPManualPrice{}, fmt.Errorf("IAP manual price %s invalid date %q: %w", row.ID, date, err)
			}
		}
	}
	if row.Attributes.StartDate != "" && row.Attributes.EndDate != "" && row.Attributes.EndDate <= row.Attributes.StartDate {
		return IAPManualPrice{}, fmt.Errorf("IAP manual price %s has invalid date window", row.ID)
	}
	return IAPManualPrice{ID: row.ID, TerritoryID: territory, PricePointID: point, StartDate: row.Attributes.StartDate, EndDate: row.Attributes.EndDate}, nil
}

func ReadIAPAvailability(ctx context.Context, c *Client, iapID string) (IAPAvailability, error) {
	if iapID == "" {
		return IAPAvailability{}, errors.New("IAP ID is required")
	}
	path := "/v2/inAppPurchases/" + url.PathEscape(iapID) + "/inAppPurchaseAvailability"
	row, err := readIAPOptionalSingleton[iapAvailabilityAttributes](ctx, c, path)
	if err != nil {
		return IAPAvailability{}, fmt.Errorf("read IAP availability: %w", err)
	}
	if row == nil {
		return IAPAvailability{}, nil
	}
	if row.Type != "inAppPurchaseAvailabilities" || row.ID == "" || row.Attributes.AvailableInNewTerritories == nil {
		return IAPAvailability{}, errors.New("IAP availability missing type, ID, or availableInNewTerritories")
	}
	out := IAPAvailability{ID: row.ID, AvailableInNewTerritories: row.Attributes.AvailableInNewTerritories, TerritoryIDs: make([]string, 0)}
	seenTerritories := make(map[string]bool)
	q := url.Values{"limit": {"200"}}
	for page, err := range Pages[EmptyAttributes](ctx, c, "/v1/inAppPurchaseAvailabilities/"+url.PathEscape(row.ID)+"/availableTerritories", q) {
		if err != nil {
			return IAPAvailability{}, fmt.Errorf("list IAP available territories: %w", err)
		}
		for _, territory := range page.Data {
			if territory.Type != "territories" || territory.ID == "" {
				return IAPAvailability{}, errors.New("IAP availability territory missing type or ID")
			}
			if seenTerritories[territory.ID] {
				return IAPAvailability{}, fmt.Errorf("duplicate IAP availability territory %s", territory.ID)
			}
			seenTerritories[territory.ID] = true
			out.TerritoryIDs = append(out.TerritoryIDs, territory.ID)
		}
	}
	return out, nil
}

func iapToOneID(rel Relationship, typ string) (string, error) {
	if len(rel.Data) == 0 || bytes.Equal(bytes.TrimSpace(rel.Data), []byte("null")) {
		return "", errors.New("missing relationship data")
	}
	var ref struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(rel.Data, &ref); err != nil {
		return "", err
	}
	if ref.Type != typ || ref.ID == "" {
		return "", fmt.Errorf("expected %s relationship with ID", typ)
	}
	return ref.ID, nil
}

func iapMissingSingleton(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound
}

func readIAPOptionalSingleton[A any](ctx context.Context, c *Client, path string) (*Resource[A], error) {
	resp, err := Get[struct {
		Data json.RawMessage `json:"data"`
	}](ctx, c, path, nil)
	if err != nil {
		if iapMissingSingleton(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("missing data in IAP singleton response")
	}
	if bytes.Equal(bytes.TrimSpace(resp.Data), []byte("null")) {
		return nil, nil
	}
	var row Resource[A]
	if err := json.Unmarshal(resp.Data, &row); err != nil {
		return nil, fmt.Errorf("decode IAP singleton resource: %w", err)
	}
	return &row, nil
}

// CreatePreservingIAPPriceSchedule replaces the current base price and retains
// other manual windows, including future prices and other territories.
func CreatePreservingIAPPriceSchedule(ctx context.Context, c *Client, iapID, territoryID, pointID string, expected *IAPBasePrice, at time.Time) (IAPCommerceWriteResult, error) {
	if iapID == "" || territoryID == "" || pointID == "" {
		return IAPCommerceWriteResult{}, errors.New("IAP ID, base territory, and price point are required")
	}
	schedule, err := ReadIAPPriceSchedule(ctx, c, iapID)
	if err != nil {
		return IAPCommerceWriteResult{}, err
	}
	current, err := schedule.ActiveBasePrice(at)
	if err != nil {
		return IAPCommerceWriteResult{}, err
	}
	if err := compareIAPBasePrice(current, expected); err != nil {
		return IAPCommerceWriteResult{}, err
	}
	if err := verifyIAPPricePoint(ctx, c, iapID, territoryID, pointID); err != nil {
		return IAPCommerceWriteResult{}, err
	}
	if current.BaseTerritoryID == territoryID && current.PricePointID == pointID {
		return IAPCommerceWriteResult{ID: schedule.ID}, nil
	}
	body, err := buildIAPPreservingPriceBody(iapID, territoryID, pointID, schedule, at.UTC().Format("2006-01-02"))
	if err != nil {
		return IAPCommerceWriteResult{}, err
	}
	return postIAPPriceSchedule(ctx, c, body)
}

func verifyIAPPricePoint(ctx context.Context, c *Client, iapID, territoryID, pointID string) error {
	points, err := ListIAPPricePoints(ctx, c, iapID, territoryID)
	if err != nil {
		return err
	}
	for _, point := range points {
		if point.ID == pointID && point.TerritoryID == territoryID {
			return nil
		}
	}
	return fmt.Errorf("IAP price point %s is not available for IAP %s in territory %s", pointID, iapID, territoryID)
}

func postIAPPriceSchedule(ctx context.Context, c *Client, body map[string]any) (IAPCommerceWriteResult, error) {
	resp, err := Post[Single[EmptyAttributes]](ctx, c, "/v1/inAppPurchasePriceSchedules", nil, body)
	if err != nil {
		return IAPCommerceWriteResult{}, fmt.Errorf("create IAP price schedule: %w", err)
	}
	if resp.Data.Type != "inAppPurchasePriceSchedules" || resp.Data.ID == "" {
		return IAPCommerceWriteResult{}, errors.New("created IAP price schedule response missing type or ID")
	}
	return IAPCommerceWriteResult{ID: resp.Data.ID, Changed: true}, nil
}

func compareIAPBasePrice(current IAPBasePrice, expected *IAPBasePrice) error {
	if expected == nil {
		if current.PricePointID != "" {
			return errors.New("IAP current price appeared since planning; replan")
		}
		return nil
	}
	if current.BaseTerritoryID != expected.BaseTerritoryID || current.PricePointID != expected.PricePointID {
		return fmt.Errorf("IAP current price changed since planning: expected %s/%s, found %s/%s", expected.BaseTerritoryID, expected.PricePointID, current.BaseTerritoryID, current.PricePointID)
	}
	return nil
}

func buildIAPPreservingPriceBody(iapID, territoryID, pointID string, schedule IAPPriceSchedule, today string) (map[string]any, error) {
	prices := make([]ManualSchedulePrice, 0, len(schedule.ManualPrices))
	for _, price := range schedule.ManualPrices {
		prices = append(prices, ManualSchedulePrice(price))
	}
	kept, replacement, err := spliceManualPrices(prices, territoryID, pointID, today, "", today, true)
	if err != nil {
		return nil, err
	}
	kept = append(kept, replacement)
	links := make([]map[string]any, 0, len(kept))
	included := make([]map[string]any, 0, len(kept))
	for index, price := range kept {
		localID := fmt.Sprintf("${newprice-%d}", index)
		links = append(links, map[string]any{"type": "inAppPurchasePrices", "id": localID})
		attributes := map[string]any{}
		if price.StartDate != "" {
			attributes["startDate"] = price.StartDate
		}
		if price.EndDate != "" {
			attributes["endDate"] = price.EndDate
		}
		included = append(included, map[string]any{
			"type": "inAppPurchasePrices", "id": localID, "attributes": attributes,
			"relationships": map[string]any{
				"inAppPurchaseV2":         map[string]any{"data": map[string]any{"type": "inAppPurchases", "id": iapID}},
				"inAppPurchasePricePoint": map[string]any{"data": map[string]any{"type": "inAppPurchasePricePoints", "id": price.PricePointID}},
			},
		})
	}
	return map[string]any{
		"data": map[string]any{"type": "inAppPurchasePriceSchedules", "relationships": map[string]any{
			"inAppPurchase": map[string]any{"data": map[string]any{"type": "inAppPurchases", "id": iapID}},
			"baseTerritory": map[string]any{"data": map[string]any{"type": "territories", "id": territoryID}},
			"manualPrices":  map[string]any{"data": links},
		}},
		"included": included,
	}, nil
}

// CreateIAPAvailability replaces the complete availability resource after
// verifying the observed values; nil expected means no resource was observed.
func CreateIAPAvailability(ctx context.Context, c *Client, iapID string, availableInNewTerritories bool, territoryIDs []string, expected *IAPAvailability) (IAPCommerceWriteResult, error) {
	if iapID == "" {
		return IAPCommerceWriteResult{}, errors.New("IAP ID is required")
	}
	current, err := ReadIAPAvailability(ctx, c, iapID)
	if err != nil {
		return IAPCommerceWriteResult{}, err
	}
	if err := compareIAPAvailability(current, expected); err != nil {
		return IAPCommerceWriteResult{}, err
	}
	if current.ID != "" && *current.AvailableInNewTerritories == availableInNewTerritories && equalIAPTerritories(current.TerritoryIDs, territoryIDs) {
		return IAPCommerceWriteResult{ID: current.ID}, nil
	}
	links := make([]map[string]any, 0, len(territoryIDs))
	for _, id := range territoryIDs {
		if id == "" {
			return IAPCommerceWriteResult{}, errors.New("available territory ID is empty")
		}
		links = append(links, map[string]any{"type": "territories", "id": id})
	}
	body := map[string]any{"data": map[string]any{
		"type": "inAppPurchaseAvailabilities", "attributes": map[string]any{"availableInNewTerritories": availableInNewTerritories},
		"relationships": map[string]any{
			"inAppPurchase":        map[string]any{"data": map[string]any{"type": "inAppPurchases", "id": iapID}},
			"availableTerritories": map[string]any{"data": links},
		},
	}}
	resp, err := Post[Single[iapAvailabilityAttributes]](ctx, c, "/v1/inAppPurchaseAvailabilities", nil, body)
	if err != nil {
		return IAPCommerceWriteResult{}, fmt.Errorf("create IAP availability: %w", err)
	}
	if resp.Data.Type != "inAppPurchaseAvailabilities" || resp.Data.ID == "" || resp.Data.Attributes.AvailableInNewTerritories == nil || *resp.Data.Attributes.AvailableInNewTerritories != availableInNewTerritories {
		return IAPCommerceWriteResult{}, errors.New("created IAP availability response did not confirm expected type, ID, or new-territory setting")
	}
	return IAPCommerceWriteResult{ID: resp.Data.ID, Changed: true}, nil
}

func compareIAPAvailability(current IAPAvailability, expected *IAPAvailability) error {
	if expected == nil {
		if current.ID != "" {
			return errors.New("IAP availability appeared since planning; replan")
		}
		return nil
	}
	if current.ID == "" || current.AvailableInNewTerritories == nil || expected.AvailableInNewTerritories == nil ||
		*current.AvailableInNewTerritories != *expected.AvailableInNewTerritories || !equalIAPTerritories(current.TerritoryIDs, expected.TerritoryIDs) {
		return errors.New("IAP availability changed since planning; replan")
	}
	return nil
}

func equalIAPTerritories(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}
