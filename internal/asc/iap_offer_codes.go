package asc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

type IAPOfferCode struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	CustomerEligibilities []string `json:"customerEligibilities"`
	ProductionCodeCount   int      `json:"productionCodeCount"`
	SandboxCodeCount      int      `json:"sandboxCodeCount"`
	Active                *bool    `json:"active,omitempty"`
}

type IAPOfferPrice struct {
	ID           string `json:"id"`
	TerritoryID  string `json:"territoryId"`
	PricePointID string `json:"pricePointId"`
}

type IAPOfferCustomBatch struct {
	ID             string `json:"id"`
	CustomCode     string `json:"-"`
	ExpirationDate string `json:"expirationDate,omitempty"`
	NumberOfCodes  int    `json:"numberOfCodes"`
	Active         *bool  `json:"active,omitempty"`
}

type IAPOfferOneTimeBatch struct {
	ID             string `json:"id"`
	ExpirationDate string `json:"expirationDate"`
	Environment    string `json:"environment"`
	NumberOfCodes  int    `json:"numberOfCodes"`
	Active         *bool  `json:"active,omitempty"`
}

type IAPOfferCodeDetail struct {
	IAPOfferCode
	Prices         []IAPOfferPrice        `json:"prices"`
	CustomBatches  []IAPOfferCustomBatch  `json:"customBatches"`
	OneTimeBatches []IAPOfferOneTimeBatch `json:"oneTimeBatches"`
}

type IAPOfferPriceChoice struct {
	TerritoryID  string
	PricePointID string
}

type iapOfferCodeAttributes struct {
	Name                  string   `json:"name"`
	CustomerEligibilities []string `json:"customerEligibilities"`
	ProductionCodeCount   int      `json:"productionCodeCount"`
	SandboxCodeCount      int      `json:"sandboxCodeCount"`
	Active                *bool    `json:"active"`
}

type iapOfferCustomAttributes struct {
	CustomCode     string `json:"customCode"`
	NumberOfCodes  int    `json:"numberOfCodes"`
	ExpirationDate string `json:"expirationDate"`
	Active         *bool  `json:"active"`
}

type iapOfferOneTimeAttributes struct {
	NumberOfCodes  int    `json:"numberOfCodes"`
	ExpirationDate string `json:"expirationDate"`
	Environment    string `json:"environment"`
	Active         *bool  `json:"active"`
}

func ListIAPOfferCodes(ctx context.Context, c *Client, iapID string) ([]IAPOfferCode, error) {
	if iapID == "" {
		return nil, errors.New("IAP ID is required")
	}
	q := url.Values{"limit": {"200"}}
	codes := make([]IAPOfferCode, 0)
	for page, err := range Pages[iapOfferCodeAttributes](ctx, c, "/v2/inAppPurchases/"+url.PathEscape(iapID)+"/offerCodes", q) {
		if err != nil {
			return nil, fmt.Errorf("list IAP offer codes: %w", err)
		}
		for _, row := range page.Data {
			code, err := projectIAPOfferCode(row)
			if err != nil {
				return nil, err
			}
			codes = append(codes, code)
		}
	}
	return codes, nil
}

func GetIAPOfferCode(ctx context.Context, c *Client, iapID, offerID string) (IAPOfferCodeDetail, error) {
	if err := requireIAPOfferOwnership(ctx, c, iapID, offerID); err != nil {
		return IAPOfferCodeDetail{}, err
	}
	resp, err := Get[Single[iapOfferCodeAttributes]](ctx, c, "/v1/inAppPurchaseOfferCodes/"+url.PathEscape(offerID), nil)
	if err != nil {
		return IAPOfferCodeDetail{}, fmt.Errorf("read IAP offer code: %w", err)
	}
	code, err := projectIAPOfferCode(resp.Data)
	if err != nil || code.ID != offerID {
		return IAPOfferCodeDetail{}, errors.New("IAP offer code response has invalid identity")
	}
	detail := IAPOfferCodeDetail{IAPOfferCode: code}
	if detail.Prices, err = listIAPOfferPrices(ctx, c, offerID); err != nil {
		return IAPOfferCodeDetail{}, err
	}
	if detail.CustomBatches, err = listIAPOfferCustomBatches(ctx, c, offerID); err != nil {
		return IAPOfferCodeDetail{}, err
	}
	if detail.OneTimeBatches, err = listIAPOfferOneTimeBatches(ctx, c, offerID); err != nil {
		return IAPOfferCodeDetail{}, err
	}
	return detail, nil
}

