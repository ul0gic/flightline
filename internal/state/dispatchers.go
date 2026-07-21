package state

import (
	"context"
	"crypto/md5" //nolint:gosec // Apple's asset protocol requires MD5
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// applyBuildAttach looks up the build by version+number and PATCHes the version's build relationship.
func applyBuildAttach(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, platform)
	if err != nil {
		return err
	}

	target, ok := ch.To.(string)
	if !ok {
		return fmt.Errorf("apply build.number: expected string, got %T", ch.To)
	}
	q := url.Values{
		"filter[app]":                        {appID},
		"filter[preReleaseVersion.version]":  {actx.Version},
		"filter[preReleaseVersion.platform]": {platform},
		"filter[version]":                    {target},
		"limit":                              {"2"},
	}
	bp, err := asc.Get[asc.Collection[asc.BuildAttributes]](ctx, c, "/v1/builds", q)
	if err != nil {
		return fmt.Errorf("apply build.number: lookup build %s: %w", target, err)
	}
	if len(bp.Data) == 0 {
		return fmt.Errorf("apply build.number: no build %q for version %s (uploaded yet?)", target, actx.Version)
	}
	if len(bp.Data) > 1 {
		return fmt.Errorf("apply build.number: build %q is ambiguous for version %s on %s", target, actx.Version, platform)
	}
	buildID := bp.Data[0].ID

	body := map[string]any{
		"data": map[string]any{"type": "builds", "id": buildID},
	}
	if err := patchRelationship(ctx, c, "/v1/appStoreVersions/"+versionID+"/relationships/build", body); err != nil {
		return fmt.Errorf("apply build.number: patch relationship: %w", err)
	}
	return nil
}

// patchRelationship PATCHes a JSON:API relationship; json.RawMessage accepts Apple's empty 204 body.
func patchRelationship(ctx context.Context, c *asc.Client, path string, body any) error {
	if _, err := asc.Patch[json.RawMessage](ctx, c, path, nil, body); err != nil {
		return err
	}
	return nil
}

// applyEncryptionDeclaration POSTs an appEncryptionDeclaration for the full ECCN path.
func applyEncryptionDeclaration(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	wireKey := strings.TrimPrefix(ch.Path, "/spec/exportCompliance/declaration/")
	if wireKey == "" || strings.Contains(wireKey, "/") {
		return fmt.Errorf("apply exportCompliance.declaration: unexpected path %s", ch.Path)
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	body := map[string]any{
		"data": map[string]any{
			"type": "appEncryptionDeclarations",
			"attributes": map[string]any{
				wireKey: ch.To,
			},
			"relationships": map[string]any{
				"app": map[string]any{
					"data": map[string]any{"type": "apps", "id": appID},
				},
			},
		},
	}
	if _, err := asc.Post[asc.Single[asc.AppEncryptionDeclarationAttributes]](
		ctx, c, "/v1/appEncryptionDeclarations", nil, body,
	); err != nil {
		return fmt.Errorf("apply exportCompliance.declaration.%s: %w", wireKey, err)
	}
	return nil
}

// applyCategoriesField PATCHes one category relationship on the editable appInfo.
func applyCategoriesField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	appInfoID, err := fetchEditableAppInfo(ctx, c, appID)
	if err != nil {
		return err
	}
	if appInfoID == "" {
		return errors.New("apply categories: no editable appInfo")
	}

	leaf := strings.TrimPrefix(ch.Path, "/spec/categories/")
	switch leaf {
	case "primary":
		return patchAppInfoCategory(ctx, c, appInfoID, "primaryCategory", ch.To, "appCategories")
	case "secondary":
		return patchAppInfoCategory(ctx, c, appInfoID, "secondaryCategory", ch.To, "appCategories")
	case "primarySubcategories":
		return patchAppInfoSubcategories(ctx, c, appInfoID, "primarySubcategoryOne", "primarySubcategoryTwo", ch.To)
	case "secondarySubcategories":
		return patchAppInfoSubcategories(ctx, c, appInfoID, "secondarySubcategoryOne", "secondarySubcategoryTwo", ch.To)
	}
	return fmt.Errorf("apply categories: unknown leaf %q", leaf)
}

func patchAppInfoCategory(ctx context.Context, c *asc.Client, appInfoID, relName string, val any, relType string) error {
	var data any
	if val == nil {
		data = nil
	} else {
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("apply categories.%s: expected string, got %T", relName, val)
		}
		data = map[string]any{"type": relType, "id": s}
	}
	body := map[string]any{"data": data}
	return patchRelationship(ctx, c, "/v1/appInfos/"+appInfoID+"/relationships/"+relName, body)
}

func patchAppInfoSubcategories(ctx context.Context, c *asc.Client, appInfoID, relOne, relTwo string, val any) error {
	subs, err := asStringSlice(val)
	if err != nil {
		return fmt.Errorf("apply categories: %w", err)
	}
	first := "" // Apple stores subcategories as two scalar relationships; set both
	second := ""
	if len(subs) > 0 {
		first = subs[0]
	}
	if len(subs) > 1 {
		second = subs[1]
	}
	pairs := []struct {
		rel, id string
	}{{relOne, first}, {relTwo, second}}
	for _, p := range pairs {
		var data any
		if p.id != "" {
			data = map[string]any{"type": "appCategories", "id": p.id}
		}
		body := map[string]any{"data": data}
		if err := patchRelationship(ctx, c, "/v1/appInfos/"+appInfoID+"/relationships/"+p.rel, body); err != nil {
			return fmt.Errorf("apply categories.%s: %w", p.rel, err)
		}
	}
	return nil
}

