package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// Writes target the editable appInfo, never the live one: the live appInfo mutates already-shipping listings.

// Mirrors AppStoreVersionLocalization.attributes: only the fields Flightline writes.
type metadataASCVersionLocalizationAttrs struct {
	Locale          string `json:"locale,omitempty"`
	Description     string `json:"description,omitempty"`
	Keywords        string `json:"keywords,omitempty"`
	WhatsNew        string `json:"whatsNew,omitempty"`
	PromotionalText string `json:"promotionalText,omitempty"`
	MarketingURL    string `json:"marketingUrl,omitempty"`
	SupportURL      string `json:"supportUrl,omitempty"`
}

// Mirrors AppInfoLocalization.attributes: only the fields Flightline writes.
type metadataAppInfoLocalizationAttrs struct {
	Locale   string `json:"locale,omitempty"`
	Name     string `json:"name,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

// MetadataView is the joined per-locale snapshot of both localization resources; stable JSON contract.
type MetadataView struct {
	Locale                string `json:"locale"`
	Name                  string `json:"name,omitempty"`
	Subtitle              string `json:"subtitle,omitempty"`
	Description           string `json:"description,omitempty"`
	Keywords              string `json:"keywords,omitempty"`
	WhatsNew              string `json:"whatsNew,omitempty"`
	PromotionalText       string `json:"promotionalText,omitempty"`
	MarketingURL          string `json:"marketingUrl,omitempty"`
	SupportURL            string `json:"supportUrl,omitempty"`
	VersionLocalizationID string `json:"versionLocalizationId,omitempty"`
	AppInfoLocalizationID string `json:"appInfoLocalizationId,omitempty"`
}

// MetadataSetResult is the stable JSON envelope for `metadata set`; Action is "noop"|"version"|"app-info"|"both".
type MetadataSetResult struct {
	Action   string       `json:"action"`
	Changed  bool         `json:"changed"`
	Metadata MetadataView `json:"metadata"`
}

func (r *MetadataSetResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"CHANGED", boolString(r.Changed)},
		{"LOCALE", r.Metadata.Locale},
		{"NAME", r.Metadata.Name},
		{"SUBTITLE", r.Metadata.Subtitle},
		{"DESCRIPTION", truncateForTable(r.Metadata.Description, 80)},
		{"KEYWORDS", r.Metadata.Keywords},
		{"WHATS_NEW", truncateForTable(r.Metadata.WhatsNew, 80)},
		{"PROMOTIONAL_TEXT", truncateForTable(r.Metadata.PromotionalText, 80)},
		{"MARKETING_URL", r.Metadata.MarketingURL},
		{"SUPPORT_URL", r.Metadata.SupportURL},
	}
	return headers, rows
}

// JSON output stays full-fidelity; only the table view truncates. Param `n` avoids the max/cap/len builtins.
func truncateForTable(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

var metadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Manage App Store metadata localizations",
	Long: `metadata writes per-locale strings into appStoreVersionLocalizations
(description, keywords, whatsNew, promotionalText, marketing/support URLs)
and appInfoLocalizations (name, subtitle). Both resources are diff-then-PATCH
idempotent: re-running with the same arguments is a no-op.`,
}

var metadataSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set per-locale metadata fields (idempotent diff-then-PATCH)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runMetadataSet,
	Example: `  flightline metadata set com.example.myapp --version 1.0.1 --locale en-US --name "MyApp" --subtitle "Slogan"
  flightline metadata set com.example.myapp --version 1.0.1 --locale en-US --description "..." --keywords "..."
  flightline metadata set com.example.myapp --version 1.0.1 --locale en-US --whats-new "Bug fixes."
  flightline metadata set com.example.myapp --version 1.0.1 --locale en-US --output json`,
}

var (
	metadataSetVersion         string
	metadataSetPlatform        string
	metadataSetLocale          string
	metadataSetName            string
	metadataSetSubtitle        string
	metadataSetDescription     string
	metadataSetKeywords        string
	metadataSetWhatsNew        string
	metadataSetPromotionalText string
	metadataSetMarketingURL    string
	metadataSetSupportURL      string
)

func init() {
	metadataSetCmd.Flags().StringVar(&metadataSetVersion, "version", "", "App Store version string (e.g. 1.0.1)")
	metadataSetCmd.Flags().StringVar(&metadataSetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	metadataSetCmd.Flags().StringVar(&metadataSetLocale, "locale", "", "BCP-47 locale code (e.g. en-US)")
	metadataSetCmd.Flags().StringVar(&metadataSetName, "name", "", "app name (appInfoLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetSubtitle, "subtitle", "", "app subtitle (appInfoLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetDescription, "description", "", "app description (versionLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetKeywords, "keywords", "", "comma-separated keywords (versionLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetWhatsNew, "whats-new", "", "release notes (versionLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetPromotionalText, "promotional-text", "", "promotional text (versionLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetMarketingURL, "marketing-url", "", "marketing URL (versionLocalization)")
	metadataSetCmd.Flags().StringVar(&metadataSetSupportURL, "support-url", "", "support URL (versionLocalization)")
	_ = metadataSetCmd.MarkFlagRequired("version")
	_ = metadataSetCmd.MarkFlagRequired("locale")

	metadataCmd.AddCommand(metadataSetCmd)
	rootCmd.AddCommand(metadataCmd)
}

type metadataFlagSet struct {
	name, subtitle, description, keywords,
	whatsNew, promotionalText, marketingURL, supportURL bool
}

// Tracks only explicitly-set flags: Apple's PATCH treats empty-string as "clear", so unset flags must not be sent.
func readChangedFlags(cmd *cobra.Command) metadataFlagSet {
	return metadataFlagSet{
		name:            cmd.Flags().Changed("name"),
		subtitle:        cmd.Flags().Changed("subtitle"),
		description:     cmd.Flags().Changed("description"),
		keywords:        cmd.Flags().Changed("keywords"),
		whatsNew:        cmd.Flags().Changed("whats-new"),
		promotionalText: cmd.Flags().Changed("promotional-text"),
		marketingURL:    cmd.Flags().Changed("marketing-url"),
		supportURL:      cmd.Flags().Changed("support-url"),
	}
}

func (f metadataFlagSet) anyVersionFlag() bool {
	return f.description || f.keywords || f.whatsNew || f.promotionalText || f.marketingURL || f.supportURL
}

func (f metadataFlagSet) anyAppInfoFlag() bool {
	return f.name || f.subtitle
}

func (f metadataFlagSet) any() bool {
	return f.anyVersionFlag() || f.anyAppInfoFlag()
}

func runMetadataSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(metadataSetVersion)
	platform := strings.TrimSpace(metadataSetPlatform)
	locale := strings.TrimSpace(metadataSetLocale)
	if versionStr == "" {
		return errors.New("metadata: --version is required")
	}
	if locale == "" {
		return errors.New("metadata: --locale is required")
	}
	flags := readChangedFlags(cmd)
	if !flags.any() {
		return errors.New("metadata: no content flags set; pass at least one of --name --subtitle --description --keywords --whats-new --promotional-text --marketing-url --support-url")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	versionView, err := lookupVersion(cmd.Context(), c, appID, versionStr, platform)
	if err != nil {
		return err
	}
	if versionView == nil {
		return fmt.Errorf("metadata: no version %q found for %q (platform=%s)", versionStr, bundleID, platform)
	}

	view := MetadataView{Locale: locale}
	patchedVersion, err := applyVersionLocalizationDiff(cmd, c, versionView, locale, flags, &view)
	if err != nil {
		return err
	}
	patchedAppInfo, err := applyAppInfoLocalizationDiff(cmd, c, appID, versionView, locale, flags, &view)
	if err != nil {
		return err
	}

	result := &MetadataSetResult{
		Action:   computeAction(patchedVersion, patchedAppInfo),
		Changed:  patchedVersion || patchedAppInfo,
		Metadata: view,
	}
	return Render(result, outputMode())
}

// Returns patched=true iff a PATCH was issued; updates view in place with the post-state.
func applyVersionLocalizationDiff(
	cmd *cobra.Command,
	c *asc.Client,
	versionView *VersionView,
	locale string,
	flags metadataFlagSet,
	view *MetadataView,
) (bool, error) {
	if !flags.anyVersionFlag() {
		return false, nil
	}
	curID, curAttrs, err := getVersionLocalization(cmd.Context(), c, versionView.ID, locale)
	if err != nil {
		return false, err
	}
	if curID == "" {
		return false, fmt.Errorf("metadata: no appStoreVersionLocalization for locale %q under version %s; create it via the locale picker first", locale, versionView.Attributes.VersionString)
	}
	view.VersionLocalizationID = curID
	copyVersionLocAttrsIntoView(view, curAttrs)

	patch, anyChanged := diffVersionLocAttrs(flags, curAttrs,
		metadataSetDescription, metadataSetKeywords, metadataSetWhatsNew,
		metadataSetPromotionalText, metadataSetMarketingURL, metadataSetSupportURL)
	if !anyChanged {
		return false, nil
	}
	updated, err := patchVersionLocalization(cmd.Context(), c, curID, patch)
	if err != nil {
		return false, err
	}
	copyVersionLocAttrsIntoView(view, updated)
	return true, nil
}

func applyAppInfoLocalizationDiff(
	cmd *cobra.Command,
	c *asc.Client,
	appID string,
	versionView *VersionView,
	locale string,
	flags metadataFlagSet,
	view *MetadataView,
) (bool, error) {
	if !flags.anyAppInfoFlag() {
		return false, nil
	}
	appInfoID, err := pickAppInfoForVersion(cmd.Context(), c, appID, versionView.Attributes.AppVersionState)
	if err != nil {
		return false, err
	}
	curID, curAttrs, err := getAppInfoLocalization(cmd.Context(), c, appInfoID, locale)
	if err != nil {
		return false, err
	}
	if curID == "" {
		return false, fmt.Errorf("metadata: no appInfoLocalization for locale %q under appInfo %s; create it via the locale picker first", locale, appInfoID)
	}
	view.AppInfoLocalizationID = curID
	copyAppInfoLocAttrsIntoView(view, curAttrs)

	patch, anyChanged := diffAppInfoLocAttrs(flags, curAttrs, metadataSetName, metadataSetSubtitle)
	if !anyChanged {
		return false, nil
	}
	updated, err := patchAppInfoLocalization(cmd.Context(), c, curID, patch)
	if err != nil {
		return false, err
	}
	copyAppInfoLocAttrsIntoView(view, updated)
	return true, nil
}

func computeAction(patchedVersion, patchedAppInfo bool) string {
	switch {
	case patchedVersion && patchedAppInfo:
		return "both"
	case patchedVersion:
		return "version"
	case patchedAppInfo:
		return "app-info"
	default:
		return "noop"
	}
}

// Returns ("", zero, nil) when no localization exists for the locale: missing is not an error.
func getVersionLocalization(ctx context.Context, c *asc.Client, versionID, locale string) (string, metadataASCVersionLocalizationAttrs, error) {
	q := url.Values{
		"filter[locale]": {locale},
		"limit":          {"1"},
	}
	page, err := asc.Get[asc.Collection[metadataASCVersionLocalizationAttrs]](
		ctx, c, "/v1/appStoreVersions/"+url.PathEscape(versionID)+"/appStoreVersionLocalizations", q,
	)
	if err != nil {
		return "", metadataASCVersionLocalizationAttrs{}, err
	}
	if len(page.Data) == 0 {
		return "", metadataASCVersionLocalizationAttrs{}, nil
	}
	return page.Data[0].ID, page.Data[0].Attributes, nil
}

func getAppInfoLocalization(ctx context.Context, c *asc.Client, appInfoID, locale string) (string, metadataAppInfoLocalizationAttrs, error) {
	q := url.Values{
		"filter[locale]": {locale},
		"limit":          {"1"},
	}
	page, err := asc.Get[asc.Collection[metadataAppInfoLocalizationAttrs]](
		ctx, c, "/v1/appInfos/"+url.PathEscape(appInfoID)+"/appInfoLocalizations", q,
	)
	if err != nil {
		return "", metadataAppInfoLocalizationAttrs{}, err
	}
	if len(page.Data) == 0 {
		return "", metadataAppInfoLocalizationAttrs{}, nil
	}
	return page.Data[0].ID, page.Data[0].Attributes, nil
}

// Pointer-typed strings let omitempty drop unset fields so a partial PATCH never clears unrelated fields.
type versionLocalizationPatch struct {
	Data versionLocalizationPatchData `json:"data"`
}

type versionLocalizationPatchData struct {
	Type       string                             `json:"type"`
	ID         string                             `json:"id"`
	Attributes versionLocalizationPatchAttributes `json:"attributes,omitempty"`
}

type versionLocalizationPatchAttributes struct {
	Description     *string `json:"description,omitempty"`
	Keywords        *string `json:"keywords,omitempty"`
	WhatsNew        *string `json:"whatsNew,omitempty"`
	PromotionalText *string `json:"promotionalText,omitempty"`
	MarketingURL    *string `json:"marketingUrl,omitempty"`
	SupportURL      *string `json:"supportUrl,omitempty"`
}

// Emits only fields the user set AND that differ from the live record; anyChanged=false means skip the PATCH.
func diffVersionLocAttrs(
	flags metadataFlagSet,
	cur metadataASCVersionLocalizationAttrs,
	description, keywords, whatsNew, promotionalText, marketingURL, supportURL string,
) (versionLocalizationPatchAttributes, bool) {
	var out versionLocalizationPatchAttributes
	changed := false
	if flags.description && description != cur.Description {
		out.Description = strPtr(description)
		changed = true
	}
	if flags.keywords && keywords != cur.Keywords {
		out.Keywords = strPtr(keywords)
		changed = true
	}
	if flags.whatsNew && whatsNew != cur.WhatsNew {
		out.WhatsNew = strPtr(whatsNew)
		changed = true
	}
	if flags.promotionalText && promotionalText != cur.PromotionalText {
		out.PromotionalText = strPtr(promotionalText)
		changed = true
	}
	if flags.marketingURL && marketingURL != cur.MarketingURL {
		out.MarketingURL = strPtr(marketingURL)
		changed = true
	}
	if flags.supportURL && supportURL != cur.SupportURL {
		out.SupportURL = strPtr(supportURL)
		changed = true
	}
	return out, changed
}

func patchVersionLocalization(ctx context.Context, c *asc.Client, locID string, attrs versionLocalizationPatchAttributes) (metadataASCVersionLocalizationAttrs, error) {
	body := versionLocalizationPatch{
		Data: versionLocalizationPatchData{
			Type:       "appStoreVersionLocalizations",
			ID:         locID,
			Attributes: attrs,
		},
	}
	resp, err := asc.Patch[asc.Single[metadataASCVersionLocalizationAttrs]](
		ctx, c, "/v1/appStoreVersionLocalizations/"+url.PathEscape(locID), nil, body,
	)
	if err != nil {
		return metadataASCVersionLocalizationAttrs{}, err
	}
	return resp.Data.Attributes, nil
}

type appInfoLocalizationPatch struct {
	Data appInfoLocalizationPatchData `json:"data"`
}

type appInfoLocalizationPatchData struct {
	Type       string                             `json:"type"`
	ID         string                             `json:"id"`
	Attributes appInfoLocalizationPatchAttributes `json:"attributes,omitempty"`
}

type appInfoLocalizationPatchAttributes struct {
	Name     *string `json:"name,omitempty"`
	Subtitle *string `json:"subtitle,omitempty"`
}

func diffAppInfoLocAttrs(
	flags metadataFlagSet,
	cur metadataAppInfoLocalizationAttrs,
	name, subtitle string,
) (appInfoLocalizationPatchAttributes, bool) {
	var out appInfoLocalizationPatchAttributes
	changed := false
	if flags.name && name != cur.Name {
		out.Name = strPtr(name)
		changed = true
	}
	if flags.subtitle && subtitle != cur.Subtitle {
		out.Subtitle = strPtr(subtitle)
		changed = true
	}
	return out, changed
}

func patchAppInfoLocalization(ctx context.Context, c *asc.Client, locID string, attrs appInfoLocalizationPatchAttributes) (metadataAppInfoLocalizationAttrs, error) {
	body := appInfoLocalizationPatch{
		Data: appInfoLocalizationPatchData{
			Type:       "appInfoLocalizations",
			ID:         locID,
			Attributes: attrs,
		},
	}
	resp, err := asc.Patch[asc.Single[metadataAppInfoLocalizationAttrs]](
		ctx, c, "/v1/appInfoLocalizations/"+url.PathEscape(locID), nil, body,
	)
	if err != nil {
		return metadataAppInfoLocalizationAttrs{}, err
	}
	return resp.Data.Attributes, nil
}

func copyVersionLocAttrsIntoView(v *MetadataView, a metadataASCVersionLocalizationAttrs) {
	v.Description = a.Description
	v.Keywords = a.Keywords
	v.WhatsNew = a.WhatsNew
	v.PromotionalText = a.PromotionalText
	v.MarketingURL = a.MarketingURL
	v.SupportURL = a.SupportURL
}

func copyAppInfoLocAttrsIntoView(v *MetadataView, a metadataAppInfoLocalizationAttrs) {
	v.Name = a.Name
	v.Subtitle = a.Subtitle
}