func projectIAPOfferCode(row Resource[iapOfferCodeAttributes]) (IAPOfferCode, error) {
	if row.Type != "inAppPurchaseOfferCodes" || row.ID == "" || row.Attributes.Name == "" {
		return IAPOfferCode{}, errors.New("IAP offer code missing type, ID, or name")
	}
	return IAPOfferCode{ID: row.ID, Name: row.Attributes.Name, CustomerEligibilities: row.Attributes.CustomerEligibilities,
		ProductionCodeCount: row.Attributes.ProductionCodeCount, SandboxCodeCount: row.Attributes.SandboxCodeCount, Active: row.Attributes.Active}, nil
}

func requireIAPOfferOwnership(ctx context.Context, c *Client, iapID, offerID string) error {
	if offerID == "" {
		return errors.New("offer ID is required")
	}
	codes, err := ListIAPOfferCodes(ctx, c, iapID)
	if err != nil {
		return err
	}
	for _, code := range codes {
		if code.ID == offerID {
			return nil
		}
	}
	return fmt.Errorf("offer %s does not belong to IAP %s", offerID, iapID)
}

func listIAPOfferPrices(ctx context.Context, c *Client, offerID string) ([]IAPOfferPrice, error) {
	prices := make([]IAPOfferPrice, 0)
	for page, err := range Pages[EmptyAttributes](ctx, c, "/v1/inAppPurchaseOfferCodes/"+url.PathEscape(offerID)+"/prices", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list IAP offer prices: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "inAppPurchaseOfferPrices" || row.ID == "" {
				return nil, errors.New("IAP offer price missing type or ID")
			}
			territory, err := iapToOneID(row.Relationships["territory"], "territories")
			if err != nil {
				return nil, fmt.Errorf("IAP offer price %s territory: %w", row.ID, err)
			}
			point, err := iapToOneID(row.Relationships["pricePoint"], "inAppPurchasePricePoints")
			if err != nil {
				return nil, fmt.Errorf("IAP offer price %s price point: %w", row.ID, err)
			}
			prices = append(prices, IAPOfferPrice{ID: row.ID, TerritoryID: territory, PricePointID: point})
		}
	}
	return prices, nil
}

func listIAPOfferCustomBatches(ctx context.Context, c *Client, offerID string) ([]IAPOfferCustomBatch, error) {
	batches := make([]IAPOfferCustomBatch, 0)
	for page, err := range Pages[iapOfferCustomAttributes](ctx, c, "/v1/inAppPurchaseOfferCodes/"+url.PathEscape(offerID)+"/customCodes", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list IAP offer custom batches: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "inAppPurchaseOfferCodeCustomCodes" || row.ID == "" {
				return nil, errors.New("IAP custom batch missing type or ID")
			}
			batches = append(batches, IAPOfferCustomBatch{ID: row.ID, CustomCode: row.Attributes.CustomCode,
				NumberOfCodes: row.Attributes.NumberOfCodes, ExpirationDate: row.Attributes.ExpirationDate, Active: row.Attributes.Active})
		}
	}
	return batches, nil
}

func listIAPOfferOneTimeBatches(ctx context.Context, c *Client, offerID string) ([]IAPOfferOneTimeBatch, error) {
	batches := make([]IAPOfferOneTimeBatch, 0)
	for page, err := range Pages[iapOfferOneTimeAttributes](ctx, c, "/v1/inAppPurchaseOfferCodes/"+url.PathEscape(offerID)+"/oneTimeUseCodes", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list IAP offer one-time batches: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "inAppPurchaseOfferCodeOneTimeUseCodes" || row.ID == "" {
				return nil, errors.New("IAP one-time batch missing type or ID")
			}
			batches = append(batches, IAPOfferOneTimeBatch{ID: row.ID, NumberOfCodes: row.Attributes.NumberOfCodes,
				ExpirationDate: row.Attributes.ExpirationDate, Environment: row.Attributes.Environment, Active: row.Attributes.Active})
		}
	}
	return batches, nil
}