// applyPricingField creates an appPriceSchedule. Apple requires territory+pricePoint together,
// so a single-leaf change re-fetches the missing field from live state.
func applyPricingField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	curTerr, curPP := fetchPricingPair(ctx, c, appID) // re-fetch so a single-leaf change doesn't blank the other
	leaf := strings.TrimPrefix(ch.Path, "/spec/pricing/")
	switch leaf {
	case "baseTerritory":
		s, _ := ch.To.(string)
		curTerr = s
	case "appPricePointId":
		s, _ := ch.To.(string)
		curPP = s
	default:
		return fmt.Errorf("apply pricing: unknown leaf %q", leaf)
	}
	if curTerr == "" || curPP == "" {
		return errors.New("apply pricing: both baseTerritory and appPricePointId are required for a price schedule")
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "appPriceSchedules",
			"relationships": map[string]any{
				"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}},
				"baseTerritory": map[string]any{
					"data": map[string]any{"type": "territories", "id": curTerr},
				},
				"manualPrices": map[string]any{
					"data": []any{
						map[string]any{"type": "appPrices", "id": "price0"},
					},
				},
			},
		},
		"included": []any{
			map[string]any{
				"type": "appPrices",
				"id":   "price0",
				"attributes": map[string]any{
					"startDate": nil,
				},
				"relationships": map[string]any{
					"appPricePoint": map[string]any{
						"data": map[string]any{"type": "appPricePoints", "id": curPP},
					},
					"territory": map[string]any{
						"data": map[string]any{"type": "territories", "id": curTerr},
					},
				},
			},
		},
	}
	if _, err := asc.Post[asc.Single[asc.AppPriceScheduleAttributes]](
		ctx, c, "/v1/appPriceSchedules", nil, body,
	); err != nil {
		return fmt.Errorf("apply pricing: create schedule: %w", err)
	}
	return nil
}

// applyReviewerDemoField PATCHes one field on appStoreReviewDetail;
// passwordRef/passwordFile are resolved to a runtime value before any HTTP work.
func applyReviewerDemoField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	leaf := strings.TrimPrefix(ch.Path, "/spec/reviewerDemo/")
	wire, val, err := reviewerDemoWireForLeaf(leaf, ch.To, actx.StateDir)
	if err != nil {
		return err
	}

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, platform)
	if err != nil {
		return err
	}
	detailID, err := fetchOrCreateReviewDetail(ctx, c, versionID)
	if err != nil {
		return err
	}

	body := map[string]any{
		"data": map[string]any{
			"type":       "appStoreReviewDetails",
			"id":         detailID,
			"attributes": map[string]any{wire: val},
		},
	}
	if _, err := asc.Patch[asc.Single[reviewerDemoAttrs]](
		ctx, c, "/v1/appStoreReviewDetails/"+detailID, nil, body,
	); err != nil {
		return fmt.Errorf("apply reviewerDemo.%s: %w", wire, err)
	}
	return nil
}

type reviewerDemoAttrs struct {
	ContactFirstName string `json:"contactFirstName,omitempty"`
	ContactLastName  string `json:"contactLastName,omitempty"`
	ContactEmail     string `json:"contactEmail,omitempty"`
	ContactPhone     string `json:"contactPhone,omitempty"`
	DemoAccountName  string `json:"demoAccountName,omitempty"`
	DemoAccountPwd   string `json:"demoAccountPassword,omitempty"`
	DemoAccountReq   *bool  `json:"demoAccountRequired,omitempty"`
	Notes            string `json:"notes,omitempty"`
}

func reviewerDemoWireForLeaf(leaf string, to any, stateDir string) (wireKey string, value any, err error) {
	switch leaf {
	case "username":
		return "demoAccountName", to, nil
	case "passwordRef", "passwordFile":
		pw, err := resolvePassword(leaf, to, stateDir)
		if err != nil {
			return "", nil, err
		}
		return "demoAccountPassword", pw, nil
	case "notes":
		return "notes", to, nil
	case "contactName":
		// Apple splits first/last; schema's contactName maps to contactFirstName only.
		return "contactFirstName", to, nil
	case "contactEmail":
		return "contactEmail", to, nil
	case "contactPhone":
		return "contactPhone", to, nil
	}
	return "", nil, fmt.Errorf("reviewerDemo: unknown leaf %q", leaf)
}

func resolvePassword(leaf string, to any, stateDir string) (string, error) {
	s, ok := to.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("reviewerDemo: %s expected string, got %T", leaf, to)
	}
	switch leaf {
	case "passwordRef":
		// Format: env:VAR_NAME
		if !strings.HasPrefix(s, "env:") {
			return "", errors.New(`reviewerDemo: passwordRef must start with "env:"`)
		}
		v := strings.TrimPrefix(s, "env:")
		val, ok := lookupEnvFn(v)
		if !ok || val == "" {
			return "", fmt.Errorf("reviewerDemo: passwordRef env %s is empty", v)
		}
		return val, nil
	case "passwordFile":
		path := s
		if !filepath.IsAbs(path) && stateDir != "" {
			path = filepath.Join(stateDir, path)
		}
		buf, err := readFileFn(path)
		if err != nil {
			return "", fmt.Errorf("reviewerDemo: read passwordFile %s: %w", path, err)
		}
		return strings.TrimRight(string(buf), "\r\n"), nil
	}
	return "", fmt.Errorf("reviewerDemo: unknown password leaf %q", leaf)
}

