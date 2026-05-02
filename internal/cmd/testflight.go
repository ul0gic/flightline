package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// BetaGroupView is one row of the testflight groups list output.
type BetaGroupView struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Attributes asc.BetaGroupAttributes `json:"attributes"`
}

// BetaGroupList is the table-aware view for `testflight groups list`.
type BetaGroupList struct {
	Groups []BetaGroupView `json:"groups"`
}

// TableRows implements TableRenderable for the beta-group list view.
func (l BetaGroupList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"NAME", "INTERNAL", "TESTERS_LIMIT", "PUBLIC_LINK", "ID"}
	rows = make([][]string, 0, len(l.Groups))
	for i := range l.Groups {
		g := &l.Groups[i]
		limit := ""
		if g.Attributes.PublicLinkLimit > 0 {
			limit = strconv.Itoa(g.Attributes.PublicLinkLimit)
		}
		rows = append(rows, []string{
			g.Attributes.Name,
			boolPtrStr(g.Attributes.IsInternalGroup),
			limit,
			g.Attributes.PublicLink,
			g.ID,
		})
	}
	return headers, rows
}

// BetaTesterView is one row of the testflight testers list output.
type BetaTesterView struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Attributes asc.BetaTesterAttributes `json:"attributes"`
}

// BetaTesterList is the table-aware view for `testflight testers list`.
type BetaTesterList struct {
	Testers []BetaTesterView `json:"testers"`
}

// TableRows implements TableRenderable for the beta-testers list view.
func (l BetaTesterList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"EMAIL", "FIRST", "LAST", "INVITE", "STATE", "ID"}
	rows = make([][]string, 0, len(l.Testers))
	for i := range l.Testers {
		t := &l.Testers[i]
		rows = append(rows, []string{
			t.Attributes.Email,
			t.Attributes.FirstName,
			t.Attributes.LastName,
			t.Attributes.InviteType,
			t.Attributes.State,
			t.ID,
		})
	}
	return headers, rows
}

// BetaReviewView is the read-side view for `testflight beta-review get`.
// Wraps the per-build BetaAppReviewSubmission record. Apple makes a fresh
// submission per build, so this is keyed (bundle, build).
type BetaReviewView struct {
	BundleID    string                                `json:"bundleId"`
	BuildID     string                                `json:"buildId"`
	BuildNumber string                                `json:"buildNumber"`
	ID          string                                `json:"id,omitempty"`
	Type        string                                `json:"type,omitempty"`
	Attributes  asc.BetaAppReviewSubmissionAttributes `json:"attributes"`
	// Note carries a "no submission yet" message when Apple returns 404 on
	// the build → betaAppReviewSubmission relationship. Empty when a real
	// submission exists.
	Note string `json:"note,omitempty"`
}

// TableRows for the beta-review view. Vertical layout reads better.
func (v *BetaReviewView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"BUILD_NUMBER", v.BuildNumber},
		{"BUILD_ID", v.BuildID},
		{"SUBMISSION_ID", v.ID},
		{"BETA_REVIEW_STATE", testflightStateCell(v.Attributes.BetaReviewState)},
		{"SUBMITTED_DATE", v.Attributes.SubmittedDate},
	}
	if v.Note != "" {
		rows = append(rows, []string{"NOTE", v.Note})
	}
	return headers, rows
}

func testflightStateCell(s string) string {
	if s == "" {
		return "(no submission)"
	}
	return s
}

var testflightCmd = &cobra.Command{
	Use:   "testflight",
	Short: "Inspect TestFlight beta groups, testers, and review state",
	Long: `testflight groups read commands over Apple's TestFlight resources:

  - groups list <bundleId>           — list internal + external beta groups
  - testers list <bundleId>          — list testers in the app or a group
  - beta-review get <bundleId> --build <n>
                                     — show beta-review state for a build

Phase 3 will add invite/manage write verbs; v1 is read-only.`,
}

// testflight groups
var testflightGroupsCmd = &cobra.Command{
	Use:   "groups",
	Short: "Manage and inspect TestFlight beta groups",
}

var testflightGroupsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List beta groups for an app (internal and external)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightGroupsList,
	Example: `  skipper testflight groups list com.example.myapp
  skipper testflight groups list com.example.myapp --output json | jq -r '.groups[].attributes.name'`,
}

var testflightGroupsListLimit int

// testflight testers
var testflightTestersCmd = &cobra.Command{
	Use:   "testers",
	Short: "Manage and inspect TestFlight beta testers",
}

var testflightTestersListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List beta testers for an app (optionally scoped to a group)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightTestersList,
	Example: `  skipper testflight testers list com.example.myapp
  skipper testflight testers list com.example.myapp --group 4242424242
  skipper testflight testers list com.example.myapp --output json | jq -r '.testers[].attributes.email'`,
}

var (
	testflightTestersListGroup string
	testflightTestersListLimit int
)