func validIAPOfferEligibilities(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]bool)
	for _, value := range values {
		if value != "NON_SPENDER" && value != "ACTIVE_SPENDER" && value != "CHURNED_SPENDER" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func futureIAPOfferDate(date string, at time.Time) bool {
	parsed, err := time.Parse("2006-01-02", date)
	return err == nil && parsed.Format("2006-01-02") > at.UTC().Format("2006-01-02")
}

func streamIAPOneTimeValues(ctx context.Context, c *Client, batchID string, destination io.Writer) (int64, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/inAppPurchaseOfferCodeOneTimeUseCodes/"+url.PathEscape(batchID)+"/values", nil, nil, "text/csv")
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("offer code values download failed with HTTP %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
		return 0, errors.New("offer code values response is not CSV")
	}
	return io.Copy(destination, resp.Body)
}

func CreateIAPOfferCode(ctx context.Context, c *Client, iapID, name string, eligibilities []string, choices []IAPOfferPriceChoice) (IAPOfferCode, error) {
	if iapID == "" || strings.TrimSpace(name) == "" || !validIAPOfferEligibilities(eligibilities) || len(choices) == 0 {
		return IAPOfferCode{}, errors.New("offer definition requires IAP, name, eligibility, and at least one price")
	}
	existing, err := ListIAPOfferCodes(ctx, c, iapID)
	if err != nil {
		return IAPOfferCode{}, err
	}
	for _, code := range existing {
		if code.Name == name {
			return IAPOfferCode{}, errors.New("offer definition name already exists for this IAP; inspect it before creating another")
		}
	}
	if err := verifyIAPOfferPriceChoices(ctx, c, iapID, choices); err != nil {
		return IAPOfferCode{}, err
	}
	body := iapOfferCodeCreateBody(iapID, name, eligibilities, choices)
	resp, err := Post[Single[iapOfferCodeAttributes]](ctx, c, "/v1/inAppPurchaseOfferCodes", nil, body)
	if err != nil {
		return IAPOfferCode{}, fmt.Errorf("create IAP offer definition: %w", err)
	}
	created, err := projectIAPOfferCode(resp.Data)
	if err != nil || created.Name != name || !sameIAPOfferEligibilities(created.CustomerEligibilities, eligibilities) {
		return IAPOfferCode{}, errors.New("created IAP offer definition response did not confirm requested name and eligibility; inspect live before retrying")
	}
	return created, nil
}

func sameIAPOfferEligibilities(a, b []string) bool {
	left, right := slices.Clone(a), slices.Clone(b)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func verifyIAPOfferPriceChoices(ctx context.Context, c *Client, iapID string, choices []IAPOfferPriceChoice) error {
	seen := make(map[string]bool)
	for _, choice := range choices {
		if choice.TerritoryID == "" || choice.PricePointID == "" || seen[choice.TerritoryID] {
			return errors.New("offer prices require one valid price point per territory")
		}
		seen[choice.TerritoryID] = true
		if err := verifyIAPPricePoint(ctx, c, iapID, choice.TerritoryID, choice.PricePointID); err != nil {
			return err
		}
	}
	return nil
}

func iapOfferCodeCreateBody(iapID, name string, eligibilities []string, choices []IAPOfferPriceChoice) map[string]any {
	links := make([]map[string]any, 0, len(choices))
	included := make([]map[string]any, 0, len(choices))
	for index, choice := range choices {
		localID := fmt.Sprintf("${newprice-%d}", index)
		links = append(links, map[string]any{"type": "inAppPurchaseOfferPrices", "id": localID})
		included = append(included, map[string]any{
			"type": "inAppPurchaseOfferPrices", "id": localID,
			"relationships": map[string]any{
				"territory":  map[string]any{"data": map[string]any{"type": "territories", "id": choice.TerritoryID}},
				"pricePoint": map[string]any{"data": map[string]any{"type": "inAppPurchasePricePoints", "id": choice.PricePointID}},
			},
		})
	}
	return map[string]any{
		"data": map[string]any{"type": "inAppPurchaseOfferCodes", "attributes": map[string]any{"name": name, "customerEligibilities": eligibilities},
			"relationships": map[string]any{
				"inAppPurchase": map[string]any{"data": map[string]any{"type": "inAppPurchases", "id": iapID}},
				"prices":        map[string]any{"data": links},
			},
		},
		"included": included,
	}
}

func CreateIAPCustomCodeBatch(ctx context.Context, c *Client, iapID, offerID, customCode string, count int, expirationDate string, at time.Time) (IAPOfferCustomBatch, error) {
	if strings.TrimSpace(customCode) == "" || count <= 0 || (expirationDate != "" && !futureIAPOfferDate(expirationDate, at)) {
		return IAPOfferCustomBatch{}, errors.New("custom batch requires a nonempty code, positive count, and future expiration date if supplied")
	}
	if err := requireIAPOfferOwnership(ctx, c, iapID, offerID); err != nil {
		return IAPOfferCustomBatch{}, err
	}
	if err := ensureIAPCustomCodeAbsent(ctx, c, offerID, customCode); err != nil {
		return IAPOfferCustomBatch{}, err
	}
	attributes := map[string]any{"customCode": customCode, "numberOfCodes": count}
	if expirationDate != "" {
		attributes["expirationDate"] = expirationDate
	}
	body := map[string]any{"data": map[string]any{"type": "inAppPurchaseOfferCodeCustomCodes", "attributes": attributes,
		"relationships": map[string]any{"offerCode": map[string]any{"data": map[string]any{"type": "inAppPurchaseOfferCodes", "id": offerID}}},
	}}
	resp, err := Post[Single[iapOfferCustomAttributes]](ctx, c, "/v1/inAppPurchaseOfferCodeCustomCodes", nil, body)
	if err != nil {
		return IAPOfferCustomBatch{}, fmt.Errorf("create IAP custom code batch: %s", redactIAPCode(err, customCode))
	}
	return confirmIAPCustomBatch(resp.Data, customCode, count, expirationDate)
}

func ensureIAPCustomCodeAbsent(ctx context.Context, c *Client, offerID, customCode string) error {
	batches, err := listIAPOfferCustomBatches(ctx, c, offerID)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if batch.CustomCode == customCode {
			return errors.New("custom code already exists for this offer")
		}
	}
	return nil
}

func confirmIAPCustomBatch(row Resource[iapOfferCustomAttributes], customCode string, count int, expirationDate string) (IAPOfferCustomBatch, error) {
	if row.Type != "inAppPurchaseOfferCodeCustomCodes" || row.ID == "" || row.Attributes.CustomCode != customCode || row.Attributes.NumberOfCodes != count || (expirationDate != "" && row.Attributes.ExpirationDate != expirationDate) {
		return IAPOfferCustomBatch{}, errors.New("custom code batch response did not confirm request; inspect live before retrying")
	}
	return IAPOfferCustomBatch{ID: row.ID, CustomCode: row.Attributes.CustomCode, NumberOfCodes: row.Attributes.NumberOfCodes, ExpirationDate: row.Attributes.ExpirationDate, Active: row.Attributes.Active}, nil
}

func redactIAPCode(err error, code string) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), code, "[redacted]")
}