// fetchOrCreateReviewDetail returns the appStoreReviewDetail ID, creating one if not yet provisioned.
func fetchOrCreateReviewDetail(ctx context.Context, c *asc.Client, versionID string) (string, error) {
	resp, err := asc.Get[asc.Single[reviewerDemoAttrs]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreReviewDetail", nil,
	)
	if err == nil && resp.Data.ID != "" {
		return resp.Data.ID, nil
	}
	body := map[string]any{
		"data": map[string]any{
			"type": "appStoreReviewDetails",
			"relationships": map[string]any{
				"appStoreVersion": map[string]any{
					"data": map[string]any{"type": "appStoreVersions", "id": versionID},
				},
			},
		},
	}
	cresp, cerr := asc.Post[asc.Single[reviewerDemoAttrs]](
		ctx, c, "/v1/appStoreReviewDetails", nil, body,
	)
	if cerr != nil {
		return "", fmt.Errorf("create appStoreReviewDetail: %w", cerr)
	}
	return cresp.Data.ID, nil
}

// applyMetadataField routes one localization field to the version or appInfo localization that owns it.
func applyMetadataField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	parts := strings.Split(strings.TrimPrefix(ch.Path, "/spec/metadata/locales/"), "/")
	if len(parts) != 2 {
		return fmt.Errorf("apply metadata: malformed path %s", ch.Path)
	}
	locale, field := parts[0], parts[1]

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, platform)
	if err != nil {
		return err
	}

	if isVersionLocalizationField(field) {
		locID, err := getOrCreateVersionLocalization(ctx, c, versionID, locale)
		if err != nil {
			return fmt.Errorf("apply metadata: %w", err)
		}
		return patchSingleField(ctx, c, "appStoreVersionLocalizations", locID,
			"/v1/appStoreVersionLocalizations/"+locID, schemaToWireMetadata(field), ch.To)
	}
	if isAppInfoLocalizationField(field) {
		appInfoID, err := fetchEditableAppInfo(ctx, c, appID)
		if err != nil {
			return err
		}
		locID, err := getOrCreateAppInfoLocalization(ctx, c, appInfoID, locale)
		if err != nil {
			return fmt.Errorf("apply metadata: %w", err)
		}
		return patchSingleField(ctx, c, "appInfoLocalizations", locID,
			"/v1/appInfoLocalizations/"+locID, schemaToWireMetadata(field), ch.To)
	}
	return fmt.Errorf("apply metadata: unknown field %q", field)
}

func isVersionLocalizationField(f string) bool {
	switch f {
	case "description", "keywords", "whatsNew", "promotionalText", "marketingUrl", "supportUrl":
		return true
	}
	return false
}

func isAppInfoLocalizationField(f string) bool {
	switch f {
	case "name", "subtitle", "privacyPolicyUrl":
		return true
	}
	return false
}

// schemaToWireMetadata maps schema field names to Apple's wire keys; currently identity.
func schemaToWireMetadata(f string) string {
	return f
}

func patchSingleField(ctx context.Context, c *asc.Client, resType, resID, path, wireField string, val any) error {
	body := map[string]any{
		"data": map[string]any{
			"type":       resType,
			"id":         resID,
			"attributes": map[string]any{wireField: val},
		},
	}
	if _, err := asc.Patch[asc.Single[map[string]any]](ctx, c, path, nil, body); err != nil {
		return fmt.Errorf("patch %s.%s: %w", path, wireField, err)
	}
	return nil
}

func getOrCreateVersionLocalization(ctx context.Context, c *asc.Client, versionID, locale string) (string, error) {
	q := url.Values{"filter[locale]": {locale}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[map[string]any]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations", q,
	)
	if err != nil {
		return "", fmt.Errorf("list version localizations: %w", err)
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "appStoreVersionLocalizations",
			"attributes": map[string]any{"locale": locale},
			"relationships": map[string]any{
				"appStoreVersion": map[string]any{
					"data": map[string]any{"type": "appStoreVersions", "id": versionID},
				},
			},
		},
	}
	resp, err := asc.Post[asc.Single[map[string]any]](
		ctx, c, "/v1/appStoreVersionLocalizations", nil, body,
	)
	if err != nil {
		return "", fmt.Errorf("create version localization: %w", err)
	}
	return resp.Data.ID, nil
}

