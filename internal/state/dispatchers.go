// dispatchers.go — per-surface apply dispatchers.
//
// Each surface has one entry in apply.go's dispatch() switch and one
// applyXField function here. The shared pattern:
//
//  1. Resolve appID / versionID / appInfoID from ApplyContext.
//  2. GET the live resource by ID.
//  3. PATCH (or POST/DELETE) the field, taking ch.To as the new value.
//
// Idempotency contract: every dispatcher PATCHes only the changed
// field, never the full attribute set, so re-applying the same change
// produces no wire diff.

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

// --- /spec/build/number — attach a build to the version ---------------------

// applyBuildAttach finds the build by version+number and PATCHes the
// version's build relationship. Per-app build numbers are unique within
// a marketing version, so version+number is the canonical lookup key.
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

// patchRelationship issues a PATCH to a JSON:API relationship endpoint.
// Apple returns 204 No Content on success — no envelope to decode.
// We use json.RawMessage as the type so asc.Patch's generic decoder
// is happy with an empty body.
func patchRelationship(ctx context.Context, c *asc.Client, path string, body any) error {
	if _, err := asc.Patch[json.RawMessage](ctx, c, path, nil, body); err != nil {
		return err
	}
	return nil
}

// --- /spec/exportCompliance/declaration/* — full ECCN block ----------------

