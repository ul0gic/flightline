package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func fetchBuildNumber(ctx context.Context, c *asc.Client, buildID string) (string, error) {
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](ctx, c, "/v1/builds/"+buildID, nil)
	if err != nil {
		return "", err
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("build %s response missing resource id", buildID)
	}
	return resp.Data.Attributes.Version, nil
}

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

	if appInfoID != "" {
		appLocs, err := listAppInfoLocalizations(ctx, c, appInfoID)
		if err != nil {
			return nil, err
		}
		for _, attrs := range appLocs {
			ml := out.Locales[attrs.Locale]
			copyAppInfoLocAttrsToSchema(&ml, attrs)
			out.Locales[attrs.Locale] = ml
		}
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
	var out []versionLocAttrs
	for page, err := range asc.Pages[versionLocAttrs](ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations", q) {
		if err != nil {
			return nil, fmt.Errorf("list version localizations: %w", err)
		}
		for _, r := range page.Data {
			out = append(out, r.Attributes)
		}
	}
	return out, nil
}

func listAppInfoLocalizations(ctx context.Context, c *asc.Client, appInfoID string) ([]appInfoLocAttrs, error) {
	q := url.Values{"limit": {"50"}}
	var out []appInfoLocAttrs
	for page, err := range asc.Pages[appInfoLocAttrs](ctx, c, "/v1/appInfos/"+appInfoID+"/appInfoLocalizations", q) {
		if err != nil {
			return nil, fmt.Errorf("list appInfo localizations: %w", err)
		}
		for _, r := range page.Data {
			out = append(out, r.Attributes)
		}
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

type categoryRelationshipsResp struct {
	Data *struct {
		ID string `json:"id"`
	} `json:"data,omitempty"`
}

func fetchCategories(ctx context.Context, c *asc.Client, appInfoID string) (*config.CategoriesSpec, error) {
	out := &config.CategoriesSpec{}
	id, err := getCategoryRelationship(ctx, c, appInfoID, "primaryCategory")
	if err != nil {
		return nil, err
	}
	if id != "" {
		s := id
		out.Primary = &s
	}
	id, err = getCategoryRelationship(ctx, c, appInfoID, "secondaryCategory")
	if err != nil {
		return nil, err
	}
	if id != "" {
		s := id
		out.Secondary = &s
	}
	for _, rel := range []string{"primarySubcategoryOne", "primarySubcategoryTwo"} {
		id, err := getCategoryRelationship(ctx, c, appInfoID, rel)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out.PrimarySubcategories = append(out.PrimarySubcategories, id)
		}
	}
	for _, rel := range []string{"secondarySubcategoryOne", "secondarySubcategoryTwo"} {
		id, err := getCategoryRelationship(ctx, c, appInfoID, rel)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out.SecondarySubcategories = append(out.SecondarySubcategories, id)
		}
	}
	if out.Primary == nil && out.Secondary == nil &&
		len(out.PrimarySubcategories) == 0 && len(out.SecondarySubcategories) == 0 {
		return nil, nil
	}
	return out, nil
}

// getCategoryRelationship returns the linked category ID, or "" when unset or 404.
// Unexpected errors and malformed non-null relationships fail the snapshot.
func getCategoryRelationship(ctx context.Context, c *asc.Client, appInfoID, rel string) (string, error) {
	resp, err := asc.Get[categoryRelationshipsResp](
		ctx, c, "/v1/appInfos/"+appInfoID+"/relationships/"+rel, nil,
	)
	if err != nil {
		if optionalMissing(err) {
			return "", nil
		}
		return "", fmt.Errorf("category relationship %s: %w", rel, err)
	}
	if resp.Data == nil {
		return "", nil
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("category relationship %s: resource missing id", rel)
	}
	return resp.Data.ID, nil
}