func getOrCreateAppInfoLocalization(ctx context.Context, c *asc.Client, appInfoID, locale string) (string, error) {
	q := url.Values{"filter[locale]": {locale}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[map[string]any]](
		ctx, c, "/v1/appInfos/"+appInfoID+"/appInfoLocalizations", q,
	)
	if err != nil {
		return "", fmt.Errorf("list appInfo localizations: %w", err)
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "appInfoLocalizations",
			"attributes": map[string]any{"locale": locale},
			"relationships": map[string]any{
				"appInfo": map[string]any{
					"data": map[string]any{"type": "appInfos", "id": appInfoID},
				},
			},
		},
	}
	resp, err := asc.Post[asc.Single[map[string]any]](
		ctx, c, "/v1/appInfoLocalizations", nil, body,
	)
	if err != nil {
		return "", fmt.Errorf("create appInfo localization: %w", err)
	}
	return resp.Data.ID, nil
}

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

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}

	if subPath == "" {
		buf, _ := json.Marshal(ch.To)
		var prod struct {
			Type           string `json:"type"`
			Name           string `json:"name,omitempty"`
			FamilySharable *bool  `json:"familySharable,omitempty"`
			ContentHosting string `json:"contentHosting,omitempty"`
			ReviewNote     string `json:"reviewNote,omitempty"`
		}
		_ = json.Unmarshal(buf, &prod)
		if prod.Type == "" {
			return fmt.Errorf("apply iap: create %s: missing type", productID)
		}
		body := map[string]any{
			"data": map[string]any{
				"type": "inAppPurchases",
				"attributes": map[string]any{
					"name":              prod.Name,
					"productId":         productID,
					"inAppPurchaseType": prod.Type,
					"familySharable":    prod.FamilySharable,
					"reviewNote":        prod.ReviewNote,
				},
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

	iapID, err := resolveIAPByProductID(ctx, c, appID, productID)
	if err != nil {
		return err
	}

	// Localization path
	if strings.HasPrefix(subPath, "localizations/") {
		locParts := strings.SplitN(strings.TrimPrefix(subPath, "localizations/"), "/", 2)
		if len(locParts) != 2 {
			return fmt.Errorf("apply iap: malformed localization path %s", ch.Path)
		}
		locale, field := locParts[0], locParts[1]
		locID, created, err := ensureIAPLocalization(ctx, c, iapID, locale, field, ch.To)
		if err != nil {
			return err
		}
		if created {
			return nil
		}
		body := map[string]any{
			"data": map[string]any{
				"type":       "inAppPurchaseLocalizations",
				"id":         locID,
				"attributes": map[string]any{field: ch.To},
			},
		}
		if _, err := asc.Patch[asc.Single[asc.IAPLocalizationAttributes]](
			ctx, c, "/v1/inAppPurchaseLocalizations/"+locID, nil, body,
		); err != nil {
			return fmt.Errorf("apply iap.%s.loc.%s.%s: %w", productID, locale, field, err)
		}
		return nil
	}

	if subPath == "reviewScreenshot" {
		return applyIAPReviewScreenshot(ctx, c, actx, iapID, productID, ch.To)
	}

	wire := iapSchemaToWire(subPath)
	body := map[string]any{
		"data": map[string]any{
			"type":       "inAppPurchases",
			"id":         iapID,
			"attributes": map[string]any{wire: ch.To},
		},
	}
	if _, err := asc.Patch[asc.Single[asc.IAPAttributes]](ctx, c, "/v2/inAppPurchases/"+iapID, nil, body); err != nil {
		return fmt.Errorf("apply iap.%s.%s: %w", productID, subPath, err)
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
func ensureIAPLocalization(ctx context.Context, c *asc.Client, iapID, locale, field string, value any) (locID string, created bool, err error) {
	q := url.Values{"limit": {"200"}}
	for page, err := range asc.Pages[asc.IAPLocalizationAttributes](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/inAppPurchaseLocalizations", q,
	) {
		if err != nil {
			return "", false, fmt.Errorf("list iap localizations: %w", err)
		}
		for _, loc := range page.Data {
			if loc.Attributes.Locale == locale {
				return loc.ID, false, nil
			}
		}
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "inAppPurchaseLocalizations",
			"attributes": map[string]any{"locale": locale, field: value},
			"relationships": map[string]any{
				"inAppPurchaseV2": map[string]any{
					"data": map[string]any{"type": "inAppPurchases", "id": iapID},
				},
			},
		},
	}
	resp, err := asc.Post[asc.Single[asc.IAPLocalizationAttributes]](
		ctx, c, "/v1/inAppPurchaseLocalizations", nil, body,
	)
	if err != nil {
		return "", false, fmt.Errorf("create iap localization %s: %w", locale, err)
	}
	return resp.Data.ID, true, nil
}

func applyScreenshotSet(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	rest := strings.TrimPrefix(ch.Path, "/spec/screenshots/locales/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("apply screenshots: malformed path %s", ch.Path)
	}
	locale, device := parts[0], parts[1]
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, actx.Platform)
	if err != nil {
		return err
	}
	localizationID, err := resolveVersionLocalizationID(ctx, c, versionID, locale)
	if err != nil {
		return err
	}
	setID, err := findOrCreateManagedScreenshotSet(ctx, c, localizationID, "appStoreVersionLocalization", "appStoreVersionLocalizations", device)
	if err != nil {
		return err
	}
	return reconcileScreenshotFiles(ctx, c, actx, setID, ch.To)
}

func applyIAPReviewScreenshot(ctx context.Context, c *asc.Client, actx ApplyContext, iapID, productID string, target any) error {
	screenshot, err := decodeIAPReviewScreenshot(target)
	if err != nil {
		return fmt.Errorf("apply iap.%s.reviewScreenshot: %w", productID, err)
	}
	path, err := resolveAssetPath(actx.StateDir, screenshot.Path)
	if err != nil {
		return fmt.Errorf("apply iap.%s.reviewScreenshot: %w", productID, err)
	}
	checksum, err := fileMD5(path)
	if err != nil {
		return fmt.Errorf("apply iap.%s.reviewScreenshot: %w", productID, err)
	}
	current, err := asc.Get[asc.Single[asc.IAPReviewScreenshotAttributes]](
		ctx, c, "/v2/inAppPurchases/"+url.PathEscape(iapID)+"/appStoreReviewScreenshot", nil,
	)
	if err == nil && current.Data.ID != "" && current.Data.Attributes.SourceFileChecksum == checksum {
		return nil
	}
	if err != nil {
		var apiErr *asc.APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusNotFound {
			return fmt.Errorf("apply iap.%s.reviewScreenshot: inspect current screenshot: %w", productID, err)
		}
	}
	_, err = c.Upload(ctx, asc.UploadOptions{
		Kind: asc.AssetKindIAPReviewScreenshot, ParentID: iapID,
		Asset: asc.UploadAsset{Path: path}, ResumeFromCheckpoint: actx.ResumeUploads,
	})
	if err != nil {
		return fmt.Errorf("apply iap.%s.reviewScreenshot: %w", productID, err)
	}
	return nil
}

func resolveVersionLocalizationID(ctx context.Context, c *asc.Client, versionID, locale string) (string, error) {
	page, err := asc.Get[asc.Collection[versionLocAttrs]](
		ctx, c, "/v1/appStoreVersions/"+url.PathEscape(versionID)+"/appStoreVersionLocalizations",
		url.Values{"limit": {"50"}},
	)
	if err != nil {
		return "", fmt.Errorf("apply screenshots: list version localizations: %w", err)
	}
	for _, localization := range page.Data {
		if localization.Attributes.Locale == locale {
			return localization.ID, nil
		}
	}
	return "", fmt.Errorf("apply screenshots: locale %q does not exist on version %s", locale, versionID)
}

func findOrCreateManagedScreenshotSet(ctx context.Context, c *asc.Client, parentID, relationship, parentType, device string) (string, error) {
	path := "/v1/" + parentType + "/" + url.PathEscape(parentID) + "/appScreenshotSets"
	page, err := asc.Get[asc.Collection[screenshotSetAttrs]](
		ctx, c, path, url.Values{"filter[screenshotDisplayType]": {device}, "limit": {"1"}},
	)
	if err != nil {
		return "", fmt.Errorf("apply screenshots: find set: %w", err)
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	body := map[string]any{"data": map[string]any{
		"type":       "appScreenshotSets",
		"attributes": map[string]any{"screenshotDisplayType": device},
		"relationships": map[string]any{relationship: map[string]any{
			"data": map[string]any{"type": parentType, "id": parentID},
		}},
	}}
	resp, err := asc.Post[asc.Single[screenshotSetAttrs]](ctx, c, "/v1/appScreenshotSets", nil, body)
	if err != nil {
		return "", fmt.Errorf("apply screenshots: create set: %w", err)
	}
	if resp.Data.ID == "" {
		return "", errors.New("apply screenshots: create set returned empty id")
	}
	return resp.Data.ID, nil
}

func reconcileScreenshotFiles(ctx context.Context, c *asc.Client, actx ApplyContext, setID string, target any) error {
	files, err := decodeScreenshotFiles(target)
	if err != nil {
		return err
	}
	existing, err := fetchExistingScreenshotFiles(ctx, c, setID)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := reconcileOneScreenshotFile(ctx, c, actx, setID, file, existing); err != nil {
			return err
		}
	}
	return deleteUnmanagedScreenshotFiles(ctx, c, existing)
}

type managedScreenshotFile struct {
	id       string
	checksum string
	consumed bool
}

func fetchExistingScreenshotFiles(ctx context.Context, c *asc.Client, setID string) ([]*managedScreenshotFile, error) {
	existing := make([]*managedScreenshotFile, 0)
	for page, pageErr := range asc.Pages[screenshotAttrs](ctx, c,
		"/v1/appScreenshotSets/"+url.PathEscape(setID)+"/appScreenshots", url.Values{"limit": {"200"}}) {
		if pageErr != nil {
			return nil, fmt.Errorf("apply screenshots: list existing: %w", pageErr)
		}
		for _, resource := range page.Data {
			existing = append(existing, &managedScreenshotFile{id: resource.ID, checksum: resource.Attributes.SourceFileChecksum})
		}
	}
	return existing, nil
}

func reconcileOneScreenshotFile(ctx context.Context, c *asc.Client, actx ApplyContext, setID string, file config.ScreenshotFile, existing []*managedScreenshotFile) error {
	path, err := resolveAssetPath(actx.StateDir, file.Path)
	if err != nil {
		return err
	}
	checksum, err := fileMD5(path)
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if !candidate.consumed && candidate.checksum == checksum {
			candidate.consumed = true
			return nil
		}
	}
	if _, err := c.Upload(ctx, asc.UploadOptions{
		Kind: asc.AssetKindAppScreenshot, ParentID: setID,
		Asset: asc.UploadAsset{Path: path}, ResumeFromCheckpoint: actx.ResumeUploads,
	}); err != nil {
		return fmt.Errorf("apply screenshots: upload %s: %w", path, err)
	}
	return nil
}

func deleteUnmanagedScreenshotFiles(ctx context.Context, c *asc.Client, existing []*managedScreenshotFile) error {
	for _, file := range existing {
		if file.consumed {
			continue
		}
		if err := c.Delete(ctx, "/v1/appScreenshots/"+url.PathEscape(file.id), nil); err != nil {
			return fmt.Errorf("apply screenshots: delete stale asset %s: %w", file.id, err)
		}
	}
	return nil
}

func decodeScreenshotFiles(value any) ([]config.ScreenshotFile, error) {
	if files, ok := value.([]config.ScreenshotFile); ok {
		return files, nil
	}
	buf, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("apply screenshots: encode target: %w", err)
	}
	var files []config.ScreenshotFile
	if err := json.Unmarshal(buf, &files); err != nil {
		return nil, fmt.Errorf("apply screenshots: decode target: %w", err)
	}
	return files, nil
}

