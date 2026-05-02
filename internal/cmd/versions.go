package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// VersionView is one row of the versions list/get output. Embeds the wire
// attributes plus the ASC-side ID so JSON consumers don't have to reach into
// a nested envelope.
type VersionView struct {
	ID         string                `json:"id"`
	Type       string                `json:"type"`
	Attributes asc.VersionAttributes `json:"attributes"`
}

// VersionList is the table-aware view for `versions list`.
type VersionList struct {
	Versions []VersionView `json:"versions"`
}

// TableRows implements TableRenderable for the versions list view.
func (l VersionList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"VERSION", "PLATFORM", "STATE", "RELEASE_TYPE", "ID"}
	rows = make([][]string, 0, len(l.Versions))
	for i := range l.Versions {
		v := &l.Versions[i]
		rows = append(rows, []string{
			v.Attributes.VersionString,
			v.Attributes.Platform,
			versionDisplayState(v.Attributes),
			v.Attributes.ReleaseType,
			v.ID,
		})
	}
	return headers, rows
}

// TableRows for a single version. Vertical layout reads better for one record.
func (v *VersionView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", v.ID},
		{"TYPE", v.Type},
		{"VERSION", v.Attributes.VersionString},
		{"PLATFORM", v.Attributes.Platform},
		{"STATE", versionDisplayState(v.Attributes)},
		{"APP_STORE_STATE", v.Attributes.AppStoreState},
		{"APP_VERSION_STATE", v.Attributes.AppVersionState},
		{"RELEASE_TYPE", v.Attributes.ReleaseType},
		{"REVIEW_TYPE", v.Attributes.ReviewType},
		{"COPYRIGHT", v.Attributes.Copyright},
		{"EARLIEST_RELEASE_DATE", v.Attributes.EarliestReleaseDate},
		{"CREATED_DATE", v.Attributes.CreatedDate},
		{"DOWNLOADABLE", boolPtrStr(v.Attributes.Downloadable)},
	}
	return headers, rows
}

// versionDisplayState picks whichever state field Apple populated. Newer
// versions surface AppVersionState; older ones use the deprecated
// AppStoreState. We never see both populated simultaneously in practice.
func versionDisplayState(a asc.VersionAttributes) string {
	if a.AppVersionState != "" {
		return a.AppVersionState
	}
	return a.AppStoreState
}

func boolPtrStr(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

var versionsCmd = &cobra.Command{
	Use:   "versions",
	Short: "Manage and inspect App Store versions",
	Long:  `versions groups read and write commands over the /v1/appStoreVersions resource.`,
}

var versionsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List App Store versions for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runVersionsList,
	Example: `  skipper versions list com.example.myapp
  skipper versions list com.example.myapp --platform IOS
  skipper versions list com.example.myapp --output json | jq -r '.versions[].versionString'`,
}

var versionsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single App Store version by versionString",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runVersionsGet,
	Example: `  skipper versions get com.example.myapp --version 1.0.1
  skipper versions get com.example.myapp --version 1.0.1 --platform IOS --output json`,
}

var versionsCreateCmd = &cobra.Command{
	Use:          "create <bundleId>",
	Short:        "Create a new App Store version (idempotent: returns existing version if already present)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runVersionsCreate,
	Example: `  skipper versions create com.example.myapp --version 1.0.1
  skipper versions create com.example.myapp --version 1.0.1 --platform IOS --release-type MANUAL
  skipper versions create com.example.myapp --version 1.0.1 --copyright "(c) 2025 Example LLC"`,
}

var versionsUpdateCmd = &cobra.Command{
	Use:          "update <bundleId>",
	Short:        "Update an existing App Store version (idempotent: PATCH only fields that differ)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runVersionsUpdate,
	Example: `  skipper versions update com.example.myapp --version 1.0.1 --release-type AFTER_APPROVAL
  skipper versions update com.example.myapp --version 1.0.1 --copyright "(c) 2025 Example LLC"
  skipper versions update com.example.myapp --version 1.0.1 --earliest-release-date 2025-06-01T08:00:00-07:00`,
}