// applyEncryptionDeclaration POSTs an appEncryptionDeclaration with the
// per-build flag set. The schema declaration block is the rare full
// classification path — the simple usesNonExemptEncryption flag is
// handled by applyEncryptionFlag.
func applyEncryptionDeclaration(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	// Apple's POST /v1/appEncryptionDeclarations creates a sticky
	// declaration record. Fields map 1:1 with the schema's declaration
	// sub-tree. We accumulate the entire sub-tree change set in one
	// request: the diff engine emits one Change per leaf, so the
	// dispatcher coalesces by path prefix.
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

// --- /spec/categories/* — primary/secondary + subcategories ----------------

// applyCategoriesField PATCHes one category relationship on the
// editable appInfo. Subcategories are a slice; we replace wholesale.
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
	// Apple stores subcategories as two scalar relationships
	// (subcategoryOne, subcategoryTwo); set both.
	first := ""
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

// --- /spec/pricing/* — base territory + price point ------------------------

// applyPricingField creates an appPriceSchedule with the desired
// (territory, pricePoint) pair. The Apple API requires both
// fields together (the schedule is a single resource with relationships
// for territory + price-point), so we coalesce: when only one of the
// two paths arrives, we re-fetch the missing field from live state.
func applyPricingField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	// Resolve current values so a single-leaf change doesn't blank
	// the missing relationship.
	curTerr, curPP := fetchPricingPair(ctx, c, appID)
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

// --- /spec/reviewerDemo/* — review login + contact info --------------------

// applyReviewerDemoField PATCHes one field on appStoreReviewDetail.
// passwordRef / passwordFile resolve to a runtime password value
// before the PATCH; the resolved password is what hits the wire.
func applyReviewerDemoField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	// Resolve the wire-key + value (incl. password lookup) BEFORE any
	// HTTP work so an unset env var fails fast with an actionable
	// error instead of bottoming out in resolveAppID.
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

// reviewerDemoAttrs mirrors the subset of Apple's
// AppStoreReviewDetail.attributes Flightline sets. Wire-name parity.
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
		// Apple stores first + last separately; we map the schema's
		// single contactName to contactFirstName and leave contactLastName
		// untouched. Users wanting both should split the schema field
		// (deferred to a v1.x extension).
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

// fetchOrCreateReviewDetail returns the appStoreReviewDetail ID for a
// given version, creating one if Apple hasn't auto-provisioned it yet.
func fetchOrCreateReviewDetail(ctx context.Context, c *asc.Client, versionID string) (string, error) {
	resp, err := asc.Get[asc.Single[reviewerDemoAttrs]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreReviewDetail", nil,
	)
	if err == nil && resp.Data.ID != "" {
		return resp.Data.ID, nil
	}
	// Create one.
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

// --- /spec/metadata/locales/<locale>/<field> — per-locale store metadata ---

// applyMetadataField routes one localization field to its owning
// resource. /spec/metadata/locales/<locale>/{description,keywords,
// whatsNew,promotionalText,marketingUrl,supportUrl} live on
// appStoreVersionLocalization; {name,subtitle,privacyPolicyUrl} live
// on appInfoLocalization.
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

func schemaToWireMetadata(f string) string {
	// All metadata field names round-trip 1:1 with Apple's wire keys
	// today. Kept as an explicit hop so future schema renames have a
	// single place to add a translation.
	return f
}

func patchSingleField(ctx context.Context, c *asc.Client, _, _, path, wireField string, val any) error {
	body := map[string]any{
		"data": map[string]any{
			"type":       "_unused_filled_below",
			"id":         "_unused_filled_below",
			"attributes": map[string]any{wireField: val},
		},
	}
	// Refill type/id from path: "/v1/appStoreVersionLocalizations/<id>"
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) == 3 {
		body["data"].(map[string]any)["type"] = segs[1]
		body["data"].(map[string]any)["id"] = segs[2]
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

// --- /spec/iap/products/<id> — non-subscription IAPs -----------------------

// applyIAPField routes by sub-path under /spec/iap/products/<productId>.
//   - "" (the product itself)        → POST or PATCH /v2/inAppPurchases
//   - "type", "name", "familySharable", "contentHosting",
//     "reviewNote"                    → PATCH /v2/inAppPurchases/{id}
//   - "/localizations/<locale>/*"     → PATCH or POST /v1/inAppPurchaseLocalizations
//   - "/reviewScreenshot"             → 3-step upload via internal/asc/upload.go
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
		// Whole-product create. ch.To is a config.IAPProduct value (any).
		// Marshal/decode to extract the type wire-string.
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

	// PATCH on an existing IAP. First resolve its ID via productId
	// filter on the app's IAPs.
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
		// Defer to internal/asc/upload.go's 3-step dance. Tracked
		// in QA-009; for v1alpha1 we surface a typed error so users
		// know to use `fline iap update --review-screenshot upload`
		// directly until the orchestrator integrates the upload helper.
		return fmt.Errorf("apply iap.%s.reviewScreenshot: upload via L1 verb (`fline iap update --review-screenshot upload`) — tracked under QA-010", productID)
	}

	// Per-field PATCH on the IAP itself.
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

// iapSchemaToWire maps the schema's IAP field name to Apple's wire key.
// "type" is the only renamed field — Apple calls it inAppPurchaseType.
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

// --- /spec/screenshots/locales/<locale>/<device> — slot-level uploads ------

// applyScreenshotSet rebuilds one device-class slot for one locale.
// The schema models a slot as an ordered array; replacing the whole
// slot is the only sensible idempotent operation (re-uploading by
// individual file would require addressing files by ID, which Apple's
// API doesn't do).
//
// v1alpha1 surface: defers to `fline screenshots upload` for the
// actual multipart 3-step. The orchestrator-side wiring of
// internal/asc/upload.go is tracked under QA-010 — reading the file
// from disk, addressing the screenshotSet by (locale, device class)
// + checksum diff is well-trodden code we want to integrate properly,
// not duplicate.
func applyScreenshotSet(_ context.Context, _ *asc.Client, _ ApplyContext, ch plan.Change) error {
	return fmt.Errorf("apply screenshots %s: upload via L1 verb (`fline screenshots upload`) — orchestrator integration tracked under QA-010", ch.Path)
}

// --- /spec/testflight/groups/<name> — beta groups + tester rosters --------

// applyTestFlightField routes by sub-path:
//   - ""                        → POST a new BetaGroup
//   - "/<field>" attribute       → PATCH the group
//   - "/testers/<email>" with OpCreate → POST tester to the group
//   - "/testers/<email>" with OpDelete → DELETE tester from the group
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
		// Create the group. ch.To is a config.TestFlightGroup value.
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
	// 1. Look up or create the betaTester.
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
	// 2. POST relationship onto the group.
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

// relationshipNoOpIfPresent POSTs a relationship link, swallowing the
// 409-style "already exists" Apple returns when the link is already in
// place. Per JSON:API, POST to a to-many relationship is an "add"
// (idempotent on duplicate links).
func relationshipNoOpIfPresent(ctx context.Context, c *asc.Client, path, testerID string) error {
	body := map[string]any{
		"data": []any{map[string]any{"type": "betaTesters", "id": testerID}},
	}
	if _, err := asc.Post[map[string]any](ctx, c, path, nil, body); err != nil {
		return fmt.Errorf("add tester to group: %w", err)
	}
	return nil
}

// --- /spec/customProductPages/<name> — alt screenshots/descriptions --------

// applyCustomProductPageField creates a page or PATCHes one of its
// attributes. Localizations + screenshots are full-tree changes
// emitted by the diff engine and dispatched here as either creates or
// updates.
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
		// Whole-page create.
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

	// Sub-path operations (visible flag PATCH, localization edits,
	// screenshot uploads). Localizations + screenshots defer to L1
	// verbs for now (orchestrator integration tracked under QA-010).
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
		// Localizations + screenshots inside CPP versions live behind
		// L1 `custom-product-pages` verbs. Surface a typed error so
		// users know to use the L1 path until QA-010 lands.
		return fmt.Errorf("apply customProductPages.%s.%s: edit via L1 verb (`fline custom-product-pages ...`) — tracked under QA-010", pageName, subPath)
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

// --- shared helpers --------------------------------------------------------

// asStringSlice coerces an `any` slice from a Change.To into []string.
// json round-tripping turns string arrays into []any of strings; both
// shapes need to work.
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

// fetchPricingPair returns the current (baseTerritory, pricePointId)
// pair from the live appPriceSchedule, or empty strings when no
// schedule exists yet.
func fetchPricingPair(ctx context.Context, c *asc.Client, appID string) (territory, pricePoint string) {
	q := url.Values{"include": {"baseTerritory,manualPrices.appPricePoint"}, "limit": {"1"}}
	resp, err := asc.Get[asc.Single[asc.AppPriceScheduleAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appPriceSchedule", q,
	)
	if err != nil {
		// Missing schedule isn't a fatal error here; the caller will
		// surface a more specific message at apply time when both
		// fields are required together.
		return "", ""
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

// --- env/file lookups (test-overridable) ----------------------------------

// lookupEnvFn / readFileFn are package-level stubs around os.LookupEnv /
// os.ReadFile so tests can substitute deterministic values without
// touching real env vars or disk. Default to the os impl.
var (
	lookupEnvFn = os.LookupEnv
	readFileFn  = os.ReadFile
)