func decodeIAPReviewScreenshot(value any) (config.IAPReviewScreenshot, error) {
	var screenshot config.IAPReviewScreenshot
	if screenshot, ok := value.(*config.IAPReviewScreenshot); ok && screenshot != nil {
		return validateIAPReviewScreenshot(*screenshot)
	}
	if screenshot, ok := value.(config.IAPReviewScreenshot); ok {
		return validateIAPReviewScreenshot(screenshot)
	}
	buf, err := json.Marshal(value)
	if err != nil {
		return config.IAPReviewScreenshot{}, err
	}
	if err := json.Unmarshal(buf, &screenshot); err != nil {
		return screenshot, err
	}
	return validateIAPReviewScreenshot(screenshot)
}

func validateIAPReviewScreenshot(screenshot config.IAPReviewScreenshot) (config.IAPReviewScreenshot, error) {
	if screenshot.Path == "" {
		return screenshot, errors.New("screenshot path is required")
	}
	return screenshot, nil
}

func resolveAssetPath(stateDir, path string) (string, error) {
	if path == "" {
		return "", errors.New("asset path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(stateDir, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat asset %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", fmt.Errorf("asset %s must be a non-empty regular file", path)
	}
	return path, nil
}

func fileMD5(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // path is resolved from the trusted state file
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := md5.New() //nolint:gosec // Apple's upload protocol requires MD5
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// applyTestFlightField creates a beta group, PATCHes a field, or adds/removes a tester by sub-path.
func applyTestFlightField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	rest := strings.TrimPrefix(ch.Path, "/spec/testflight/groups/")
	parts := strings.SplitN(rest, "/", 2)
	groupName := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}

	if subPath == "" {
		buf, _ := json.Marshal(ch.To)
		var g struct {
			IsInternal      *bool `json:"isInternal,omitempty"`
			PublicLink      *bool `json:"publicLink,omitempty"`
			PublicLinkLimit *int  `json:"publicLinkLimit,omitempty"`
		}
		_ = json.Unmarshal(buf, &g)
		body := map[string]any{
			"data": map[string]any{
				"type": "betaGroups",
				"attributes": map[string]any{
					"name":              groupName,
					"publicLinkEnabled": g.PublicLink,
					"publicLinkLimit":   g.PublicLinkLimit,
				},
				"relationships": map[string]any{
					"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}},
				},
			},
		}
		if _, err := asc.Post[asc.Single[asc.BetaGroupAttributes]](ctx, c, "/v1/betaGroups", nil, body); err != nil {
			return fmt.Errorf("apply testflight.create %s: %w", groupName, err)
		}
		return nil
	}

	groupID, err := resolveBetaGroupByName(ctx, c, appID, groupName)
	if err != nil {
		return err
	}

	if strings.HasPrefix(subPath, "testers/") {
		email := strings.TrimPrefix(subPath, "testers/")
		switch ch.Op {
		case plan.OpCreate:
			return createTester(ctx, c, groupID, email)
		case plan.OpDelete:
			return removeTester(ctx, c, groupID, email)
		}
		return fmt.Errorf("apply testflight.%s.testers/%s: unsupported op %s", groupName, email, ch.Op)
	}

	// Group-attribute PATCH.
	wire := subPath
	if subPath == "publicLink" {
		wire = "publicLinkEnabled"
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "betaGroups",
			"id":         groupID,
			"attributes": map[string]any{wire: ch.To},
		},
	}
	if _, err := asc.Patch[asc.Single[asc.BetaGroupAttributes]](ctx, c, "/v1/betaGroups/"+groupID, nil, body); err != nil {
		return fmt.Errorf("apply testflight.%s.%s: %w", groupName, subPath, err)
	}
	return nil
}

