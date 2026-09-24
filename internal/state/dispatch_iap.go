package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// applyIAPField routes by sub-path: bare product → create, field → PATCH, localizations → onward.
func applyIAPField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	rest := strings.TrimPrefix(ch.Path, "/spec/iap/products/")
	if rest == "" {
		return fmt.Errorf("apply iap: malformed path %s", ch.Path)
	}
	parts := strings.SplitN(rest, "/", 2)
	productID := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}
	if subPath == "type" {
		return fmt.Errorf("apply iap.%s.type: inAppPurchaseType is immutable after creation", productID)
	}
	if subPath == "contentHosting" {
		return fmt.Errorf("apply iap.%s.contentHosting: contentHosting is read-only", productID)
	}
	if subPath != "" && !strings.HasPrefix(subPath, "localizations/") && subPath != "reviewScreenshot" && !writableIAPField(subPath) {
		return fmt.Errorf("apply iap.%s.%s: unsupported writable field", productID, subPath)
	}

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}

	if subPath == "" {
		return createIAPParent(ctx, c, appID, productID, ch.To)
	}

	iapID, err := resolveIAPByProductID(ctx, c, appID, productID)
	if err != nil {
		return err
	}

	// Localization path
	if strings.HasPrefix(subPath, "localizations/") {
		return applyIAPLocalization(ctx, c, iapID, productID, subPath, ch.To)
	}

	if subPath == "reviewScreenshot" {
		return applyIAPReviewScreenshot(ctx, c, actx, iapID, productID, ch.To)
	}

	return patchIAPField(ctx, c, iapID, productID, subPath, ch.To)
}

func createIAPParent(ctx context.Context, c *asc.Client, appID, productID string, value any) error {
	product, err := decodeIAPProduct(value)
	if err != nil {
		return fmt.Errorf("apply iap: create %s: %w", productID, err)
	}
	if err := validateIAPParentCreate(product); err != nil {
		return fmt.Errorf("apply iap: create %s: %w", productID, err)
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "inAppPurchases",
			"attributes": iapCreateAttributes(productID, product),
			"relationships": map[string]any{
				"app": map[string]any{
					"data": map[string]any{"type": "apps", "id": appID},
				},
			},
		},
	}
	if _, err := asc.Post[asc.Single[asc.IAPAttributes]](ctx, c, "/v2/inAppPurchases", nil, body); err != nil {
		return fmt.Errorf("apply iap.create %s: %w", productID, err)
	}
	return nil
}

func patchIAPField(ctx context.Context, c *asc.Client, iapID, productID, field string, value any) error {
	body := map[string]any{"data": map[string]any{
		"type": "inAppPurchases", "id": iapID, "attributes": map[string]any{iapSchemaToWire(field): value},
	}}
	if _, err := asc.Patch[asc.Single[asc.IAPAttributes]](ctx, c, "/v2/inAppPurchases/"+iapID, nil, body); err != nil {
		return fmt.Errorf("apply iap.%s.%s: %w", productID, field, err)
	}
	return nil
}

func decodeIAPProduct(value any) (config.IAPProduct, error) {
	buf, err := json.Marshal(value)
	if err != nil {
		return config.IAPProduct{}, err
	}
	var product config.IAPProduct
	if err := json.Unmarshal(buf, &product); err != nil {
		return config.IAPProduct{}, err
	}
	return product, nil
}

func validateIAPParentCreate(product config.IAPProduct) error {
	if product.Type == "" {
		return errors.New("missing type")
	}
	if product.Name == nil {
		return errors.New("missing name")
	}
	if product.ContentHosting != nil {
		return errors.New("contentHosting is read-only")
	}
	return nil
}

func iapCreateAttributes(productID string, product config.IAPProduct) map[string]any {
	attributes := map[string]any{
		"name":              *product.Name,
		"productId":         productID,
		"inAppPurchaseType": product.Type,
	}
	if product.FamilySharable != nil {
		attributes["familySharable"] = *product.FamilySharable
	}
	if product.ReviewNote != nil {
		attributes["reviewNote"] = *product.ReviewNote
	}
	return attributes
}

func applyIAPLocalization(ctx context.Context, c *asc.Client, iapID, productID, subPath string, value any) error {
	parts := strings.Split(strings.TrimPrefix(subPath, "localizations/"), "/")
	if len(parts) == 1 {
		localization, err := decodeIAPLocalization(value)
		if err != nil {
			return fmt.Errorf("apply iap.%s.localization %s: %w", productID, parts[0], err)
		}
		return reconcileIAPLocalization(ctx, c, iapID, productID, parts[0], localization)
	}
	if len(parts) != 2 || !writableIAPLocalizationField(parts[1]) {
		return fmt.Errorf("apply iap.%s: malformed localization path %s", productID, subPath)
	}
	if parts[1] == "description" {
		if err := validateIAPDescriptionWrite(value); err != nil {
			return fmt.Errorf("apply iap.%s.loc.%s.description: %w", productID, parts[0], err)
		}
	}
	locID, attrs, found, err := findIAPLocalization(ctx, c, iapID, parts[0])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("apply iap.%s.loc.%s: localization must be created as a complete object", productID, parts[0])
	}
	return patchIAPLocalizationField(ctx, c, locID, parts[1], value, attrs)
}