// Per-subcommand flag state. Separate variables so cobra default values don't
// collide across `list` (default empty = all platforms) and `get` (default
// IOS — the role-spec directive: --platform always defaults to IOS).
var (
	versionsListPlatform string
	versionsListLimit    int
	versionsGetVersion   string
	versionsGetPlatform  string

	// versionsCreate flags. ReleaseType / Copyright / ReviewType / EarliestReleaseDate
	// are optional in Apple's create body; we forward only flags the user
	// explicitly set (cmd.Flags().Changed) so partial create requests stay
	// minimal.
	versionsCreateVersion             string
	versionsCreatePlatform            string
	versionsCreateCopyright           string
	versionsCreateReleaseType         string
	versionsCreateReviewType          string
	versionsCreateEarliestReleaseDate string

	// versionsUpdate flags. Same forwarding rule — only Changed flags reach
	// the PATCH body.
	versionsUpdateVersion             string
	versionsUpdatePlatform            string
	versionsUpdateCopyright           string
	versionsUpdateReleaseType         string
	versionsUpdateReviewType          string
	versionsUpdateEarliestReleaseDate string
)

func init() {
	versionsListCmd.Flags().StringVar(&versionsListPlatform, "platform", "", "filter by platform (IOS|MAC_OS|TV_OS|VISION_OS); empty = all")
	versionsListCmd.Flags().IntVar(&versionsListLimit, "limit", 0, "max versions to emit (0 = no cap)")

	versionsGetCmd.Flags().StringVar(&versionsGetVersion, "version", "", "version string to fetch (e.g. 1.0.1)")
	versionsGetCmd.Flags().StringVar(&versionsGetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	_ = versionsGetCmd.MarkFlagRequired("version")

	versionsCreateCmd.Flags().StringVar(&versionsCreateVersion, "version", "", "version string (e.g. 1.0.1)")
	versionsCreateCmd.Flags().StringVar(&versionsCreatePlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	versionsCreateCmd.Flags().StringVar(&versionsCreateCopyright, "copyright", "", "copyright line (e.g. '(c) 2025 Example LLC')")
	versionsCreateCmd.Flags().StringVar(&versionsCreateReleaseType, "release-type", "", "release type (MANUAL|AFTER_APPROVAL|SCHEDULED)")
	versionsCreateCmd.Flags().StringVar(&versionsCreateReviewType, "review-type", "", "review type (APP_STORE|NOTARIZATION)")
	versionsCreateCmd.Flags().StringVar(&versionsCreateEarliestReleaseDate, "earliest-release-date", "", "earliest release date (RFC3339; only with --release-type SCHEDULED)")
	_ = versionsCreateCmd.MarkFlagRequired("version")

	versionsUpdateCmd.Flags().StringVar(&versionsUpdateVersion, "version", "", "version string of the version to update (e.g. 1.0.1)")
	versionsUpdateCmd.Flags().StringVar(&versionsUpdatePlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	versionsUpdateCmd.Flags().StringVar(&versionsUpdateCopyright, "copyright", "", "new copyright line")
	versionsUpdateCmd.Flags().StringVar(&versionsUpdateReleaseType, "release-type", "", "new release type (MANUAL|AFTER_APPROVAL|SCHEDULED)")
	versionsUpdateCmd.Flags().StringVar(&versionsUpdateReviewType, "review-type", "", "new review type (APP_STORE|NOTARIZATION)")
	versionsUpdateCmd.Flags().StringVar(&versionsUpdateEarliestReleaseDate, "earliest-release-date", "", "new earliest release date (RFC3339; only with --release-type SCHEDULED)")
	_ = versionsUpdateCmd.MarkFlagRequired("version")

	versionsCmd.AddCommand(versionsListCmd)
	versionsCmd.AddCommand(versionsGetCmd)
	versionsCmd.AddCommand(versionsCreateCmd)
	versionsCmd.AddCommand(versionsUpdateCmd)
	rootCmd.AddCommand(versionsCmd)
}

func runVersionsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	q := url.Values{"limit": {"200"}}
	if p := strings.TrimSpace(versionsListPlatform); p != "" {
		q.Set("filter[platform]", p)
	}

	views, err := collectVersions(cmd.Context(), c, "/v1/apps/"+appID+"/appStoreVersions", q, versionsListLimit)
	if err != nil {
		return err
	}
	return Render(VersionList{Versions: views}, outputMode())
}

func runVersionsGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(versionsGetVersion)
	platform := strings.TrimSpace(versionsGetPlatform)
	if versionStr == "" {
		return fmt.Errorf("versions: --version is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	q := url.Values{
		"filter[versionString]": {versionStr},
		"limit":                 {"1"},
	}
	if platform != "" {
		q.Set("filter[platform]", platform)
	}

	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		cmd.Context(), c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return err
	}
	if len(page.Data) == 0 {
		return fmt.Errorf("versions: no version %q found for %q (platform=%s)", versionStr, bundleID, platform)
	}

	view := &VersionView{
		ID:         page.Data[0].ID,
		Type:       page.Data[0].Type,
		Attributes: page.Data[0].Attributes,
	}
	return Render(view, outputMode())
}

// resolveAppID resolves a bundleId to its ASC app ID. The same filter pattern
// `apps get` uses; centralized here so other commands can reuse without
// reaching into `apps.go`.
//
// Returns a typed error message that names the bundleId so users see what
// went missing.
func resolveAppID(ctx context.Context, c *asc.Client, bundleID string) (string, error) {
	q := url.Values{
		"filter[bundleId]": {bundleID},
		"limit":            {"1"},
	}
	page, err := asc.Get[asc.Collection[AppAttributes]](ctx, c, "/v1/apps", q)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("apps: no app found with bundleId %q", bundleID)
	}
	return page.Data[0].ID, nil
}