// testflight beta-review
var testflightBetaReviewCmd = &cobra.Command{
	Use:   "beta-review",
	Short: "Inspect TestFlight beta-review submissions",
}

var testflightBetaReviewGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Show the beta-review submission state for a specific build",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightBetaReviewGet,
	Example: `  skipper testflight beta-review get com.example.myapp --build 42
  skipper testflight beta-review get com.example.myapp --build 42 --output json`,
}

var testflightBetaReviewGetBuild string

func init() {
	testflightGroupsListCmd.Flags().IntVar(&testflightGroupsListLimit, "limit", 0, "max groups to emit (0 = no cap)")

	testflightTestersListCmd.Flags().StringVar(&testflightTestersListGroup, "group", "", "scope listing to this beta-group ID; empty = app-wide")
	testflightTestersListCmd.Flags().IntVar(&testflightTestersListLimit, "limit", 0, "max testers to emit (0 = no cap)")

	testflightBetaReviewGetCmd.Flags().StringVar(&testflightBetaReviewGetBuild, "build", "", "build number to inspect (CFBundleVersion, e.g. 42)")
	_ = testflightBetaReviewGetCmd.MarkFlagRequired("build")

	testflightGroupsCmd.AddCommand(testflightGroupsListCmd)
	testflightTestersCmd.AddCommand(testflightTestersListCmd)
	testflightBetaReviewCmd.AddCommand(testflightBetaReviewGetCmd)

	testflightCmd.AddCommand(testflightGroupsCmd)
	testflightCmd.AddCommand(testflightTestersCmd)
	testflightCmd.AddCommand(testflightBetaReviewCmd)
	rootCmd.AddCommand(testflightCmd)
}

func runTestflightGroupsList(cmd *cobra.Command, args []string) error {
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
	views, err := collectBetaGroups(cmd.Context(), c, "/v1/apps/"+appID+"/betaGroups", q, testflightGroupsListLimit)
	if err != nil {
		return err
	}
	return Render(BetaGroupList{Groups: views}, outputMode())
}

func runTestflightTestersList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	var path string
	q := url.Values{"limit": {"200"}}

	if g := strings.TrimSpace(testflightTestersListGroup); g != "" {
		// Group-scoped: testers belonging to a specific beta group. Bundle
		// is still resolved so a typo in --group surfaces against the
		// expected app rather than a foreign group.
		if _, err := resolveAppID(cmd.Context(), c, bundleID); err != nil {
			return err
		}
		path = "/v1/betaGroups/" + g + "/betaTesters"
	} else {
		appID, err := resolveAppID(cmd.Context(), c, bundleID)
		if err != nil {
			return err
		}
		path = "/v1/apps/" + appID + "/betaTesters"
	}

	views, err := collectBetaTesters(cmd.Context(), c, path, q, testflightTestersListLimit)
	if err != nil {
		return err
	}
	return Render(BetaTesterList{Testers: views}, outputMode())
}

func runTestflightBetaReviewGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	build := strings.TrimSpace(testflightBetaReviewGetBuild)
	if build == "" {
		return fmt.Errorf("testflight: --build is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Find the build by CFBundleVersion under the app.
	bq := url.Values{
		"filter[version]": {build},
		"limit":           {"1"},
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		cmd.Context(), c, "/v1/apps/"+appID+"/builds", bq,
	)
	if err != nil {
		return err
	}
	if len(bpage.Data) == 0 {
		return fmt.Errorf("testflight: no build %q found for %q", build, bundleID)
	}
	buildID := bpage.Data[0].ID

	view := &BetaReviewView{
		BundleID:    bundleID,
		BuildID:     buildID,
		BuildNumber: build,
	}

	// Fetch the per-build betaAppReviewSubmission. Apple returns 200 with
	// `data: null` (or an empty data block) when no submission exists for
	// the build; older accounts may return 404. Both surface as the typed
	// "(no submission)" view rather than fatal.
	resp, err := asc.Get[asc.Single[asc.BetaAppReviewSubmissionAttributes]](
		cmd.Context(), c, "/v1/builds/"+buildID+"/betaAppReviewSubmission", nil,
	)
	if err != nil {
		view.Note = "no beta-review submission yet for this build"
		return Render(view, outputMode())
	}
	view.ID = resp.Data.ID
	view.Type = resp.Data.Type
	view.Attributes = resp.Data.Attributes
	if view.ID == "" {
		view.Note = "no beta-review submission yet for this build"
	}
	return Render(view, outputMode())
}

// collectBetaGroups walks the paging iterator and returns flattened
// BetaGroupView rows.
func collectBetaGroups(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]BetaGroupView, error) {
	out := make([]BetaGroupView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.BetaGroupAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, BetaGroupView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// collectBetaTesters walks the paging iterator and returns flattened
// BetaTesterView rows.
func collectBetaTesters(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]BetaTesterView, error) {
	out := make([]BetaTesterView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.BetaTesterAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, BetaTesterView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}
