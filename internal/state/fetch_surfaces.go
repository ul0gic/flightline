// fetch_surfaces.go — per-spec-surface live-state projectors.
//
// Each fetchX function in this file pulls one schema surface from
// ASC and returns the typed *config.* value for it. Errors are
// returned (callers in fetch.go decide whether to surface them or
// treat them as "not managed" — most surfaces are best-effort because
// a fresh app has empty rosters/locales/etc.).

package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/skipper/internal/asc"
	"github.com/ul0gic/skipper/internal/config"
)

// --- build number ----------------------------------------------------------

func fetchBuildNumber(ctx context.Context, c *asc.Client, buildID string) (string, error) {
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](ctx, c, "/v1/builds/"+buildID, nil)
	if err != nil {
		return "", err
	}
	return resp.Data.Attributes.Version, nil
}

// --- metadata locales ------------------------------------------------------

func fetchMetadataLocales(ctx context.Context, c *asc.Client, versionID, appInfoID string) (*config.MetadataSpec, error) {
	out := &config.MetadataSpec{Locales: map[string]config.MetadataLocale{}}

	verLocs, err := listVersionLocalizations(ctx, c, versionID)
	if err != nil {
		return nil, err
	}
	for _, attrs := range verLocs {
		ml := out.Locales[attrs.Locale]
		copyVerLocAttrsToSchema(&ml, attrs)
		out.Locales[attrs.Locale] = ml
	}

	appLocs, err := listAppInfoLocalizations(ctx, c, appInfoID)
	if err != nil {
		return out, nil //nolint:nilerr // version locs succeeded; app-info side is best-effort
	}
	for _, attrs := range appLocs {
		ml := out.Locales[attrs.Locale]
		copyAppInfoLocAttrsToSchema(&ml, attrs)
		out.Locales[attrs.Locale] = ml
	}
	if len(out.Locales) == 0 {
		return nil, nil
	}
	return out, nil
}

type versionLocAttrs struct {
	Locale          string `json:"locale,omitempty"`
	Description     string `json:"description,omitempty"`
	Keywords        string `json:"keywords,omitempty"`
	WhatsNew        string `json:"whatsNew,omitempty"`
	PromotionalText string `json:"promotionalText,omitempty"`
	MarketingURL    string `json:"marketingUrl,omitempty"`
	SupportURL      string `json:"supportUrl,omitempty"`
}

type appInfoLocAttrs struct {
	Locale           string `json:"locale,omitempty"`
	Name             string `json:"name,omitempty"`
	Subtitle         string `json:"subtitle,omitempty"`
	PrivacyPolicyURL string `json:"privacyPolicyUrl,omitempty"`
}

func listVersionLocalizations(ctx context.Context, c *asc.Client, versionID string) ([]versionLocAttrs, error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[versionLocAttrs]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations", q,
	)
	if err != nil {
		return nil, fmt.Errorf("list version localizations: %w", err)
	}
	out := make([]versionLocAttrs, 0, len(page.Data))
	for _, r := range page.Data {
		out = append(out, r.Attributes)
	}
	return out, nil
}

func listAppInfoLocalizations(ctx context.Context, c *asc.Client, appInfoID string) ([]appInfoLocAttrs, error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[appInfoLocAttrs]](
		ctx, c, "/v1/appInfos/"+appInfoID+"/appInfoLocalizations", q,
	)
	if err != nil {
		return nil, fmt.Errorf("list appInfo localizations: %w", err)
	}
	out := make([]appInfoLocAttrs, 0, len(page.Data))
	for _, r := range page.Data {
		out = append(out, r.Attributes)
	}
	return out, nil
}

func copyVerLocAttrsToSchema(ml *config.MetadataLocale, a versionLocAttrs) {
	if a.Description != "" {
		s := a.Description
		ml.Description = &s
	}
	if a.Keywords != "" {
		s := a.Keywords
		ml.Keywords = &s
	}
	if a.WhatsNew != "" {
		s := a.WhatsNew
		ml.WhatsNew = &s
	}
	if a.PromotionalText != "" {
		s := a.PromotionalText
		ml.PromotionalText = &s
	}
	if a.MarketingURL != "" {
		s := a.MarketingURL
		ml.MarketingURL = &s
	}
	if a.SupportURL != "" {
		s := a.SupportURL
		ml.SupportURL = &s
	}
}

func copyAppInfoLocAttrsToSchema(ml *config.MetadataLocale, a appInfoLocAttrs) {
	if a.Name != "" {
		s := a.Name
		ml.Name = &s
	}
	if a.Subtitle != "" {
		s := a.Subtitle
		ml.Subtitle = &s
	}
	if a.PrivacyPolicyURL != "" {
		s := a.PrivacyPolicyURL
		ml.PrivacyPolicyURL = &s
	}
}

