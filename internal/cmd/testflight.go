package cmd

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type BetaGroupView struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Attributes asc.BetaGroupAttributes `json:"attributes"`
}

type BetaGroupList struct {
	Groups []BetaGroupView `json:"groups"`
}

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

type BetaTesterView struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Attributes asc.BetaTesterAttributes `json:"attributes"`
}

type BetaTesterList struct {
	Testers []BetaTesterView `json:"testers"`
}

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

// Apple makes a fresh submission per build, so this view is keyed (bundle, build).
type BetaReviewView struct {
	BundleID    string                                `json:"bundleId"`
	BuildID     string                                `json:"buildId"`
	BuildNumber string                                `json:"buildNumber"`
	ID          string                                `json:"id,omitempty"`
	Type        string                                `json:"type,omitempty"`
	Attributes  asc.BetaAppReviewSubmissionAttributes `json:"attributes"`
	// Carries a "no submission yet" message on a 404 from the build relationship; empty otherwise.
	Note string `json:"note,omitempty"`
}

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

  - groups list <bundleId>          : list internal + external beta groups
  - testers list <bundleId>         : list testers in the app or a group
  - beta-review get <bundleId> --build <n>
                                    : show beta-review state for a build`,
}

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
	Example: `  flightline testflight groups list com.example.myapp
  flightline testflight groups list com.example.myapp --output json | jq -r '.groups[].attributes.name'`,
}

var testflightGroupsListLimit int

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
	Example: `  flightline testflight testers list com.example.myapp
  flightline testflight testers list com.example.myapp --group 4242424242
  flightline testflight testers list com.example.myapp --output json | jq -r '.testers[].attributes.email'`,
}

var (
	testflightTestersListGroup string
	testflightTestersListLimit int
)

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
	Example: `  flightline testflight beta-review get com.example.myapp --build 42
	  flightline testflight beta-review get com.example.myapp --build 2 --version 1.1 --platform IOS
	  flightline testflight beta-review get com.example.myapp --build 42 --output json`,
}

var testflightGroupsCreateCmd = &cobra.Command{
	Use:          "create <bundleId>",
	Short:        "Create a TestFlight beta group",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightGroupsCreate,
	Long: `Creates a beta group on the named app. Idempotent: if a group with the
same --name already exists for the app, the existing group is returned
without a POST and changed=false.

Internal vs external is selected via --internal (default false = external).
Public-link controls are optional and only meaningful for external groups;
Apple silently ignores them on internal groups.`,
	Example: `  flightline testflight groups create com.example.myapp --name "Internal" --internal
  flightline testflight groups create com.example.myapp --name "Public Beta" --public-link --public-link-limit 10000`,
}

var testflightGroupsUpdateCmd = &cobra.Command{
	Use:          "update <groupId>",
	Short:        "Update a TestFlight beta group's mutable attributes",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightGroupsUpdate,
	Long: `PATCHes a beta group. Only flags explicitly passed are sent; omitted
flags leave the corresponding attribute untouched. Idempotent: reads
current state first, only PATCHes when at least one attribute differs.`,
	Example: `  flightline testflight groups update BG-EXTERNAL-1 --public-link-limit 5000
  flightline testflight groups update BG-EXTERNAL-1 --feedback`,
}

var testflightGroupsDeleteCmd = &cobra.Command{
	Use:          "delete <groupId>",
	Short:        "Delete a TestFlight beta group",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightGroupsDelete,
	Long: `DELETEs a beta group. Idempotent: if the group is already absent
(404 from Apple) the command exits 0 with changed=false rather than
failing: re-running a delete script should not be a hard error.`,
	Example: `  flightline testflight groups delete BG-EXTERNAL-1`,
}

var testflightTestersAddCmd = &cobra.Command{
	Use:          "add <groupId>",
	Short:        "Add testers to a TestFlight beta group (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightTestersAdd,
	Long: `Adds one or more testers to a beta group via POST