// collectVersions walks the paging iterator and returns flattened VersionView
// rows. limit 0 means "no cap" — return everything Apple paginates through.
func collectVersions(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]VersionView, error) {
	out := make([]VersionView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, VersionView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// defaultListCap is shared across every list command. Mirrors defaultAppCap
// in apps.go but lives here because it'll be reused by builds /
// review-submissions as well — keeping it in apps.go would require either
// re-importing or a circular dependency.
//
// Using `defaultListCap` rather than overloading `defaultAppCap`:
// the apps.go function is named for the resource it serves; renaming would
// breach file ownership. New name, same shape.
func defaultListCap(limit int) int {
	if limit > 0 {
		return limit
	}
	return 32
}

// VersionWriteResult is the JSON-stable envelope every write verb returns.
// `Changed=false` means the existing state already matched the request and
// no PATCH/POST was issued. `Action` is one of "created" | "updated" |
// "noop" — stable contract.
type VersionWriteResult struct {
	Action  string      `json:"action"`
	Changed bool        `json:"changed"`
	Version VersionView `json:"version"`
}

// TableRows for a write result. One vertical attribute table plus a leading
// "ACTION" row so users see the no-op / mutation outcome at a glance.
func (r *VersionWriteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"CHANGED", boolString(r.Changed)},
		{"ID", r.Version.ID},
		{"VERSION", r.Version.Attributes.VersionString},
		{"PLATFORM", r.Version.Attributes.Platform},
		{"STATE", versionDisplayState(r.Version.Attributes)},
		{"RELEASE_TYPE", r.Version.Attributes.ReleaseType},
		{"REVIEW_TYPE", r.Version.Attributes.ReviewType},
		{"COPYRIGHT", r.Version.Attributes.Copyright},
		{"EARLIEST_RELEASE_DATE", r.Version.Attributes.EarliestReleaseDate},
	}
	return headers, rows
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// versionWriteEnvelope mirrors the create / update body shape Apple's API
// requires. Pointer-typed optional fields so we can omit them when the user
// didn't pass the flag (idempotent write contract).
type versionWriteEnvelope struct {
	Data versionWriteData `json:"data"`
}

type versionWriteData struct {
	Type          string                 `json:"type"`
	ID            string                 `json:"id,omitempty"`
	Attributes    versionWriteAttributes `json:"attributes,omitempty"`
	Relationships *versionWriteRels      `json:"relationships,omitempty"`
}

type versionWriteAttributes struct {
	Platform            *string `json:"platform,omitempty"`
	VersionString       *string `json:"versionString,omitempty"`
	Copyright           *string `json:"copyright,omitempty"`
	ReviewType          *string `json:"reviewType,omitempty"`
	ReleaseType         *string `json:"releaseType,omitempty"`
	EarliestReleaseDate *string `json:"earliestReleaseDate,omitempty"`
}

type versionWriteRels struct {
	App *versionWriteRel `json:"app,omitempty"`
}

type versionWriteRel struct {
	Data versionWriteRelRef `json:"data"`
}

type versionWriteRelRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// runVersionsCreate POSTs a new appStoreVersion. Idempotent: if a version
// with the same versionString + platform already exists, returns the existing
// record with action="noop" rather than letting Apple's API surface a 409.
//
// Flag set covers the common ASC create dance: --version, --platform,
// --copyright, --release-type, --review-type, --earliest-release-date.
// Only flags the user explicitly set reach the wire body.
func runVersionsCreate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(versionsCreateVersion)
	platform := strings.TrimSpace(versionsCreatePlatform)
	if versionStr == "" {
		return fmt.Errorf("versions: --version is required")
	}
	if platform == "" {
		return fmt.Errorf("versions: --platform is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Idempotency probe: does the version already exist?
	existing, err := lookupVersion(cmd.Context(), c, appID, versionStr, platform)
	if err != nil {
		return err
	}
	if existing != nil {
		// Even on a "noop" the user may have passed flags — diff against the
		// existing record and update if anything differs. Mirrors the
		// `versions update` happy-path.
		return reconcileVersionUpdate(cmd, c, *existing,
			versionsCreateCopyright,
			versionsCreateReleaseType,
			versionsCreateReviewType,
			versionsCreateEarliestReleaseDate,
		)
	}

	body := versionWriteEnvelope{
		Data: versionWriteData{
			Type: "appStoreVersions",
			Attributes: versionWriteAttributes{
				Platform:      strPtr(platform),
				VersionString: strPtr(versionStr),
			},
			Relationships: &versionWriteRels{
				App: &versionWriteRel{
					Data: versionWriteRelRef{Type: "apps", ID: appID},
				},
			},
		},
	}
	if cmd.Flags().Changed("copyright") {
		body.Data.Attributes.Copyright = strPtr(versionsCreateCopyright)
	}
	if cmd.Flags().Changed("release-type") {
		body.Data.Attributes.ReleaseType = strPtr(versionsCreateReleaseType)
	}
	if cmd.Flags().Changed("review-type") {
		body.Data.Attributes.ReviewType = strPtr(versionsCreateReviewType)
	}
	if cmd.Flags().Changed("earliest-release-date") {
		body.Data.Attributes.EarliestReleaseDate = strPtr(versionsCreateEarliestReleaseDate)
	}

	resp, err := asc.Post[asc.Single[asc.VersionAttributes]](
		cmd.Context(), c, "/v1/appStoreVersions", nil, body,
	)
	if err != nil {
		return err
	}
	view := VersionView{
		ID:         resp.Data.ID,
		Type:       resp.Data.Type,
		Attributes: resp.Data.Attributes,
	}
	return Render(&VersionWriteResult{Action: "created", Changed: true, Version: view}, outputMode())
}

// runVersionsUpdate PATCHes an existing appStoreVersion. Idempotent: diffs
// against the current state and skips the PATCH entirely if no field would
// change. The version itself is identified by --version + --platform; the
// resource ID is resolved server-side.
func runVersionsUpdate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(versionsUpdateVersion)
	platform := strings.TrimSpace(versionsUpdatePlatform)
	if versionStr == "" {
		return fmt.Errorf("versions: --version is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	existing, err := lookupVersion(cmd.Context(), c, appID, versionStr, platform)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("versions: no version %q found for %q (platform=%s)", versionStr, bundleID, platform)
	}
	return reconcileVersionUpdate(cmd, c, *existing,
		versionsUpdateCopyright,
		versionsUpdateReleaseType,
		versionsUpdateReviewType,
		versionsUpdateEarliestReleaseDate,
	)
}

// reconcileVersionUpdate is the shared idempotent-PATCH path used by both
// `versions update` and the warm-path of `versions create`. Diffs the
// caller's intent against the live resource and:
//
//   - If no field would change, returns action="noop" with the existing view.
//   - Otherwise PATCHes only the differing fields and returns action="updated".
//
// The flag-Changed gate is read from cmd.Flags() so we ignore zero-value
// strings that the user never explicitly set.
func reconcileVersionUpdate(
	cmd *cobra.Command,
	c *asc.Client,
	existing VersionView,
	copyright, releaseType, reviewType, earliestReleaseDate string,
) error {
	attrs, changed := diffVersionAttrs(cmd, existing.Attributes, copyright, releaseType, reviewType, earliestReleaseDate)
	if !changed {
		return Render(&VersionWriteResult{Action: "noop", Changed: false, Version: existing}, outputMode())
	}
	body := versionWriteEnvelope{
		Data: versionWriteData{
			Type:       "appStoreVersions",
			ID:         existing.ID,
			Attributes: attrs,
		},
	}
	resp, err := asc.Patch[asc.Single[asc.VersionAttributes]](
		cmd.Context(), c, "/v1/appStoreVersions/"+url.PathEscape(existing.ID), nil, body,
	)
	if err != nil {
		return err
	}
	view := VersionView{
		ID:         resp.Data.ID,
		Type:       resp.Data.Type,
		Attributes: resp.Data.Attributes,
	}
	return Render(&VersionWriteResult{Action: "updated", Changed: true, Version: view}, outputMode())
}

// diffVersionAttrs builds a versionWriteAttributes containing only the fields
// the user explicitly set AND that differ from the existing resource. Returns
// (attrs, changed). When changed=false the caller skips the PATCH entirely.
func diffVersionAttrs(
	cmd *cobra.Command,
	cur asc.VersionAttributes,
	copyright, releaseType, reviewType, earliestReleaseDate string,
) (versionWriteAttributes, bool) {
	var out versionWriteAttributes
	changed := false
	if cmd.Flags().Changed("copyright") && copyright != cur.Copyright {
		out.Copyright = strPtr(copyright)
		changed = true
	}
	if cmd.Flags().Changed("release-type") && releaseType != cur.ReleaseType {
		out.ReleaseType = strPtr(releaseType)
		changed = true
	}
	if cmd.Flags().Changed("review-type") && reviewType != cur.ReviewType {
		out.ReviewType = strPtr(reviewType)
		changed = true
	}
	if cmd.Flags().Changed("earliest-release-date") && earliestReleaseDate != cur.EarliestReleaseDate {
		out.EarliestReleaseDate = strPtr(earliestReleaseDate)
		changed = true
	}
	return out, changed
}

// lookupVersion is the idempotency probe shared by create + update. Returns
// (nil, nil) when the version isn't found — callers branch on that as the
// "doesn't exist yet" signal rather than treating it as an error.
func lookupVersion(ctx context.Context, c *asc.Client, appID, versionStr, platform string) (*VersionView, error) {
	q := url.Values{
		"filter[versionString]": {versionStr},
		"limit":                 {"1"},
	}
	if platform != "" {
		q.Set("filter[platform]", platform)
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return nil, err
	}
	if len(page.Data) == 0 {
		return nil, nil
	}
	return &VersionView{
		ID:         page.Data[0].ID,
		Type:       page.Data[0].Type,
		Attributes: page.Data[0].Attributes,
	}, nil
}

// strPtr is the little helper that makes the Changed-only optional-field
// pattern readable. Pointer-to-string lets us distinguish "field not sent"
// (nil, omitempty drops it) from "field set to empty string" (pointer to "").
func strPtr(s string) *string { return &s }