// --- categories ------------------------------------------------------------

type categoryRelationshipsResp struct {
	Data *struct {
		ID string `json:"id"`
	} `json:"data,omitempty"`
}

func fetchCategories(ctx context.Context, c *asc.Client, appInfoID string) *config.CategoriesSpec {
	out := &config.CategoriesSpec{}
	if id := getCategoryRelationship(ctx, c, appInfoID, "primaryCategory"); id != "" {
		s := id
		out.Primary = &s
	}
	if id := getCategoryRelationship(ctx, c, appInfoID, "secondaryCategory"); id != "" {
		s := id
		out.Secondary = &s
	}
	for _, rel := range []string{"primarySubcategoryOne", "primarySubcategoryTwo"} {
		if id := getCategoryRelationship(ctx, c, appInfoID, rel); id != "" {
			out.PrimarySubcategories = append(out.PrimarySubcategories, id)
		}
	}
	for _, rel := range []string{"secondarySubcategoryOne", "secondarySubcategoryTwo"} {
		if id := getCategoryRelationship(ctx, c, appInfoID, rel); id != "" {
			out.SecondarySubcategories = append(out.SecondarySubcategories, id)
		}
	}
	if out.Primary == nil && out.Secondary == nil &&
		len(out.PrimarySubcategories) == 0 && len(out.SecondarySubcategories) == 0 {
		return nil
	}
	return out
}

// getCategoryRelationship returns the linked category id, or "" when
// no link is set / Apple 404s the relationship endpoint. Errors are
// swallowed because empty is the canonical "unset" signal.
func getCategoryRelationship(ctx context.Context, c *asc.Client, appInfoID, rel string) string {
	resp, err := asc.Get[categoryRelationshipsResp](
		ctx, c, "/v1/appInfos/"+appInfoID+"/relationships/"+rel, nil,
	)
	if err != nil || resp.Data == nil {
		return ""
	}
	return resp.Data.ID
}

// --- pricing ---------------------------------------------------------------

func fetchPricing(ctx context.Context, c *asc.Client, appID string) *config.PricingSpec {
	terr, pp := fetchPricingPair(ctx, c, appID)
	if terr == "" && pp == "" {
		return nil
	}
	out := &config.PricingSpec{}
	if terr != "" {
		out.BaseTerritory = &terr
	}
	if pp != "" {
		out.AppPricePointID = &pp
	}
	return out
}

// --- reviewer demo ---------------------------------------------------------

func fetchReviewerDemo(ctx context.Context, c *asc.Client, versionID string) *config.ReviewerDemoSpec {
	resp, err := asc.Get[asc.Single[reviewerDemoAttrs]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreReviewDetail", nil,
	)
	if err != nil {
		// Many versions don't have a detail row provisioned yet —
		// surface as "not managed".
		return nil
	}
	a := resp.Data.Attributes
	if a.DemoAccountName == "" && a.ContactEmail == "" && a.ContactPhone == "" &&
		a.ContactFirstName == "" && a.ContactLastName == "" && a.Notes == "" {
		return nil
	}
	out := &config.ReviewerDemoSpec{}
	if a.DemoAccountName != "" {
		s := a.DemoAccountName
		out.Username = &s
	}
	if a.Notes != "" {
		s := a.Notes
		out.Notes = &s
	}
	// Apple stores first + last separately; the schema's contactName
	// is rendered as "First Last" on the way out. Round-trip from the
	// schema split is documented in the apply dispatcher.
	if a.ContactFirstName != "" || a.ContactLastName != "" {
		full := strings.TrimSpace(a.ContactFirstName + " " + a.ContactLastName)
		out.ContactName = &full
	}
	if a.ContactEmail != "" {
		s := a.ContactEmail
		out.ContactEmail = &s
	}
	if a.ContactPhone != "" {
		s := a.ContactPhone
		out.ContactPhone = &s
	}
	// Password is intentionally never round-tripped — see apply
	// dispatcher's resolvePassword.
	return out
}

// --- IAPs ------------------------------------------------------------------