func decodeIAPLocalization(value any) (config.IAPLocalization, error) {
	buf, err := json.Marshal(value)
	if err != nil {
		return config.IAPLocalization{}, err
	}
	var localization config.IAPLocalization
	if err := json.Unmarshal(buf, &localization); err != nil {
		return config.IAPLocalization{}, err
	}
	return localization, nil
}

func reconcileIAPLocalization(ctx context.Context, c *asc.Client, iapID, productID, locale string, desired config.IAPLocalization) error {
	if desired.Name == nil {
		return fmt.Errorf("apply iap.%s.loc.%s: missing name", productID, locale)
	}
	if desired.Description != nil {
		if err := validateIAPDescriptionWrite(*desired.Description); err != nil {
			return fmt.Errorf("apply iap.%s.loc.%s.description: %w", productID, locale, err)
		}
	}
	id, attrs, found, err := findIAPLocalization(ctx, c, iapID, locale)
	if err != nil {
		return err
	}
	if !found {
		return createIAPLocalization(ctx, c, iapID, locale, desired)
	}
	if err := patchIAPLocalizationField(ctx, c, id, "name", *desired.Name, attrs); err != nil {
		return err
	}
	if desired.Description == nil {
		return nil
	}
	return patchIAPLocalizationField(ctx, c, id, "description", *desired.Description, attrs)
}

func findIAPLocalization(ctx context.Context, c *asc.Client, iapID, locale string) (id string, attributes asc.IAPLocalizationAttributes, found bool, err error) {
	for page, err := range asc.Pages[asc.IAPLocalizationAttributes](ctx, c, "/v2/inAppPurchases/"+iapID+"/inAppPurchaseLocalizations", url.Values{"limit": {"200"}}) {
		if err != nil {
			return "", asc.IAPLocalizationAttributes{}, false, fmt.Errorf("list iap localizations: %w", err)
		}
		for _, localization := range page.Data {
			if localization.Attributes.Locale == locale {
				return localization.ID, localization.Attributes, true, nil
			}
		}
	}
	return "", asc.IAPLocalizationAttributes{}, false, nil
}

func createIAPLocalization(ctx context.Context, c *asc.Client, iapID, locale string, desired config.IAPLocalization) error {
	attributes := map[string]any{"locale": locale, "name": *desired.Name}
	if desired.Description != nil {
		attributes["description"] = *desired.Description
	}
	body := map[string]any{"data": map[string]any{
		"type": "inAppPurchaseLocalizations", "attributes": attributes,
		"relationships": map[string]any{"inAppPurchaseV2": map[string]any{
			"data": map[string]any{"type": "inAppPurchases", "id": iapID},
		}},
	}}
	if _, err := asc.Post[asc.Single[asc.IAPLocalizationAttributes]](ctx, c, "/v1/inAppPurchaseLocalizations", nil, body); err != nil {
		return fmt.Errorf("create iap localization %s: %w", locale, err)
	}
	return nil
}

func patchIAPLocalizationField(ctx context.Context, c *asc.Client, id, field string, value any, current asc.IAPLocalizationAttributes) error {
	if value == nil || (field == "name" && value == current.Name) || (field == "description" && value == current.Description) {
		return nil
	}
	body := map[string]any{"data": map[string]any{
		"type": "inAppPurchaseLocalizations", "id": id, "attributes": map[string]any{field: value},
	}}
	if _, err := asc.Patch[asc.Single[asc.IAPLocalizationAttributes]](ctx, c, "/v1/inAppPurchaseLocalizations/"+id, nil, body); err != nil {
		return fmt.Errorf("patch iap localization %s.%s: %w", id, field, err)
	}
	return nil
}

func writableIAPField(field string) bool {
	return field == "name" || field == "familySharable" || field == "reviewNote"
}

func writableIAPLocalizationField(field string) bool {
	return field == "name" || field == "description"
}

func validateIAPDescriptionWrite(value any) error {
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("expected string, got %T", value)
	}
	if utf8.RuneCountInString(text) > 45 {
		return errors.New("description exceeds Apple's 45-character write limit")
	}
	return nil
}

// iapSchemaToWire maps schema field names to Apple's wire keys; only "type" → "inAppPurchaseType" differs.
func iapSchemaToWire(field string) string {
	if field == "type" {
		return "inAppPurchaseType"
	}
	return field
}

func resolveIAPByProductID(ctx context.Context, c *asc.Client, appID, productID string) (string, error) {
	q := url.Values{
		"filter[productId]": {productID},
		"limit":             {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.IAPAttributes]](
		ctx, c, "/v1/apps/"+appID+"/inAppPurchasesV2", q,
	)
	if err != nil {
		return "", fmt.Errorf("resolve iap %s: %w", productID, err)
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("iap %s not found on app", productID)
	}
	return page.Data[0].ID, nil
}

// ensureIAPLocalization finds the locale's localization or creates it carrying the applied field.
// Apple's localization relationship endpoint rejects filter[locale], so matching is client-side.
