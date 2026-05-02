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
	Long:  `versions groups read commands over the /v1/appStoreVersions resource.`,
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

// Per-subcommand flag state. Separate variables so cobra default values don't
// collide across `list` (default empty = all platforms) and `get` (default
// IOS — the role-spec directive: --platform always defaults to IOS).
var (
	versionsListPlatform string
	versionsListLimit    int
	versionsGetVersion   string
	versionsGetPlatform  string
)

func init() {
	versionsListCmd.Flags().StringVar(&versionsListPlatform, "platform", "", "filter by platform (IOS|MAC_OS|TV_OS|VISION_OS); empty = all")
	versionsListCmd.Flags().IntVar(&versionsListLimit, "limit", 0, "max versions to emit (0 = no cap)")

	versionsGetCmd.Flags().StringVar(&versionsGetVersion, "version", "", "version string to fetch (e.g. 1.0.1)")
	versionsGetCmd.Flags().StringVar(&versionsGetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	_ = versionsGetCmd.MarkFlagRequired("version")

	versionsCmd.AddCommand(versionsListCmd)
	versionsCmd.AddCommand(versionsGetCmd)
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