func fetchIAPs(ctx context.Context, c *asc.Client, appID string) (*config.IAPSpec, error) {
	q := url.Values{"limit": {"100"}}
	page, err := asc.Get[asc.Collection[asc.IAPAttributes]](
		ctx, c, "/v1/apps/"+appID+"/inAppPurchasesV2", q,
	)
	if err != nil {
		return nil, fmt.Errorf("list IAPs: %w", err)
	}
	if len(page.Data) == 0 {
		return nil, nil
	}
	out := &config.IAPSpec{Products: map[string]config.IAPProduct{}}
	for _, r := range page.Data {
		prod := config.IAPProduct{Type: r.Attributes.InAppPurchaseType}
		if r.Attributes.Name != "" {
			s := r.Attributes.Name
			prod.Name = &s
		}
		if r.Attributes.FamilySharable != nil {
			v := *r.Attributes.FamilySharable
			prod.FamilySharable = &v
		}
		if r.Attributes.ContentHosting != nil {
			// Apple reports a *bool; schema models it as enum
			// "HOSTED"/"NON_HOSTED". Translate.
			s := "NON_HOSTED"
			if *r.Attributes.ContentHosting {
				s = "HOSTED"
			}
			prod.ContentHosting = &s
		}
		if r.Attributes.ReviewNote != "" {
			s := r.Attributes.ReviewNote
			prod.ReviewNote = &s
		}
		// localizations
		if locs, lerr := fetchIAPLocalizations(ctx, c, r.ID); lerr == nil && len(locs) > 0 {
			prod.Localizations = locs
		}
		// reviewScreenshot — surface metadata only (URL via asc.IAPReviewScreenshotAttributes).
		// Round-trip: we record an empty Path "" sentinel so apply doesn't try to re-upload
		// when re-fetched data matches the YAML's path.
		out.Products[r.Attributes.ProductID] = prod
	}
	return out, nil
}

func fetchIAPLocalizations(ctx context.Context, c *asc.Client, iapID string) (map[string]config.IAPLocalization, error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[asc.IAPLocalizationAttributes]](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/inAppPurchaseLocalizations", q,
	)
	if err != nil {
		return nil, err
	}
	if len(page.Data) == 0 {
		return nil, nil
	}
	out := map[string]config.IAPLocalization{}
	for _, r := range page.Data {
		l := config.IAPLocalization{}
		if r.Attributes.Name != "" {
			s := r.Attributes.Name
			l.Name = &s
		}
		if r.Attributes.Description != "" {
			s := r.Attributes.Description
			l.Description = &s
		}
		out[r.Attributes.Locale] = l
	}
	return out, nil
}

// --- TestFlight ------------------------------------------------------------

func fetchTestFlightGroups(ctx context.Context, c *asc.Client, appID string) (*config.TestFlightSpec, error) {
	q := url.Values{"limit": {"100"}}
	page, err := asc.Get[asc.Collection[asc.BetaGroupAttributes]](
		ctx, c, "/v1/apps/"+appID+"/betaGroups", q,
	)
	if err != nil {
		return nil, fmt.Errorf("list betaGroups: %w", err)
	}
	if len(page.Data) == 0 {
		return nil, nil
	}
	out := &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{}}
	for _, r := range page.Data {
		g := config.TestFlightGroup{}
		if r.Attributes.IsInternalGroup != nil {
			v := *r.Attributes.IsInternalGroup
			g.IsInternal = &v
		}
		if r.Attributes.PublicLinkEnabled != nil {
			v := *r.Attributes.PublicLinkEnabled
			g.PublicLink = &v
		}
		if r.Attributes.PublicLinkLimit > 0 {
			n := r.Attributes.PublicLinkLimit
			g.PublicLinkLimit = &n
		}
		if testers, terr := fetchGroupTesters(ctx, c, r.ID); terr == nil {
			g.Testers = testers
		}
		out.Groups[r.Attributes.Name] = g
	}
	return out, nil
}

func fetchGroupTesters(ctx context.Context, c *asc.Client, groupID string) ([]config.TestFlightTester, error) {
	q := url.Values{"limit": {"200"}}
	page, err := asc.Get[asc.Collection[asc.BetaTesterAttributes]](
		ctx, c, "/v1/betaGroups/"+groupID+"/betaTesters", q,
	)
	if err != nil {
		return nil, err
	}
	out := make([]config.TestFlightTester, 0, len(page.Data))
	for _, r := range page.Data {
		t := config.TestFlightTester{Email: r.Attributes.Email}
		if r.Attributes.FirstName != "" {
			s := r.Attributes.FirstName
			t.FirstName = &s
		}
		if r.Attributes.LastName != "" {
			s := r.Attributes.LastName
			t.LastName = &s
		}
		out = append(out, t)
	}
	return out, nil
}

// --- screenshots -----------------------------------------------------------