func fetchReviewerDemo(ctx context.Context, c *asc.Client, versionID string) (*config.ReviewerDemoSpec, error) {
	resp, err := asc.Get[struct {
		Data *asc.Resource[reviewerDemoAttrs] `json:"data"`
	}](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreReviewDetail", nil,
	)
	if err != nil {
		if optionalMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if resp.Data == nil {
		return nil, nil
	}
	if resp.Data.ID == "" {
		return nil, errors.New("review detail response missing resource id")
	}
	return projectReviewerDemo(resp.Data.Attributes), nil
}

func projectReviewerDemo(a reviewerDemoAttrs) *config.ReviewerDemoSpec {
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
	// Apple stores first+last separately; schema's contactName is "First Last" on the way out.
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
	return out // password is never round-tripped; apply's resolvePassword re-reads it
}

func fetchIAPs(ctx context.Context, c *asc.Client, appID string) (*config.IAPSpec, error) {
	q := url.Values{"limit": {"100"}}
	out := &config.IAPSpec{Products: map[string]config.IAPProduct{}}
	for page, err := range asc.Pages[asc.IAPAttributes](ctx, c, "/v1/apps/"+appID+"/inAppPurchasesV2", q) {
		if err != nil {
			return nil, fmt.Errorf("list IAPs: %w", err)
		}
		for _, r := range page.Data {
			prod, err := fetchIAPProduct(ctx, c, r)
			if err != nil {
				return nil, err
			}
			out.Products[r.Attributes.ProductID] = prod
		}
	}
	if len(out.Products) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchIAPProduct(ctx context.Context, c *asc.Client, r asc.Resource[asc.IAPAttributes]) (config.IAPProduct, error) {
	prod := config.IAPProduct{Type: r.Attributes.InAppPurchaseType}
	if r.Attributes.Name != "" {
		prod.Name = optStr(r.Attributes.Name)
	}
	prod.FamilySharable = copyBool(r.Attributes.FamilySharable)
	if r.Attributes.ContentHosting != nil {
		// Apple *bool → schema enum "HOSTED"/"NON_HOSTED".
		hosting := "NON_HOSTED"
		if *r.Attributes.ContentHosting {
			hosting = "HOSTED"
		}
		prod.ContentHosting = &hosting
	}
	prod.ReviewNote = optStr(r.Attributes.ReviewNote)
	screenshot, err := fetchIAPReviewScreenshotProjection(ctx, c, r.ID)
	if err != nil {
		return config.IAPProduct{}, fmt.Errorf("IAP %s screenshot: %w", r.Attributes.ProductID, err)
	}
	prod.ReviewScreenshot = screenshot
	locs, err := fetchIAPLocalizations(ctx, c, r.ID)
	if err != nil {
		return config.IAPProduct{}, fmt.Errorf("IAP %s localizations: %w", r.Attributes.ProductID, err)
	}
	if len(locs) > 0 {
		prod.Localizations = locs
	}
	commerce, err := FetchIAPCommerce(ctx, c, r.ID)
	if err != nil {
		return config.IAPProduct{}, fmt.Errorf("IAP %s commerce: %w", r.Attributes.ProductID, err)
	}
	prod.Commerce = commerce
	return prod, nil
}

func fetchIAPReviewScreenshotProjection(ctx context.Context, c *asc.Client, iapID string) (*config.IAPReviewScreenshot, error) {
	resp, err := asc.Get[struct {
		Data *asc.Resource[asc.IAPReviewScreenshotAttributes] `json:"data"`
	}](
		ctx, c, "/v2/inAppPurchases/"+iapID+"/appStoreReviewScreenshot", nil,
	)
	if err != nil {
		if optionalMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if resp.Data == nil {
		return nil, nil
	}
	if resp.Data.ID == "" {
		return nil, errors.New("IAP review screenshot response missing resource id")
	}
	return &config.IAPReviewScreenshot{
		Path: resp.Data.Attributes.FileName, SourceFileChecksum: resp.Data.Attributes.SourceFileChecksum,
	}, nil
}

func fetchIAPLocalizations(ctx context.Context, c *asc.Client, iapID string) (map[string]config.IAPLocalization, error) {
	q := url.Values{"limit": {"50"}}
	out := map[string]config.IAPLocalization{}
	for page, err := range asc.Pages[asc.IAPLocalizationAttributes](ctx, c, "/v2/inAppPurchases/"+iapID+"/inAppPurchaseLocalizations", q) {
		if err != nil {
			return nil, err
		}
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
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchTestFlightGroups(ctx context.Context, c *asc.Client, appID string) (*config.TestFlightSpec, error) {
	return fetchTestFlightGroupsWithOptions(ctx, c, appID, FetchOpts{})
}

func fetchTestFlightGroupsWithOptions(ctx context.Context, c *asc.Client, appID string, opts FetchOpts) (*config.TestFlightSpec, error) {
	q := url.Values{"limit": {"100"}}
	out := &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{}}
	for page, err := range asc.Pages[asc.BetaGroupAttributes](ctx, c, "/v1/apps/"+appID+"/betaGroups", q) {
		if err != nil {
			return nil, fmt.Errorf("list betaGroups: %w", err)
		}
		for _, r := range page.Data {
			g, err := fetchTestFlightGroup(ctx, c, r, opts)
			if err != nil {
				return nil, err
			}
			out.Groups[r.Attributes.Name] = g
		}
	}
	if len(out.Groups) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchGroupTesters(ctx context.Context, c *asc.Client, groupID string) ([]config.TestFlightTester, error) {
	q := url.Values{"limit": {"200"}}
	out := make([]config.TestFlightTester, 0)
	for page, err := range asc.Pages[asc.BetaTesterAttributes](ctx, c, "/v1/betaGroups/"+groupID+"/betaTesters", q) {
		if err != nil {
			return nil, err
		}
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
	}
	return out, nil
}

// fetchScreenshots projects locale → device → []file. Path is left blank: Apple
// stores rendered URLs, not source paths: so the diff engine flags local-vs-live mismatches.
func fetchScreenshots(ctx context.Context, c *asc.Client, versionID string) (*config.ScreenshotsSpec, error) {
	out := &config.ScreenshotsSpec{Locales: map[string]map[string][]config.ScreenshotFile{}}
	for locPage, err := range asc.Pages[versionLocAttrs](ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations", url.Values{"limit": {"50"}}) {
		if err != nil {
			return nil, fmt.Errorf("list screenshot locale rows: %w", err)
		}
		for _, locRow := range locPage.Data {
			devices, err := fetchScreenshotDevices(ctx, c, locRow.ID)
			if err != nil {
				return nil, fmt.Errorf("locale %s screenshot sets: %w", locRow.Attributes.Locale, err)
			}
			if len(devices) > 0 {
				out.Locales[locRow.Attributes.Locale] = devices
			}
		}
	}
	if len(out.Locales) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchScreenshotDevices(ctx context.Context, c *asc.Client, localizationID string) (map[string][]config.ScreenshotFile, error) {
	devices := map[string][]config.ScreenshotFile{}
	for setPage, err := range asc.Pages[screenshotSetAttrs](ctx, c, "/v1/appStoreVersionLocalizations/"+localizationID+"/appScreenshotSets", url.Values{"limit": {"50"}}) {
		if err != nil {
			return nil, err
		}
		for _, set := range setPage.Data {
			files, err := fetchScreenshotsInSet(ctx, c, set.ID)
			if err != nil {
				return nil, fmt.Errorf("screenshot set %s: %w", set.ID, err)
			}
			if len(files) > 0 {
				devices[set.Attributes.ScreenshotDisplayType] = files
			}
		}
	}
	return devices, nil
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
	var out []config.ScreenshotFile
	for page, err := range asc.Pages[screenshotAttrs](ctx, c, "/v1/appScreenshotSets/"+setID+"/appScreenshots", url.Values{"limit": {"20"}}) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			f := config.ScreenshotFile{
				Path: r.Attributes.FileName, SourceFileChecksum: r.Attributes.SourceFileChecksum,
			}
			if r.Attributes.SourceFileChecksum != "" {
				s := "checksum:" + r.Attributes.SourceFileChecksum
				f.Alt = &s
			}
			out = append(out, f)
		}
	}
	return out, nil
}

func fetchCustomProductPages(ctx context.Context, c *asc.Client, appID string) (config.CustomProductPagesSpec, error) {
	q := url.Values{"limit": {"100"}}
	out := config.CustomProductPagesSpec{}
	for page, err := range asc.Pages[asc.AppCustomProductPageAttributes](ctx, c, "/v1/apps/"+appID+"/appCustomProductPages", q) {
		if err != nil {
			return nil, fmt.Errorf("list customProductPages: %w", err)
		}
		for _, r := range page.Data {
			cpp := config.CustomProductPage{}
			if r.Attributes.Visible != nil {
				v := *r.Attributes.Visible
				cpp.Visible = &v
			}
			locs, err := fetchCPPLocalizations(ctx, c, r.ID)
			if err != nil {
				return nil, fmt.Errorf("custom product page %s: %w", r.Attributes.Name, err)
			}
			if len(locs) > 0 {
				cpp.Localizations = locs
			}
			out[r.Attributes.Name] = cpp
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// fetchCPPLocalizations walks page → version → localizations; returns nil when
// no version exists yet (CPPs may not have a version on first creation).
func fetchCPPLocalizations(ctx context.Context, c *asc.Client, pageID string) (map[string]config.CustomProductPageLocale, error) {
	var versions []asc.Resource[asc.AppCustomProductPageVersionAttributes]
	for page, err := range asc.Pages[asc.AppCustomProductPageVersionAttributes](ctx, c, "/v1/appCustomProductPages/"+pageID+"/appCustomProductPageVersions", url.Values{"limit": {"50"}}) {
		if err != nil {
			return nil, fmt.Errorf("list versions: %w", err)
		}
		versions = append(versions, page.Data...)
	}
	if len(versions) == 0 {
		return nil, nil
	}
	verID, err := latestCPPVersionID(versions)
	if err != nil {
		return nil, err
	}
	out := map[string]config.CustomProductPageLocale{}
	for page, err := range asc.Pages[asc.AppCustomProductPageLocalizationAttributes](ctx, c, "/v1/appCustomProductPageVersions/"+verID+"/appCustomProductPageLocalizations", url.Values{"limit": {"50"}}) {
		if err != nil {
			return nil, fmt.Errorf("version %s localizations: %w", verID, err)
		}
		for _, r := range page.Data {
			l := config.CustomProductPageLocale{}
			if r.Attributes.PromotionalText != "" {
				s := r.Attributes.PromotionalText
				l.PromotionalText = &s
			}
			shots, err := fetchCPPScreenshots(ctx, c, r.ID)
			if err != nil {
				return nil, fmt.Errorf("localization %s screenshots: %w", r.ID, err)
			}
			l.Screenshots = shots
			previews, err := FetchCPPPreviews(ctx, c, r.ID)
			if err != nil {
				return nil, fmt.Errorf("localization %s previews: %w", r.ID, err)
			}
			l.Previews = previews
			out[r.Attributes.Locale] = l
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchCPPScreenshots(ctx context.Context, c *asc.Client, localizationID string) (map[string][]config.ScreenshotFile, error) {
	out := map[string][]config.ScreenshotFile{}
	for page, err := range asc.Pages[screenshotSetAttrs](ctx, c, "/v1/appCustomProductPageLocalizations/"+localizationID+"/appScreenshotSets", url.Values{"limit": {"50"}}) {
		if err != nil {
			return nil, fmt.Errorf("list screenshot sets: %w", err)
		}
		for _, set := range page.Data {
			files, err := fetchScreenshotsInSet(ctx, c, set.ID)
			if err != nil {
				return nil, fmt.Errorf("screenshot set %s: %w", set.ID, err)
			}
			if len(files) > 0 {
				out[set.Attributes.ScreenshotDisplayType] = files
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchTestFlightGroup(ctx context.Context, c *asc.Client, r asc.Resource[asc.BetaGroupAttributes], opts FetchOpts) (config.TestFlightGroup, error) {
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
	testers, err := fetchGroupTesters(ctx, c, r.ID)
	if err != nil {
		return config.TestFlightGroup{}, fmt.Errorf("beta group %s testers: %w", r.Attributes.Name, err)
	}
	g.Testers = testers
	if wantsBetaGroupBuilds(opts, r.Attributes.Name) {
		builds, err := FetchBetaGroupBuilds(ctx, c, r.ID)
		if err != nil {
			return config.TestFlightGroup{}, fmt.Errorf("beta group %s builds: %w", r.Attributes.Name, err)
		}
		g.Builds = &builds
	}
	return g, nil
}
