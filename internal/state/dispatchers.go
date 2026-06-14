package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
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
		"filter[app]":                       {appID},
		"filter[preReleaseVersion.version]": {actx.Version},
		"filter[version]":                   {target},
		"limit":                             {"1"},
	}
	bp, err := asc.Get[asc.Collection[asc.BuildAttributes]](ctx, c, "/v1/builds", q)
	if err != nil {
		return fmt.Errorf("apply build.number: lookup build %s: %w", target, err)
	}
	if len(bp.Data) == 0 {
		return fmt.Errorf("apply build.number: no build %q for version %s (uploaded yet?)", target, actx.Version)
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
		locID, err := getOrCreateIAPLocalization(ctx, c, iapID, locale)
		if err != nil {
			return err
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
		return fmt.Errorf("apply iap.%s.reviewScreenshot: upload via L1 verb (`flightline iap update --review-screenshot upload`): tracked under QA-010", productID)
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

func getOrCreateIAPLocalization(ctx context.Context, c *asc.Client, iapID, locale string) (string, error) {
	q := url.Values{"filter[locale]": {locale}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.IAPLocalizationAttributes]](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/inAppPurchaseLocalizations", q,
	)
	if err != nil {
		return "", fmt.Errorf("list iap localizations: %w", err)
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "inAppPurchaseLocalizations",
			"attributes": map[string]any{"locale": locale},
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
		return "", fmt.Errorf("create iap localization: %w", err)
	}
	return resp.Data.ID, nil
}

// applyScreenshotSet defers to the L1 `flightline screenshots upload` verb (QA-010).
func applyScreenshotSet(_ context.Context, _ *asc.Client, _ ApplyContext, ch plan.Change) error {
	return fmt.Errorf("apply screenshots %s: upload via L1 verb (`flightline screenshots upload`): orchestrator integration tracked under QA-010", ch.Path)
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

// applyCustomProductPageField creates a page or PATCHes one attribute; localizations defer to L1 (QA-010).
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
		buf, _ := json.Marshal(ch.To)
		var p struct {
			Visible *bool `json:"visible,omitempty"`
		}
		_ = json.Unmarshal(buf, &p)
		body := map[string]any{
			"data": map[string]any{
				"type": "customProductPages",
				"attributes": map[string]any{
					"name":    pageName,
					"visible": p.Visible,
				},
				"relationships": map[string]any{
					"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}},
				},
			},
		}
		if _, err := asc.Post[asc.Single[asc.AppCustomProductPageAttributes]](
			ctx, c, "/v1/customProductPages", nil, body,
		); err != nil {
			return fmt.Errorf("apply customProductPages.create %s: %w", pageName, err)
		}
		return nil
	}

	if subPath == "visible" {
		pageID, err := resolveCustomProductPage(ctx, c, appID, pageName)
		if err != nil {
			return err
		}
		body := map[string]any{
			"data": map[string]any{
				"type":       "customProductPages",
				"id":         pageID,
				"attributes": map[string]any{"visible": ch.To},
			},
		}
		if _, err := asc.Patch[asc.Single[asc.AppCustomProductPageAttributes]](
			ctx, c, "/v1/customProductPages/"+pageID, nil, body,
		); err != nil {
			return fmt.Errorf("apply customProductPages.%s.visible: %w", pageName, err)
		}
		return nil
	}

	if strings.HasPrefix(subPath, "localizations/") {
		return fmt.Errorf("apply customProductPages.%s.%s: edit via L1 verb (`flightline custom-product-pages ...`): tracked under QA-010", pageName, subPath)
	}
	return fmt.Errorf("apply customProductPages.%s.%s: unhandled sub-path", pageName, subPath)
}

func resolveCustomProductPage(ctx context.Context, c *asc.Client, appID, name string) (string, error) {
	q := url.Values{"filter[name]": {name}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/apps/"+appID+"/customProductPages", q,
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
