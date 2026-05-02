package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// metadata splits across two Apple resources:
//
//   - appStoreVersionLocalizations: description, keywords, whatsNew,
//     promotionalText, marketingUrl, supportUrl. Owned by the version.
//   - appInfoLocalizations: name, subtitle. Owned by the appInfo. The
//     "editable" appInfo for the version's lifecycle bucket is the one we
//     write to (matching the live appInfo would mutate already-shipping
//     listings).
//
// `metadata set` accepts the union of both flag sets and routes each field
// to its right resource. Idempotent: GET both localizations first, diff
// against the requested change, PATCH each resource only when its slice
// of fields actually differs. A bare `metadata set` with no field flags
// is a hard error — there is nothing to write — rather than a silent
// no-op masquerading as success.

// metadataASCVersionLocalizationAttrs mirrors Apple's
// AppStoreVersionLocalization.attributes — only the fields Skipper writes.
type metadataASCVersionLocalizationAttrs struct {
	Locale          string `json:"locale,omitempty"`
	Description     string `json:"description,omitempty"`
	Keywords        string `json:"keywords,omitempty"`
	WhatsNew        string `json:"whatsNew,omitempty"`
	PromotionalText string `json:"promotionalText,omitempty"`
	MarketingURL    string `json:"marketingUrl,omitempty"`
	SupportURL      string `json:"supportUrl,omitempty"`
}

// metadataAppInfoLocalizationAttrs mirrors Apple's
// AppInfoLocalization.attributes — only the fields Skipper writes.
type metadataAppInfoLocalizationAttrs struct {
	Locale   string `json:"locale,omitempty"`
	Name     string `json:"name,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

// MetadataView is the joined per-locale snapshot of both localization
// resources for a version. Stable JSON contract.
type MetadataView struct {
	Locale          string `json:"locale"`
	Name            string `json:"name,omitempty"`
	Subtitle        string `json:"subtitle,omitempty"`
	Description     string `json:"description,omitempty"`
	Keywords        string `json:"keywords,omitempty"`
	WhatsNew        string `json:"whatsNew,omitempty"`
	PromotionalText string `json:"promotionalText,omitempty"`
	MarketingURL    string `json:"marketingUrl,omitempty"`
	SupportURL      string `json:"supportUrl,omitempty"`
	// Resource IDs — useful for downstream tooling that wants to PATCH
	// the same localizations directly.
	VersionLocalizationID string `json:"versionLocalizationId,omitempty"`
	AppInfoLocalizationID string `json:"appInfoLocalizationId,omitempty"`
}

// MetadataSetResult is the JSON-stable envelope for `metadata set`.
//
// Action is one of "noop" | "version" | "app-info" | "both" — describes
// which resources received PATCHes. Changed is true iff at least one
// PATCH was issued. Metadata is the after-state view of the locale.
type MetadataSetResult struct {
	Action   string       `json:"action"`
	Changed  bool         `json:"changed"`
	Metadata MetadataView `json:"metadata"`
}

// TableRows for MetadataSetResult — vertical layout, one row per field.
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

// truncateForTable shortens long-form copy with a single-character ellipsis
// in the table view. JSON output stays full-fidelity — only the table cell
// is summarised. `n` names the byte-count cap; we avoid `max`/`cap`/`len`
// because each shadows a predeclared builtin in Go 1.21+.
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
idempotent — re-running with the same arguments is a no-op.`,
}

var metadataSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set per-locale metadata fields (idempotent diff-then-PATCH)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runMetadataSet,
	Example: `  skipper metadata set com.example.myapp --version 1.0.1 --locale en-US --name "MyApp" --subtitle "Slogan"
  skipper metadata set com.example.myapp --version 1.0.1 --locale en-US --description "..." --keywords "..."
  skipper metadata set com.example.myapp --version 1.0.1 --locale en-US --whats-new "Bug fixes."
  skipper metadata set com.example.myapp --version 1.0.1 --locale en-US --output json`,
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

// metadataFlagSet bundles the flags every subcommand wraps so the set of
// fields the user actually passed can be inspected uniformly.
type metadataFlagSet struct {
	name, subtitle, description, keywords,
	whatsNew, promotionalText, marketingURL, supportURL bool
}

