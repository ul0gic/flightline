package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// BuildView is one row of the builds list/get output.
type BuildView struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`
	Attributes asc.BuildAttributes `json:"attributes"`
}

// BuildList is the table-aware view for `builds list`.
type BuildList struct {
	Builds []BuildView `json:"builds"`
}

// TableRows implements TableRenderable for the builds list view.
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

// TableRows for a single build. Vertical layout reads better for one record.
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

// expiredCell highlights expiry in the table column. Apple's API surfaces
// expiry as a boolean snapshot; if the build is expired we want it loud
// because expired builds can't ship to TestFlight or be attached to a
// version submission.
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
	Example: `  skipper builds list com.example.myapp
  skipper builds list com.example.myapp --limit 20
  skipper builds list com.example.myapp --output json | jq -r '.builds[].version'`,
}

var buildsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get a single build by build number (CFBundleVersion)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBuildsGet,
	Example: `  skipper builds get com.example.myapp --build 42
  skipper builds get com.example.myapp --build 42 --output json | jq .attributes.processingState`,
}

var (
	buildsListLimit int
	buildsGetBuild  string
)

func init() {
	buildsListCmd.Flags().IntVar(&buildsListLimit, "limit", 0, "max builds to emit (0 = no cap)")

	buildsGetCmd.Flags().StringVar(&buildsGetBuild, "build", "", "build number to fetch (CFBundleVersion, e.g. 42)")
	_ = buildsGetCmd.MarkFlagRequired("build")

	buildsCmd.AddCommand(buildsListCmd)
	buildsCmd.AddCommand(buildsGetCmd)
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
		return fmt.Errorf("builds: --build is required")
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

// collectBuilds walks the paging iterator and returns flattened BuildView rows.
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
