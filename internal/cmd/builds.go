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

type BuildView struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`
	Attributes asc.BuildAttributes `json:"attributes"`
}

type BuildList struct {
	Builds []BuildView `json:"builds"`
}

func (l BuildList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"BUILD", "STATE", "EXPIRED", "UPLOADED", "ID"}
	rows = make([][]string, 0, len(l.Builds))
	for i := range l.Builds {
		b := &l.Builds[i]
		rows = append(rows, []string{
			b.Attributes.Version,
			b.Attributes.ProcessingState,
			expiredCell(b.Attributes),
			b.Attributes.UploadedDate,
			b.ID,
		})
	}
	return headers, rows
}

func (b *BuildView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", b.ID},
		{"TYPE", b.Type},
		{"BUILD", b.Attributes.Version},
		{"PROCESSING_STATE", b.Attributes.ProcessingState},
		{"EXPIRED", expiredCell(b.Attributes)},
		{"EXPIRATION_DATE", b.Attributes.ExpirationDate},
		{"UPLOADED_DATE", b.Attributes.UploadedDate},
		{"MIN_OS_VERSION", b.Attributes.MinOsVersion},
		{"USES_NON_EXEMPT_ENCRYPTION", boolPtrStr(b.Attributes.UsesNonExemptEncryption)},
		{"BUILD_AUDIENCE_TYPE", b.Attributes.BuildAudienceType},
	}
	return headers, rows
}

// expiredCell flags expiry loudly: expired builds can't ship to TestFlight
// or attach to a version submission.
func expiredCell(a asc.BuildAttributes) string {
	if a.Expired == nil {
		return ""
	}
	if *a.Expired {
		return "EXPIRED"
	}
	return "active"
}

var buildsCmd = &cobra.Command{
	Use:   "builds",
	Short: "Manage and inspect builds",
	Long:  `builds groups read commands over the /v1/builds resource.`,
}

var buildsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List builds for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBuildsList,
	Example: `  flightline builds list com.example.myapp
  flightline builds list com.example.myapp --limit 20
  flightline builds list com.example.myapp --output json | jq -r '.builds[].version'`,
}

var buildsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single build by build number (CFBundleVersion)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBuildsGet,
	Example: `  flightline builds get com.example.myapp --build 42
  flightline builds get com.example.myapp --build 42 --output json | jq .attributes.processingState`,
}

var buildsAttachCmd = &cobra.Command{
	Use:          "attach <bundleId>",
	Short:        "Attach a build to an App Store version (idempotent: skip if already attached)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBuildsAttach,
	Example: `  flightline builds attach com.example.myapp --version 1.0.1 --build 42
  flightline builds attach com.example.myapp --version 1.0.1 --build 42 --platform IOS --output json`,
}

var (
	buildsListLimit      int
	buildsGetBuild       string
	buildsAttachVersion  string
	buildsAttachBuild    string
	buildsAttachPlatform string
)

func init() {
	buildsListCmd.Flags().IntVar(&buildsListLimit, "limit", 0, "max builds to emit (0 = no cap)")

	buildsGetCmd.Flags().StringVar(&buildsGetBuild, "build", "", "build number to fetch (CFBundleVersion, e.g. 42)")
	_ = buildsGetCmd.MarkFlagRequired("build")

	buildsAttachCmd.Flags().StringVar(&buildsAttachVersion, "version", "", "App Store version string the build is attached to (e.g. 1.0.1)")
	buildsAttachCmd.Flags().StringVar(&buildsAttachBuild, "build", "", "build number to attach (CFBundleVersion, e.g. 42)")
	buildsAttachCmd.Flags().StringVar(&buildsAttachPlatform, "platform", "IOS", "platform of the App Store version (IOS|MAC_OS|TV_OS|VISION_OS)")
	_ = buildsAttachCmd.MarkFlagRequired("version")
	_ = buildsAttachCmd.MarkFlagRequired("build")

	buildsCmd.AddCommand(buildsListCmd)
	buildsCmd.AddCommand(buildsGetCmd)
	buildsCmd.AddCommand(buildsAttachCmd)
	rootCmd.AddCommand(buildsCmd)
}

func runBuildsList(cmd *cobra.Command, args []string) error {
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
	views, err := collectBuilds(cmd.Context(), c, "/v1/apps/"+appID+"/builds", q, buildsListLimit)
	if err != nil {
		return err
	}
	return Render(BuildList{Builds: views}, outputMode())
}

func runBuildsGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	build := strings.TrimSpace(buildsGetBuild)
	if build == "" {
		return errors.New("builds: --build is required")
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
		"filter[version]": {build},
		"limit":           {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		cmd.Context(), c, "/v1/apps/"+appID+"/builds", q,
	)
	if err != nil {
		return err
	}
	if len(page.Data) == 0 {
		return fmt.Errorf("builds: no build %q found for %q", build, bundleID)
	}

	view := &BuildView{
		ID:         page.Data[0].ID,
		Type:       page.Data[0].Type,
		Attributes: page.Data[0].Attributes,
	}
	return Render(view, outputMode())
}

func collectBuilds(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]BuildView, error) {
	out := make([]BuildView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.BuildAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, BuildView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// BuildAttachResult is the JSON-stable envelope for `builds attach`.
// Action is "attached" (PATCH linked the build) or "noop" (already linked).
type BuildAttachResult struct {
	Action    string `json:"action"`
	Changed   bool   `json:"changed"`
	Version   string `json:"version"`
	VersionID string `json:"versionId"`
	Build     string `json:"build"`
	BuildID   string `json:"buildId"`
	Platform  string `json:"platform,omitempty"`
}

func (r *BuildAttachResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"CHANGED", boolString(r.Changed)},
		{"VERSION", r.Version},
		{"VERSION_ID", r.VersionID},
		{"BUILD", r.Build},
		{"BUILD_ID", r.BuildID},
		{"PLATFORM", r.Platform},
	}
	return headers, rows
}

// buildLinkageEnvelope is the relationships/build payload for GET (may be
// {"data": null}) and PATCH. GET returns only the {type,id} ref.
type buildLinkageEnvelope struct {
	Data *buildLinkageRef `json:"data"`
}

// buildLinkageRef is a pointer so Apple's data:null ("no build attached")
// round-trips without false-positive matching.
type buildLinkageRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// runBuildsAttach is idempotent: it PATCHes the build linkage only when the
// currently-attached build differs, otherwise returns action="noop".
func runBuildsAttach(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(buildsAttachVersion)
	buildNum := strings.TrimSpace(buildsAttachBuild)
	platform := strings.TrimSpace(buildsAttachPlatform)
	if versionStr == "" {
		return errors.New("builds: --version is required")
	}
	if buildNum == "" {
		return errors.New("builds: --build is required")
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
		return fmt.Errorf("builds: no version %q found for %q (platform=%s)", versionStr, bundleID, platform)
	}

	buildView, err := lookupBuild(cmd.Context(), c, appID, buildNum)
	if err != nil {
		return err
	}
	if buildView == nil {
		return fmt.Errorf("builds: no build %q found for %q", buildNum, bundleID)
	}

	current, err := getAttachedBuild(cmd.Context(), c, versionView.ID)
	if err != nil {
		return err
	}

	result := &BuildAttachResult{
		Version:   versionStr,
		VersionID: versionView.ID,
		Build:     buildNum,
		BuildID:   buildView.ID,
		Platform:  versionView.Attributes.Platform,
	}

	if current != nil && current.ID == buildView.ID {
		result.Action = "noop"
		result.Changed = false
		return Render(result, outputMode())
	}

	body := buildLinkageEnvelope{Data: &buildLinkageRef{Type: "builds", ID: buildView.ID}}
	if err := patchAttachedBuild(cmd.Context(), c, versionView.ID, body); err != nil {
		return err
	}
	result.Action = "attached"
	result.Changed = true
	return Render(result, outputMode())
}

// lookupBuild returns (nil, nil) when no build with version=<num> exists.
func lookupBuild(ctx context.Context, c *asc.Client, appID, buildNum string) (*BuildView, error) {
	q := url.Values{
		"filter[version]": {buildNum},
		"limit":           {"1"},
	}
	page, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		ctx, c, "/v1/apps/"+appID+"/builds", q,
	)
	if err != nil {
		return nil, err
	}
	if len(page.Data) == 0 {
		return nil, nil
	}
	return &BuildView{
		ID:         page.Data[0].ID,
		Type:       page.Data[0].Type,
		Attributes: page.Data[0].Attributes,
	}, nil
}

// resolveBuildID maps a build number (CFBundleVersion) to Apple's build
// resource id; an ambiguous number errors with the candidates instead of guessing.
func resolveBuildID(ctx context.Context, c *asc.Client, appID, bundleID, buildNum string) (string, error) {
	q := url.Values{
		"filter[version]": {buildNum},
		"limit":           {"2"},
	}
	page, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		ctx, c, "/v1/apps/"+appID+"/builds", q,
	)
	if err != nil {
		return "", err
	}
	switch len(page.Data) {
	case 0:
		return "", fmt.Errorf("no build %q found for %q: run `flightline builds list %s` to see uploaded builds", buildNum, bundleID, bundleID)
	case 1:
		return page.Data[0].ID, nil
	default:
		a, b := &page.Data[0], &page.Data[1]
		return "", fmt.Errorf(
			"build number %q is ambiguous for %q: matches build %s (uploaded %s) and build %s (uploaded %s); run `flightline builds list %s` to inspect them",
			buildNum, bundleID, a.ID, a.Attributes.UploadedDate, b.ID, b.Attributes.UploadedDate, bundleID,
		)
	}
}

// getAttachedBuild returns nil when Apple's response is data:null (none attached).
func getAttachedBuild(ctx context.Context, c *asc.Client, versionID string) (*buildLinkageRef, error) {
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/relationships/build"
	resp, err := asc.Get[buildLinkageEnvelope](ctx, c, path, nil)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// patchAttachedBuild PATCHes the linkage relationship (Apple returns 204).
func patchAttachedBuild(ctx context.Context, c *asc.Client, versionID string, body buildLinkageEnvelope) error {
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/relationships/build"
	if _, err := asc.Patch[buildLinkageEnvelope](ctx, c, path, nil, body); err != nil {
		return err
	}
	return nil
}