// fetchScreenshots walks every screenshot set on every version
// localization and projects to the schema's locale → device → []file
// shape. The Path field is left blank — Apple stores rendered URLs,
// not source paths — so the diff engine's full-tree comparison
// correctly flags any local-vs-live mismatch when the user edits
// state.yaml.
func fetchScreenshots(ctx context.Context, c *asc.Client, versionID string) (*config.ScreenshotsSpec, error) {
	verLocsResp, err := asc.Get[asc.Collection[versionLocAttrs]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations",
		url.Values{"limit": {"50"}},
	)
	if err != nil {
		return nil, fmt.Errorf("list locale rows: %w", err)
	}
	if len(verLocsResp.Data) == 0 {
		return nil, nil
	}

	out := &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{}}
	hadAny := false
	for _, locRow := range verLocsResp.Data {
		setsResp, serr := asc.Get[asc.Collection[screenshotSetAttrs]](
			ctx, c, "/v1/appStoreVersionLocalizations/"+locRow.ID+"/appScreenshotSets",
			url.Values{"limit": {"50"}},
		)
		if serr != nil {
			continue
		}
		if len(setsResp.Data) == 0 {
			continue
		}
		devices := map[string][]config.ScreenshotFile{}
		for _, set := range setsResp.Data {
			files, ferr := fetchScreenshotsInSet(ctx, c, set.ID)
			if ferr != nil || len(files) == 0 {
				continue
			}
			devices[set.Attributes.ScreenshotDisplayType] = files
		}
		if len(devices) > 0 {
			out.Locales[locRow.Attributes.Locale] = devices
			hadAny = true
		}
	}
	if !hadAny {
		return nil, nil
	}
	return out, nil
}

type screenshotSetAttrs struct {
	ScreenshotDisplayType string `json:"screenshotDisplayType,omitempty"`
}

type screenshotAttrs struct {
	FileName           string `json:"fileName,omitempty"`
	SourceFileChecksum string `json:"sourceFileChecksum,omitempty"`
	AssetDeliveryState any    `json:"assetDeliveryState,omitempty"`
}

func fetchScreenshotsInSet(ctx context.Context, c *asc.Client, setID string) ([]config.ScreenshotFile, error) {
	resp, err := asc.Get[asc.Collection[screenshotAttrs]](
		ctx, c, "/v1/appScreenshotSets/"+setID+"/appScreenshots",
		url.Values{"limit": {"20"}},
	)
	if err != nil {
		return nil, err
	}
	out := make([]config.ScreenshotFile, 0, len(resp.Data))
	for _, r := range resp.Data {
		f := config.ScreenshotFile{Path: r.Attributes.FileName}
		if r.Attributes.SourceFileChecksum != "" {
			s := "checksum:" + r.Attributes.SourceFileChecksum
			f.Alt = &s
		}
		out = append(out, f)
	}
	return out, nil
}

// --- custom product pages --------------------------------------------------

func fetchCustomProductPages(ctx context.Context, c *asc.Client, appID string) (config.CustomProductPagesSpec, error) {
	q := url.Values{"limit": {"100"}}
	page, err := asc.Get[asc.Collection[asc.AppCustomProductPageAttributes]](
		ctx, c, "/v1/apps/"+appID+"/customProductPages", q,
	)
	if err != nil {
		return nil, fmt.Errorf("list customProductPages: %w", err)
	}
	if len(page.Data) == 0 {
		return nil, nil
	}
	out := config.CustomProductPagesSpec{}
	for _, r := range page.Data {
		cpp := config.CustomProductPage{}
		if r.Attributes.Visible != nil {
			v := *r.Attributes.Visible
			cpp.Visible = &v
		}
		if locs := fetchCPPLocalizations(ctx, c, r.ID); len(locs) > 0 {
			cpp.Localizations = locs
		}
		out[r.Attributes.Name] = cpp
	}
	return out, nil
}

// fetchCPPLocalizations walks page → version → localizations. Empty
// or missing intermediate steps return an empty map (CPPs may not
// have any version yet on first creation).
func fetchCPPLocalizations(ctx context.Context, c *asc.Client, pageID string) map[string]config.CustomProductPageLocale {
	versResp, err := asc.Get[asc.Collection[asc.AppCustomProductPageVersionAttributes]](
		ctx, c, "/v1/customProductPages/"+pageID+"/customProductPageVersions",
		url.Values{"limit": {"5"}},
	)
	if err != nil || len(versResp.Data) == 0 {
		return nil
	}
	verID := versResp.Data[0].ID
	locResp, err := asc.Get[asc.Collection[asc.AppCustomProductPageLocalizationAttributes]](
		ctx, c, "/v1/customProductPageVersions/"+verID+"/customProductPageLocalizations",
		url.Values{"limit": {"50"}},
	)
	if err != nil || len(locResp.Data) == 0 {
		return nil
	}
	out := map[string]config.CustomProductPageLocale{}
	for _, r := range locResp.Data {
		l := config.CustomProductPageLocale{}
		if r.Attributes.PromotionalText != "" {
			s := r.Attributes.PromotionalText
			l.PromotionalText = &s
		}
		out[r.Attributes.Locale] = l
	}
	return out
}

// silence the import sweep of std libs we use only when JSON-decoding
// arbitrary Apple shapes.
var (
	_ = json.Marshal
	_ = errors.New
)