func resolveBetaGroupByName(ctx context.Context, c *asc.Client, appID, name string) (string, error) {
	q := url.Values{"filter[name]": {name}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.BetaGroupAttributes]](
		ctx, c, "/v1/apps/"+appID+"/betaGroups", q,
	)
	if err != nil {
		return "", fmt.Errorf("resolve betaGroup %s: %w", name, err)
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("betaGroup %s not found on app", name)
	}
	return page.Data[0].ID, nil
}

func createTester(ctx context.Context, c *asc.Client, groupID, email string) error {
	q := url.Values{"filter[email]": {email}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.BetaTesterAttributes]](ctx, c, "/v1/betaTesters", q)
	if err != nil {
		return fmt.Errorf("lookup tester %s: %w", email, err)
	}
	var testerID string
	if len(page.Data) > 0 {
		testerID = page.Data[0].ID
	} else {
		body := map[string]any{
			"data": map[string]any{
				"type":       "betaTesters",
				"attributes": map[string]any{"email": email},
				"relationships": map[string]any{
					"betaGroups": map[string]any{
						"data": []any{map[string]any{"type": "betaGroups", "id": groupID}},
					},
				},
			},
		}
		resp, err := asc.Post[asc.Single[asc.BetaTesterAttributes]](ctx, c, "/v1/betaTesters", nil, body)
		if err != nil {
			return fmt.Errorf("create tester %s: %w", email, err)
		}
		return relationshipNoOpIfPresent(ctx, c, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", resp.Data.ID)
	}
	return relationshipNoOpIfPresent(ctx, c, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", testerID)
}

func removeTester(ctx context.Context, c *asc.Client, groupID, email string) error {
	q := url.Values{"filter[email]": {email}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.BetaTesterAttributes]](ctx, c, "/v1/betaTesters", q)
	if err != nil {
		return fmt.Errorf("lookup tester %s: %w", email, err)
	}
	if len(page.Data) == 0 {
		return nil // nothing to remove
	}
	body := map[string]any{
		"data": []any{map[string]any{"type": "betaTesters", "id": page.Data[0].ID}},
	}
	if err := c.DeleteWithBody(ctx, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", nil, body); err != nil {
		return fmt.Errorf("remove tester %s: %w", email, err)
	}
	return nil
}