func CreateIAPOneTimeCodeBatch(ctx context.Context, c *Client, iapID, offerID string, count int, expirationDate, environment string, at time.Time) (IAPOfferOneTimeBatch, error) {
	if count <= 0 || !futureIAPOfferDate(expirationDate, at) || (environment != "PRODUCTION" && environment != "SANDBOX") {
		return IAPOfferOneTimeBatch{}, errors.New("one-time batch requires positive count, future expiration date, and PRODUCTION or SANDBOX environment")
	}
	if err := requireIAPOfferOwnership(ctx, c, iapID, offerID); err != nil {
		return IAPOfferOneTimeBatch{}, err
	}
	body := map[string]any{"data": map[string]any{"type": "inAppPurchaseOfferCodeOneTimeUseCodes",
		"attributes":    map[string]any{"numberOfCodes": count, "expirationDate": expirationDate, "environment": environment},
		"relationships": map[string]any{"offerCode": map[string]any{"data": map[string]any{"type": "inAppPurchaseOfferCodes", "id": offerID}}},
	}}
	resp, err := Post[Single[iapOfferOneTimeAttributes]](ctx, c, "/v1/inAppPurchaseOfferCodeOneTimeUseCodes", nil, body)
	if err != nil {
		return IAPOfferOneTimeBatch{}, fmt.Errorf("create IAP one-time code batch outcome may be uncertain; inspect live batches before retrying: %w", err)
	}
	row := resp.Data
	if row.Type != "inAppPurchaseOfferCodeOneTimeUseCodes" || row.ID == "" || row.Attributes.NumberOfCodes != count || row.Attributes.Environment != environment || row.Attributes.ExpirationDate != expirationDate {
		return IAPOfferOneTimeBatch{}, errors.New("one-time code batch response did not confirm request; inspect live before retrying")
	}
	return IAPOfferOneTimeBatch{ID: row.ID, NumberOfCodes: count, ExpirationDate: row.Attributes.ExpirationDate, Environment: row.Attributes.Environment, Active: row.Attributes.Active}, nil
}

func DownloadIAPOneTimeCodes(ctx context.Context, c *Client, iapID, offerID, batchID string, destination io.Writer) (int64, error) {
	if destination == nil || batchID == "" {
		return 0, errors.New("destination and batch ID are required")
	}
	if err := requireIAPOfferOwnership(ctx, c, iapID, offerID); err != nil {
		return 0, err
	}
	batches, err := listIAPOfferOneTimeBatches(ctx, c, offerID)
	if err != nil {
		return 0, err
	}
	found := false
	for _, batch := range batches {
		if batch.ID == batchID {
			found = true
			break
		}
	}
	if !found {
		return 0, fmt.Errorf("one-time batch %s does not belong to offer %s", batchID, offerID)
	}
	return streamIAPOneTimeValues(ctx, c, batchID, destination)
}