// readChangedFlags returns a metadataFlagSet recording which content flags
// the user explicitly set. Used by the diff path so unset flags don't
// accidentally clear server-side fields (Apple's PATCH semantics on a
// nullable string treat empty-string as "clear").
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

// anyVersionFlag reports whether any flag belonging to the version
// localization bucket was set. Used to decide whether to GET that resource.
func (f metadataFlagSet) anyVersionFlag() bool {
	return f.description || f.keywords || f.whatsNew || f.promotionalText || f.marketingURL || f.supportURL
}

// anyAppInfoFlag reports whether any flag belonging to the app-info
// localization bucket was set.
func (f metadataFlagSet) anyAppInfoFlag() bool {
	return f.name || f.subtitle
}

// any reports whether at least one content flag was set.
func (f metadataFlagSet) any() bool {
	return f.anyVersionFlag() || f.anyAppInfoFlag()
}

func runMetadataSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(metadataSetVersion)
	platform := strings.TrimSpace(metadataSetPlatform)
	locale := strings.TrimSpace(metadataSetLocale)
	if versionStr == "" {
		return fmt.Errorf("metadata: --version is required")
	}
	if locale == "" {
		return fmt.Errorf("metadata: --locale is required")
	}
	flags := readChangedFlags(cmd)
	if !flags.any() {
		return fmt.Errorf("metadata: no content flags set; pass at least one of --name --subtitle --description --keywords --whats-new --promotional-text --marketing-url --support-url")
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

// applyVersionLocalizationDiff handles the version-localization slice of
// `metadata set`. Returns (patched, err): patched=true iff a PATCH was
// issued. No-op (false, nil) when no version-tier flags are set or when
// the diff produces no changes. Updates view in place with the post-state.
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

// applyAppInfoLocalizationDiff handles the app-info-localization slice of
// `metadata set`. Same shape as applyVersionLocalizationDiff.
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

// computeAction maps the (patchedVersion, patchedAppInfo) pair onto the
// stable "action" enum exposed in JSON output.
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

// getVersionLocalization fetches the appStoreVersionLocalization for the
// given version + locale. Returns ("", zero, nil) when no localization
// exists for the locale — same "missing-but-not-an-error" idiom as
// lookupVersion / lookupBuild.
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

// getAppInfoLocalization fetches the appInfoLocalization for the given
// appInfo + locale. Same "missing → zero values" idiom.
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

// versionLocalizationPatch is the wire body for a version-localization
// PATCH. Pointer-typed strings let omitempty drop unset fields so a bare
// `metadata set --description X` PATCH never accidentally clears unrelated
// fields.
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

// diffVersionLocAttrs builds a patch body containing only the fields the
// user explicitly set AND that differ from the live record. Returns
// (patchAttributes, anyChanged). When anyChanged=false the caller skips
// the PATCH entirely.
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

// patchVersionLocalization issues the PATCH and returns the post-state
// attributes so callers can render the after-image.
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

// appInfoLocalizationPatch is the wire body for an app-info-localization
// PATCH. Same pointer-omitempty idiom as the version variant.
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

// diffAppInfoLocAttrs builds a patch body for the app-info localization
// resource. Same shape as diffVersionLocAttrs.
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

// patchAppInfoLocalization issues the PATCH and returns the post-state
// attributes.
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

// copyVersionLocAttrsIntoView mirrors the live attrs into the joined view
// (callers populate from the current GET; if a PATCH lands they overwrite
// with the post-state).
func copyVersionLocAttrsIntoView(v *MetadataView, a metadataASCVersionLocalizationAttrs) {
	v.Description = a.Description
	v.Keywords = a.Keywords
	v.WhatsNew = a.WhatsNew
	v.PromotionalText = a.PromotionalText
	v.MarketingURL = a.MarketingURL
	v.SupportURL = a.SupportURL
}

// copyAppInfoLocAttrsIntoView mirrors app-info attrs into the joined view.
func copyAppInfoLocAttrsIntoView(v *MetadataView, a metadataAppInfoLocalizationAttrs) {
	v.Name = a.Name
	v.Subtitle = a.Subtitle
}