// relationshipNoOpIfPresent POSTs a to-many relationship link; JSON:API treats duplicate adds as idempotent.
func relationshipNoOpIfPresent(ctx context.Context, c *asc.Client, path, testerID string) error {
	body := map[string]any{
		"data": []any{map[string]any{"type": "betaTesters", "id": testerID}},
	}
	if _, err := asc.Post[map[string]any](ctx, c, path, nil, body); err != nil {
		return fmt.Errorf("add tester to group: %w", err)
	}
	return nil
}

// applyCustomProductPageField reconciles a page, its editable version/localizations, and screenshot assets.
func applyCustomProductPageField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	rest := strings.TrimPrefix(ch.Path, "/spec/customProductPages/")
	parts := strings.SplitN(rest, "/", 2)
	pageName := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}

	if subPath == "" {
		return createCustomProductPage(ctx, c, actx, appID, pageName, ch.To)
	}

	if subPath == "visible" {
		pageID, err := resolveCustomProductPage(ctx, c, appID, pageName)
		if err != nil {
			return err
		}
		visible, ok := ch.To.(bool)
		if !ok {
			return fmt.Errorf("apply customProductPages.%s.visible: expected bool, got %T", pageName, ch.To)
		}
		return patchCustomProductPageVisible(ctx, c, pageID, visible)
	}

	if strings.HasPrefix(subPath, "localizations/") {
		pageID, err := resolveCustomProductPage(ctx, c, appID, pageName)
		if err != nil {
			return err
		}
		return applyCPPLocalizationField(ctx, c, actx, pageID, pageName, subPath, ch.To)
	}
	return fmt.Errorf("apply customProductPages.%s.%s: unhandled sub-path", pageName, subPath)
}

func createCustomProductPage(ctx context.Context, c *asc.Client, actx ApplyContext, appID, pageName string, target any) error {
	buf, _ := json.Marshal(target)
	var page config.CustomProductPage
	_ = json.Unmarshal(buf, &page)
	body := map[string]any{"data": map[string]any{
		"type":       "appCustomProductPages",
		"attributes": map[string]any{"name": pageName},
		"relationships": map[string]any{
			"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}},
		},
	}}
	resp, err := asc.Post[asc.Single[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/appCustomProductPages", nil, body,
	)
	if err != nil {
		return fmt.Errorf("apply customProductPages.create %s: %w", pageName, err)
	}
	pageID := resp.Data.ID
	if pageID == "" {
		return fmt.Errorf("apply customProductPages.create %s: empty id", pageName)
	}
	if page.Visible != nil {
		if err := patchCustomProductPageVisible(ctx, c, pageID, *page.Visible); err != nil {
			return err
		}
	}
	for _, locale := range sortedMapKeys(page.Localizations) {
		if err := reconcileCPPLocalization(ctx, c, actx, pageID, locale, page.Localizations[locale]); err != nil {
			return fmt.Errorf("apply customProductPages.%s.localizations.%s: %w", pageName, locale, err)
		}
	}
	return nil
}

func applyCPPLocalizationField(ctx context.Context, c *asc.Client, actx ApplyContext, pageID, pageName, subPath string, target any) error {
	parts := strings.Split(strings.TrimPrefix(subPath, "localizations/"), "/")
	if len(parts) < 2 {
		return fmt.Errorf("apply customProductPages.%s.%s: malformed localization path", pageName, subPath)
	}
	versionID, err := ensureEditableCPPVersion(ctx, c, pageID)
	if err != nil {
		return err
	}
	localizationID, err := ensureCPPLocalization(ctx, c, versionID, parts[0], nil)
	if err != nil {
		return err
	}
	if len(parts) == 2 && parts[1] == "promotionalText" {
		return patchCPPPromotionalText(ctx, c, localizationID, pageName, subPath, target)
	}
	if len(parts) == 3 && parts[1] == "screenshots" {
		setID, err := findOrCreateManagedScreenshotSet(ctx, c, localizationID,
			"appCustomProductPageLocalization", "appCustomProductPageLocalizations", parts[2])
		if err != nil {
			return err
		}
		return reconcileScreenshotFiles(ctx, c, actx, setID, target)
	}
	return fmt.Errorf("apply customProductPages.%s.%s: unhandled localization path", pageName, subPath)
}

func patchCPPPromotionalText(ctx context.Context, c *asc.Client, localizationID, pageName, subPath string, target any) error {
	body := map[string]any{"data": map[string]any{
		"type": "appCustomProductPageLocalizations", "id": localizationID,
		"attributes": map[string]any{"promotionalText": target},
	}}
	if _, err := asc.Patch[asc.Single[asc.AppCustomProductPageLocalizationAttributes]](
		ctx, c, "/v1/appCustomProductPageLocalizations/"+url.PathEscape(localizationID), nil, body,
	); err != nil {
		return fmt.Errorf("apply customProductPages.%s.%s: %w", pageName, subPath, err)
	}
	return nil
}

func patchCustomProductPageVisible(ctx context.Context, c *asc.Client, pageID string, visible bool) error {
	body := map[string]any{"data": map[string]any{
		"type": "appCustomProductPages", "id": pageID,
		"attributes": map[string]any{"visible": visible},
	}}
	if _, err := asc.Patch[asc.Single[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/appCustomProductPages/"+url.PathEscape(pageID), nil, body,
	); err != nil {
		return fmt.Errorf("apply customProductPages.visible: %w", err)
	}
	return nil
}

func reconcileCPPLocalization(ctx context.Context, c *asc.Client, actx ApplyContext, pageID, locale string, desired config.CustomProductPageLocale) error {
	versionID, err := ensureEditableCPPVersion(ctx, c, pageID)
	if err != nil {
		return err
	}
	localizationID, err := ensureCPPLocalization(ctx, c, versionID, locale, desired.PromotionalText)
	if err != nil {
		return err
	}
	for _, device := range sortedMapKeys(desired.Screenshots) {
		setID, err := findOrCreateManagedScreenshotSet(ctx, c, localizationID,
			"appCustomProductPageLocalization", "appCustomProductPageLocalizations", device)
		if err != nil {
			return err
		}
		if err := reconcileScreenshotFiles(ctx, c, actx, setID, desired.Screenshots[device]); err != nil {
			return err
		}
	}
	return nil
}

func ensureEditableCPPVersion(ctx context.Context, c *asc.Client, pageID string) (string, error) {
	path := "/v1/appCustomProductPages/" + url.PathEscape(pageID) + "/appCustomProductPageVersions"
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageVersionAttributes]](
		ctx, c, path, url.Values{"limit": {"50"}},
	)
	if err != nil {
		return "", fmt.Errorf("apply customProductPages: list versions: %w", err)
	}
	for _, version := range page.Data {
		switch version.Attributes.State {
		case "PREPARE_FOR_SUBMISSION", "REJECTED":
			return version.ID, nil
		}
	}
	body := map[string]any{"data": map[string]any{
		"type": "appCustomProductPageVersions",
		"relationships": map[string]any{"appCustomProductPage": map[string]any{
			"data": map[string]any{"type": "appCustomProductPages", "id": pageID},
		}},
	}}
	resp, err := asc.Post[asc.Single[asc.AppCustomProductPageVersionAttributes]](
		ctx, c, "/v1/appCustomProductPageVersions", nil, body,
	)
	if err != nil {
		return "", fmt.Errorf("apply customProductPages: create version: %w", err)
	}
	if resp.Data.ID == "" {
		return "", errors.New("apply customProductPages: create version returned empty id")
	}
	return resp.Data.ID, nil
}