/v1/betaGroups/{id}/relationships/betaTesters. Pass tester IDs via
--tester (repeatable). Idempotent: testers already in the group are
filtered out before the POST so re-running the command is a no-op.`,
	Example: `  flightline testflight testers add BG-EXTERNAL-1 --tester T1 --tester T2`,
}

var testflightTestersRemoveCmd = &cobra.Command{
	Use:          "remove <groupId>",
	Short:        "Remove testers from a TestFlight beta group (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightTestersRemove,
	Long: `Removes one or more testers from a beta group via DELETE
/v1/betaGroups/{id}/relationships/betaTesters. Idempotent: testers
already absent are filtered out so re-running is a no-op.`,
	Example: `  flightline testflight testers remove BG-EXTERNAL-1 --tester T1`,
}

var testflightBetaReviewSubmitCmd = &cobra.Command{
	Use:          "submit <bundleId>",
	Short:        "Submit a build for TestFlight beta review",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightBetaReviewSubmit,
	Long: `Creates a betaAppReviewSubmission for the named (bundleId, build) pair.
Apple's beta review is one-shot per build: if a submission already
exists for the build, the command surfaces the existing submission ID
with changed=false rather than erroring.`,
	Example: `  flightline testflight beta-review submit com.example.myapp --build 42
	  flightline testflight beta-review submit com.example.myapp --build 2 --version 1.1 --platform IOS
	  flightline testflight beta-review submit com.example.myapp --build 42 --output json`,
}

var (
	testflightBetaReviewGetBuild    string
	testflightBetaReviewGetVersion  string
	testflightBetaReviewGetPlatform string
	testflightBetaReviewSubmitBuild string
	testflightBetaReviewSubmitVer   string
	testflightBetaReviewSubmitPlat  string

	testflightGroupsCreateName            string
	testflightGroupsCreateInternal        bool
	testflightGroupsCreatePublicLink      bool
	testflightGroupsCreatePublicLinkLimit int
	testflightGroupsCreateFeedback        bool

	testflightGroupsUpdateName            string
	testflightGroupsUpdatePublicLink      bool
	testflightGroupsUpdatePublicLinkLimit int
	testflightGroupsUpdateFeedback        bool

	testflightTestersAddIDs    []string
	testflightTestersRemoveIDs []string
)

func init() {
	testflightGroupsListCmd.Flags().IntVar(&testflightGroupsListLimit, "limit", 0, "max groups to emit (0 = no cap)")

	testflightTestersListCmd.Flags().StringVar(&testflightTestersListGroup, "group", "", "scope listing to this beta-group ID; empty = app-wide")
	testflightTestersListCmd.Flags().IntVar(&testflightTestersListLimit, "limit", 0, "max testers to emit (0 = no cap)")

	testflightBetaReviewGetCmd.Flags().StringVar(&testflightBetaReviewGetBuild, "build", "", "build number to inspect (CFBundleVersion, e.g. 42)")
	testflightBetaReviewGetCmd.Flags().StringVar(&testflightBetaReviewGetVersion, "version", "", "App Store version/train used to disambiguate duplicate build numbers")
	testflightBetaReviewGetCmd.Flags().StringVar(&testflightBetaReviewGetPlatform, "platform", "IOS", "platform used to disambiguate duplicate build numbers")
	_ = testflightBetaReviewGetCmd.MarkFlagRequired("build")

	testflightGroupsCreateCmd.Flags().StringVar(&testflightGroupsCreateName, "name", "", "group name (must be unique per app)")
	testflightGroupsCreateCmd.Flags().BoolVar(&testflightGroupsCreateInternal, "internal", false, "create as an internal group (default external)")
	testflightGroupsCreateCmd.Flags().BoolVar(&testflightGroupsCreatePublicLink, "public-link", false, "enable the public join link (external groups only)")
	testflightGroupsCreateCmd.Flags().IntVar(&testflightGroupsCreatePublicLinkLimit, "public-link-limit", 0, "max testers reachable via the public link (0 = unlimited)")
	testflightGroupsCreateCmd.Flags().BoolVar(&testflightGroupsCreateFeedback, "feedback", false, "enable in-app feedback for this group")
	_ = testflightGroupsCreateCmd.MarkFlagRequired("name")

	testflightGroupsUpdateCmd.Flags().StringVar(&testflightGroupsUpdateName, "name", "", "rename the group")
	testflightGroupsUpdateCmd.Flags().BoolVar(&testflightGroupsUpdatePublicLink, "public-link", false, "enable the public join link")
	testflightGroupsUpdateCmd.Flags().IntVar(&testflightGroupsUpdatePublicLinkLimit, "public-link-limit", 0, "set the max testers reachable via the public link")
	testflightGroupsUpdateCmd.Flags().BoolVar(&testflightGroupsUpdateFeedback, "feedback", false, "enable in-app feedback")

	testflightTestersAddCmd.Flags().StringSliceVar(&testflightTestersAddIDs, "tester", nil, "tester ID to add (repeat for multiple)")
	_ = testflightTestersAddCmd.MarkFlagRequired("tester")

	testflightTestersRemoveCmd.Flags().StringSliceVar(&testflightTestersRemoveIDs, "tester", nil, "tester ID to remove (repeat for multiple)")
	_ = testflightTestersRemoveCmd.MarkFlagRequired("tester")

	testflightBetaReviewSubmitCmd.Flags().StringVar(&testflightBetaReviewSubmitBuild, "build", "", "build number to submit (CFBundleVersion, e.g. 42)")
	testflightBetaReviewSubmitCmd.Flags().StringVar(&testflightBetaReviewSubmitVer, "version", "", "App Store version/train used to disambiguate duplicate build numbers")
	testflightBetaReviewSubmitCmd.Flags().StringVar(&testflightBetaReviewSubmitPlat, "platform", "IOS", "platform used to disambiguate duplicate build numbers")
	_ = testflightBetaReviewSubmitCmd.MarkFlagRequired("build")

	testflightGroupsCmd.AddCommand(testflightGroupsListCmd)
	testflightGroupsCmd.AddCommand(testflightGroupsCreateCmd)
	testflightGroupsCmd.AddCommand(testflightGroupsUpdateCmd)
	testflightGroupsCmd.AddCommand(testflightGroupsDeleteCmd)

	testflightTestersCmd.AddCommand(testflightTestersListCmd)
	testflightTestersCmd.AddCommand(testflightTestersAddCmd)
	testflightTestersCmd.AddCommand(testflightTestersRemoveCmd)

	testflightBetaReviewCmd.AddCommand(testflightBetaReviewGetCmd)
	testflightBetaReviewCmd.AddCommand(testflightBetaReviewSubmitCmd)

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
		// Resolve the bundle even in group scope so a typo in --group surfaces against the expected app.
		if _, err := resolveAppID(cmd.Context(), c, bundleID); err != nil {
			return err
		}
		path = "/v1/betaGroups/" + g + "/betaTesters"
	} else {
		appID, err := resolveAppID(cmd.Context(), c, bundleID)
		if err != nil {
			return err
		}
		path = "/v1/betaTesters"
		q.Set("filter[apps]", appID)
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
		return errors.New("testflight: --build is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	buildID, err := resolveBuildIDWithOptions(cmd.Context(), c, appID, bundleID, build, buildLookupOptions{
		ReleaseVersion: testflightBetaReviewGetVersion,
		Platform:       testflightBetaReviewGetPlatform,
	})
	if err != nil {
		return err
	}

	view := &BetaReviewView{
		BundleID:    bundleID,
		BuildID:     buildID,
		BuildNumber: build,
	}

	// No submission yet surfaces as Apple's 200-with-null-data or a 404 on older accounts; both map to the "(no submission)" view.
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

type BetaGroupSetResult struct {
	GroupID    string                  `json:"groupId"`
	Changed    bool                    `json:"changed"`
	Created    bool                    `json:"created,omitempty"`
	Note       string                  `json:"note,omitempty"`
	Attributes asc.BetaGroupAttributes `json:"attributes"`
}

func (r *BetaGroupSetResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"GROUP_ID", r.GroupID},
		{"CHANGED", boolStrTF(r.Changed)},
		{"CREATED", boolStrTF(r.Created)},
		{"NAME", r.Attributes.Name},
		{"INTERNAL", boolPtrStr(r.Attributes.IsInternalGroup)},
		{"PUBLIC_LINK", r.Attributes.PublicLink},
		{"FEEDBACK", boolPtrStr(r.Attributes.FeedbackEnabled)},
	}
	if r.Note != "" {
		rows = append(rows, []string{"NOTE", r.Note})
	}
	return headers, rows
}

type BetaGroupDeleteResult struct {
	GroupID string `json:"groupId"`
	Changed bool   `json:"changed"`
	Note    string `json:"note,omitempty"`
}

func (r *BetaGroupDeleteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"GROUP_ID", r.GroupID},
		{"CHANGED", boolStrTF(r.Changed)},
	}
	if r.Note != "" {
		rows = append(rows, []string{"NOTE", r.Note})
	}
	return headers, rows
}

type BetaTestersChangeResult struct {
	GroupID   string   `json:"groupId"`
	Changed   bool     `json:"changed"`
	Action    string   `json:"action"`
	Applied   []string `json:"applied,omitempty"`
	Skipped   []string `json:"skipped,omitempty"`
	Requested []string `json:"requested"`
}

func (r *BetaTestersChangeResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"GROUP_ID", r.GroupID},
		{"ACTION", r.Action},
		{"CHANGED", boolStrTF(r.Changed)},
		{"REQUESTED", strings.Join(r.Requested, ",")},
		{"APPLIED", strings.Join(r.Applied, ",")},
		{"SKIPPED", strings.Join(r.Skipped, ",")},
	}
	return headers, rows
}

// Changed=false signals an existing submission was reused.
type BetaReviewSubmitResult struct {
	BundleID     string                                `json:"bundleId"`
	BuildID      string                                `json:"buildId"`
	BuildNumber  string                                `json:"buildNumber"`
	SubmissionID string                                `json:"submissionId,omitempty"`
	Changed      bool                                  `json:"changed"`
	Note         string                                `json:"note,omitempty"`
	Attributes   asc.BetaAppReviewSubmissionAttributes `json:"attributes"`
}

func (r *BetaReviewSubmitResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", r.BundleID},
		{"BUILD_NUMBER", r.BuildNumber},
		{"BUILD_ID", r.BuildID},
		{"SUBMISSION_ID", r.SubmissionID},
		{"CHANGED", boolStrTF(r.Changed)},
		{"BETA_REVIEW_STATE", testflightStateCell(r.Attributes.BetaReviewState)},
		{"SUBMITTED_DATE", r.Attributes.SubmittedDate},
	}
	if r.Note != "" {
		rows = append(rows, []string{"NOTE", r.Note})
	}
	return headers, rows
}

func boolStrTF(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func runTestflightGroupsCreate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	name := strings.TrimSpace(testflightGroupsCreateName)
	if name == "" {
		return errors.New("testflight: --name is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Pre-empt the POST on a name match: Apple 422s on duplicate names, but a re-run should be a no-op, not an error.
	existing, err := findBetaGroupByName(cmd.Context(), c, appID, name)
	if err != nil {
		return err
	}
	if existing != nil {
		return Render(&BetaGroupSetResult{
			GroupID:    existing.ID,
			Changed:    false,
			Created:    false,
			Note:       "no change (idempotent): group with same name already exists",
			Attributes: existing.Attributes,
		}, outputMode())
	}

	body := buildBetaGroupCreate(appID, name, testflightGroupsCreateInternal,
		cmd.Flags().Changed("public-link"), testflightGroupsCreatePublicLink,
		cmd.Flags().Changed("public-link-limit"), testflightGroupsCreatePublicLinkLimit,
		cmd.Flags().Changed("feedback"), testflightGroupsCreateFeedback)

	resp, err := asc.Post[asc.Single[asc.BetaGroupAttributes]](
		cmd.Context(), c, "/v1/betaGroups", nil, body,
	)
	if err != nil {
		return err
	}

	return Render(&BetaGroupSetResult{
		GroupID:    resp.Data.ID,
		Changed:    true,
		Created:    true,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

// Name match is case-sensitive, mirroring Apple's UI; returns (nil, nil) when no match exists.
func findBetaGroupByName(ctx context.Context, c *asc.Client, appID, name string) (*asc.Resource[asc.BetaGroupAttributes], error) {
	q := url.Values{
		"limit":                   {"200"},
		"filter[name]":            {name},
		"filter[isInternalGroup]": {},
	}
	q.Del("filter[isInternalGroup]") // not all spec versions support this filter; keep clean
	page, err := asc.Get[asc.Collection[asc.BetaGroupAttributes]](ctx, c, "/v1/apps/"+appID+"/betaGroups", q)
	if err != nil {
		return nil, err
	}
	for i := range page.Data {
		if page.Data[i].Attributes.Name == name {
			return &page.Data[i], nil
		}
	}
	return nil, nil
}

// Optional attributes are emitted only when their flag was set, so Apple defaults aren't pinned to boolean-zero.
func buildBetaGroupCreate(
	appID, name string,
	internal bool,
	publicLinkSet, publicLink bool,
	publicLinkLimitSet bool, publicLinkLimit int,
	feedbackSet, feedback bool,
) map[string]any {
	attrs := map[string]any{"name": name}
	attrs["isInternalGroup"] = internal
	if publicLinkSet {
		attrs["publicLinkEnabled"] = publicLink
	}
	if publicLinkLimitSet {
		attrs["publicLinkLimit"] = publicLinkLimit
		attrs["publicLinkLimitEnabled"] = publicLinkLimit > 0
	}
	if feedbackSet {
		attrs["feedbackEnabled"] = feedback
	}

	return map[string]any{
		"data": map[string]any{
			"type":       "betaGroups",
			"attributes": attrs,
			"relationships": map[string]any{
				"app": map[string]any{
					"data": map[string]any{"type": "apps", "id": appID},
				},
			},
		},
	}
}

func runTestflightGroupsUpdate(cmd *cobra.Command, args []string) error {
	groupID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	cur, err := asc.Get[asc.Single[asc.BetaGroupAttributes]](
		cmd.Context(), c, "/v1/betaGroups/"+groupID, nil,
	)
	if err != nil {
		return err
	}

	patchAttrs := computeBetaGroupPatchAttrs(cmd, cur.Data.Attributes)

	if len(patchAttrs) == 0 {
		return Render(&BetaGroupSetResult{
			GroupID:    groupID,
			Changed:    false,
			Note:       "no change (idempotent): all requested attributes already match",
			Attributes: cur.Data.Attributes,
		}, outputMode())
	}

	body := map[string]any{
		"data": map[string]any{
			"type":       "betaGroups",
			"id":         groupID,
			"attributes": patchAttrs,
		},
	}
	resp, err := asc.Patch[asc.Single[asc.BetaGroupAttributes]](
		cmd.Context(), c, "/v1/betaGroups/"+groupID, nil, body,
	)
	if err != nil {
		return err
	}
	return Render(&BetaGroupSetResult{
		GroupID:    groupID,
		Changed:    true,
		Attributes: resp.Data.Attributes,
	}, outputMode())
}

// Only changed flags contribute, and values matching current state are dropped, so no redundant PATCH is sent.
func computeBetaGroupPatchAttrs(cmd *cobra.Command, cur asc.BetaGroupAttributes) map[string]any {
	patch := map[string]any{}
	flags := cmd.Flags()
	if flags.Changed("name") {
		newName := strings.TrimSpace(testflightGroupsUpdateName)
		if newName != cur.Name {
			patch["name"] = newName
		}
	}
	if flags.Changed("public-link") {
		curVal := false
		if cur.PublicLinkEnabled != nil {
			curVal = *cur.PublicLinkEnabled
		}
		if curVal != testflightGroupsUpdatePublicLink {
			patch["publicLinkEnabled"] = testflightGroupsUpdatePublicLink
		}
	}
	if flags.Changed("public-link-limit") {
		if cur.PublicLinkLimit != testflightGroupsUpdatePublicLinkLimit {
			patch["publicLinkLimit"] = testflightGroupsUpdatePublicLinkLimit
			patch["publicLinkLimitEnabled"] = testflightGroupsUpdatePublicLinkLimit > 0
		}
	}
	if flags.Changed("feedback") {
		curVal := false
		if cur.FeedbackEnabled != nil {
			curVal = *cur.FeedbackEnabled
		}
		if curVal != testflightGroupsUpdateFeedback {
			patch["feedbackEnabled"] = testflightGroupsUpdateFeedback
		}
	}
	return patch
}

// 404 (already absent) is changed=false, not an error, so a re-run stays idempotent.
func runTestflightGroupsDelete(cmd *cobra.Command, args []string) error {
	groupID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}
	if err := c.Delete(cmd.Context(), "/v1/betaGroups/"+groupID, nil); err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return Render(&BetaGroupDeleteResult{
				GroupID: groupID,
				Changed: false,
				Note:    "no change (idempotent): group already absent",
			}, outputMode())
		}
		return err
	}
	return Render(&BetaGroupDeleteResult{
		GroupID: groupID,
		Changed: true,
	}, outputMode())
}

// Already-present testers are filtered out so a re-run is idempotent.
func runTestflightTestersAdd(cmd *cobra.Command, args []string) error {
	groupID := args[0]
	requested := dedupeStrings(testflightTestersAddIDs)
	if len(requested) == 0 {
		return errors.New("testflight: --tester is required (repeat for multiple)")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	current, err := listGroupTesterIDs(cmd.Context(), c, groupID)
	if err != nil {
		return err
	}
	curSet := stringSet(current)

	var applied, skipped []string
	for _, id := range requested {
		if curSet[id] {
			skipped = append(skipped, id)
		} else {
			applied = append(applied, id)
		}
	}

	res := &BetaTestersChangeResult{
		GroupID:   groupID,
		Action:    "add",
		Requested: requested,
		Applied:   applied,
		Skipped:   skipped,
	}

	if len(applied) == 0 {
		res.Changed = false
		return Render(res, outputMode())
	}

	body := buildBetaTesterLinkages(applied)
	if _, err := asc.Post[map[string]any](
		cmd.Context(), c, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", nil, body,
	); err != nil {
		// A 204 (empty body) decodes to the zero map without error; only real failures reach here.
		return err
	}
	res.Changed = true
	return Render(res, outputMode())
}

// Already-absent testers are filtered out so a re-run is idempotent.
func runTestflightTestersRemove(cmd *cobra.Command, args []string) error {
	groupID := args[0]
	requested := dedupeStrings(testflightTestersRemoveIDs)
	if len(requested) == 0 {
		return errors.New("testflight: --tester is required (repeat for multiple)")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	current, err := listGroupTesterIDs(cmd.Context(), c, groupID)
	if err != nil {
		return err
	}
	curSet := stringSet(current)

	var applied, skipped []string
	for _, id := range requested {
		if curSet[id] {
			applied = append(applied, id)
		} else {
			skipped = append(skipped, id)
		}
	}

	res := &BetaTestersChangeResult{
		GroupID:   groupID,
		Action:    "remove",
		Requested: requested,
		Applied:   applied,
		Skipped:   skipped,
	}

	if len(applied) == 0 {
		res.Changed = false
		return Render(res, outputMode())
	}

	// Apple requires a body-bearing DELETE here, which the shared Client.Delete helper can't issue.
	if err := deleteBetaTesterLinkages(cmd.Context(), c, groupID, applied); err != nil {
		return err
	}

	res.Changed = true
	return Render(res, outputMode())
}

// Uses the linkage endpoint (IDs only, no full tester payload) to save rate-limit cost.
func listGroupTesterIDs(ctx context.Context, c *asc.Client, groupID string) ([]string, error) {
	q := url.Values{"limit": {"200"}}
	resp, err := asc.Get[map[string]any](
		ctx, c, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", q,
	)
	if err != nil {
		return nil, err
	}
	rawData, ok := resp["data"]
	if !ok {
		return nil, nil
	}
	arr, ok := rawData.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		obj, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := obj["id"].(string); ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// Same body shape serves both the linkage POST and the DELETE-with-body variant.
func buildBetaTesterLinkages(ids []string) map[string]any {
	data := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]any{"type": "betaTesters", "id": id})
	}
	return map[string]any{"data": data}
}

// Apple's deleteToManyRelationship requires a body the shared Client.Delete can't send; delegate to DeleteWithBody when present.
func deleteBetaTesterLinkages(ctx context.Context, c *asc.Client, groupID string, ids []string) error {
	body := buildBetaTesterLinkages(ids)
	if dwb, ok := any(c).(interface {
		DeleteWithBody(ctx context.Context, path string, query url.Values, body any) error
	}); ok {
		return dwb.DeleteWithBody(ctx, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", nil, body)
	}
	return errors.New("testflight: testers remove requires asc.Client.DeleteWithBody (not yet wired); see https://developer.apple.com/documentation/appstoreconnectapi/delete_relationship for the contract: file an issue if blocked")
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func stringSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// An existing submission for the build returns changed=false rather than erroring.
func runTestflightBetaReviewSubmit(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	build := strings.TrimSpace(testflightBetaReviewSubmitBuild)
	if build == "" {
		return errors.New("testflight: --build is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	buildID, err := resolveBuildIDWithOptions(cmd.Context(), c, appID, bundleID, build, buildLookupOptions{
		ReleaseVersion: testflightBetaReviewSubmitVer,
		Platform:       testflightBetaReviewSubmitPlat,
	})
	if err != nil {
		return err
	}

	// Idempotency check: does the build already have a submission?
	existing, err := asc.Get[asc.Single[asc.BetaAppReviewSubmissionAttributes]](
		cmd.Context(), c, "/v1/builds/"+buildID+"/betaAppReviewSubmission", nil,
	)
	if err == nil && existing.Data.ID != "" {
		return Render(&BetaReviewSubmitResult{
			BundleID:     bundleID,
			BuildID:      buildID,
			BuildNumber:  build,
			SubmissionID: existing.Data.ID,
			Changed:      false,
			Attributes:   existing.Data.Attributes,
			Note:         "no change (idempotent): submission already exists for this build",
		}, outputMode())
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "betaAppReviewSubmissions",
			"relationships": map[string]any{
				"build": map[string]any{
					"data": map[string]any{"type": "builds", "id": buildID},
				},
			},
		},
	}
	resp, err := asc.Post[asc.Single[asc.BetaAppReviewSubmissionAttributes]](
		cmd.Context(), c, "/v1/betaAppReviewSubmissions", nil, body,
	)
	if err != nil {
		return err
	}

	return Render(&BetaReviewSubmitResult{
		BundleID:     bundleID,
		BuildID:      buildID,
		BuildNumber:  build,
		SubmissionID: resp.Data.ID,
		Changed:      true,
		Attributes:   resp.Data.Attributes,
	}, outputMode())
}