func ensureCPPLocalization(ctx context.Context, c *asc.Client, versionID, locale string, promotionalText *string) (string, error) {
	path := "/v1/appCustomProductPageVersions/" + url.PathEscape(versionID) + "/appCustomProductPageLocalizations"
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageLocalizationAttributes]](
		ctx, c, path, url.Values{"limit": {"50"}},
	)
	if err != nil {
		return "", fmt.Errorf("apply customProductPages: list localizations: %w", err)
	}
	for _, localization := range page.Data {
		if localization.Attributes.Locale == locale {
			return localization.ID, nil
		}
	}
	attributes := map[string]any{"locale": locale}
	if promotionalText != nil {
		attributes["promotionalText"] = *promotionalText
	}
	body := map[string]any{"data": map[string]any{
		"type": "appCustomProductPageLocalizations", "attributes": attributes,
		"relationships": map[string]any{"appCustomProductPageVersion": map[string]any{
			"data": map[string]any{"type": "appCustomProductPageVersions", "id": versionID},
		}},
	}}
	resp, err := asc.Post[asc.Single[asc.AppCustomProductPageLocalizationAttributes]](
		ctx, c, "/v1/appCustomProductPageLocalizations", nil, body,
	)
	if err != nil {
		return "", fmt.Errorf("apply customProductPages: create localization %s: %w", locale, err)
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("apply customProductPages: create localization %s returned empty id", locale)
	}
	return resp.Data.ID, nil
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolveCustomProductPage(ctx context.Context, c *asc.Client, appID, name string) (string, error) {
	q := url.Values{"filter[name]": {name}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appCustomProductPages", q,
	)
	if err != nil {
		return "", fmt.Errorf("resolve customProductPage %s: %w", name, err)
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("customProductPage %s not found on app", name)
	}
	return page.Data[0].ID, nil
}

// asStringSlice coerces a Change.To into []string; handles both []string and []any (JSON round-trip).
func asStringSlice(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	switch s := v.(type) {
	case []string:
		return s, nil
	case []any:
		out := make([]string, len(s))
		for i, e := range s {
			str, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("expected []string, got element %T at %d", e, i)
			}
			out[i] = str
		}
		return out, nil
	}
	return nil, fmt.Errorf("expected []string, got %T", v)
}

// fetchPricingPair returns (baseTerritory, pricePointId) from the live schedule, or empty strings when absent.
func fetchPricingPair(ctx context.Context, c *asc.Client, appID string) (territory, pricePoint string) {
	q := url.Values{"include": {"baseTerritory,manualPrices.appPricePoint"}, "limit": {"1"}}
	resp, err := asc.Get[asc.Single[asc.AppPriceScheduleAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appPriceSchedule", q,
	)
	if err != nil {
		return "", "" // caller surfaces a specific error if both fields are required
	}
	terr := ""
	pp := ""
	for _, raw := range resp.Included {
		var rec struct {
			Type          string         `json:"type"`
			ID            string         `json:"id"`
			Attributes    map[string]any `json:"attributes,omitempty"`
			Relationships map[string]any `json:"relationships,omitempty"`
		}
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		switch rec.Type {
		case "territories":
			if terr == "" {
				terr = rec.ID
			}
		case "appPricePoints":
			if pp == "" {
				pp = rec.ID
			}
		}
	}
	return terr, pp
}

// lookupEnvFn and readFileFn are package-level stubs so tests can inject deterministic values.
var (
	lookupEnvFn = os.LookupEnv
	readFileFn  = os.ReadFile
)
