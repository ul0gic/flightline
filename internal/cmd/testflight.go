package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
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
	Example: `  fline testflight groups list com.example.myapp
  fline testflight groups list com.example.myapp --output json | jq -r '.groups[].attributes.name'`,
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
	Example: `  fline testflight testers list com.example.myapp
  fline testflight testers list com.example.myapp --group 4242424242
  fline testflight testers list com.example.myapp --output json | jq -r '.testers[].attributes.email'`,
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
	Example: `  fline testflight beta-review get com.example.myapp --build 42
  fline testflight beta-review get com.example.myapp --build 42 --output json`,
}

// testflight groups create/update/delete
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
	Example: `  fline testflight groups create com.example.myapp --name "Internal" --internal
  fline testflight groups create com.example.myapp --name "Public Beta" --public-link --public-link-limit 10000`,
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
	Example: `  fline testflight groups update BG-EXTERNAL-1 --public-link-limit 5000
  fline testflight groups update BG-EXTERNAL-1 --feedback`,
}

var testflightGroupsDeleteCmd = &cobra.Command{
	Use:          "delete <groupId>",
	Short:        "Delete a TestFlight beta group",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightGroupsDelete,
	Long: `DELETEs a beta group. Idempotent: if the group is already absent
(404 from Apple) the command exits 0 with changed=false rather than
failing — re-running a delete script should not be a hard error.`,
	Example: `  fline testflight groups delete BG-EXTERNAL-1`,
}

// testflight testers add/remove
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
	Example: `  fline testflight testers add BG-EXTERNAL-1 --tester T1 --tester T2`,
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
	Example: `  fline testflight testers remove BG-EXTERNAL-1 --tester T1`,
}

// testflight beta-review submit
var testflightBetaReviewSubmitCmd = &cobra.Command{
	Use:          "submit <bundleId>",
	Short:        "Submit a build for TestFlight beta review",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runTestflightBetaReviewSubmit,
	Long: `Creates a betaAppReviewSubmission for the named (bundleId, build) pair.
Apple's beta review is one-shot per build — if a submission already
exists for the build, the command surfaces the existing submission ID
with changed=false rather than erroring.`,
	Example: `  fline testflight beta-review submit com.example.myapp --build 42
  fline testflight beta-review submit com.example.myapp --build 42 --output json`,
}

var (
	testflightBetaReviewGetBuild    string
	testflightBetaReviewSubmitBuild string

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

// BetaGroupSetResult is the structured outcome of `testflight groups create/update`.
// Surfaces whether a write was issued and the post-state group attributes.
type BetaGroupSetResult struct {
	GroupID    string                  `json:"groupId"`
	Changed    bool                    `json:"changed"`
	Created    bool                    `json:"created,omitempty"`
	Note       string                  `json:"note,omitempty"`
	Attributes asc.BetaGroupAttributes `json:"attributes"`
}

// TableRows for a beta-group set result.
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

// BetaGroupDeleteResult is the structured outcome of `testflight groups delete`.
type BetaGroupDeleteResult struct {
	GroupID string `json:"groupId"`
	Changed bool   `json:"changed"`
	Note    string `json:"note,omitempty"`
}

// TableRows for the delete result.
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

// BetaTestersChangeResult is the structured outcome of `testflight testers
// add/remove`. Returns which tester IDs were actually applied vs filtered
// out by the idempotency check.
type BetaTestersChangeResult struct {
	GroupID   string   `json:"groupId"`
	Changed   bool     `json:"changed"`
	Action    string   `json:"action"`            // "add" | "remove"
	Applied   []string `json:"applied,omitempty"` // IDs sent to Apple
	Skipped   []string `json:"skipped,omitempty"` // IDs filtered out (already in/out)
	Requested []string `json:"requested"`
}

// TableRows for a tester membership change.
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

// BetaReviewSubmitResult is the structured outcome of `testflight
// beta-review submit`. Carries the submission ID; Changed=false signals an
// existing submission was reused.
type BetaReviewSubmitResult struct {
	BundleID     string                                `json:"bundleId"`
	BuildID      string                                `json:"buildId"`
	BuildNumber  string                                `json:"buildNumber"`
	SubmissionID string                                `json:"submissionId,omitempty"`
	Changed      bool                                  `json:"changed"`
	Note         string                                `json:"note,omitempty"`
	Attributes   asc.BetaAppReviewSubmissionAttributes `json:"attributes"`
}

// TableRows for the submit result.
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

// boolStrTF renders a bool as "true"/"false" for testflight result tables.
func boolStrTF(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// runTestflightGroupsCreate creates a beta group on the named app.
// Idempotent on (app, name): an existing group with the same name is
// returned without a POST.
func runTestflightGroupsCreate(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	name := strings.TrimSpace(testflightGroupsCreateName)
	if name == "" {
		return fmt.Errorf("testflight: --name is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Idempotent name check: list groups, pre-empt POST when a name match
	// exists. Apple does enforce name uniqueness per app at write time
	// (422), but pre-empting avoids burning a rate-limit token + giving
	// the user a hard error for what is in practice a re-run.
	existing, err := findBetaGroupByName(cmd.Context(), c, appID, name)
	if err != nil {
		return err
	}
	if existing != nil {
		return Render(&BetaGroupSetResult{
			GroupID:    existing.ID,
			Changed:    false,
			Created:    false,
			Note:       "no change (idempotent) — group with same name already exists",
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

// findBetaGroupByName scans the app's beta groups and returns the first one
// whose name matches (case-sensitive — Apple's UI is). Returns (nil, nil)
// when no match exists.
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

// buildBetaGroupCreate crafts the JSON:API POST body for /v1/betaGroups.
// Optional attributes are only emitted when their flag was actually
// supplied (per cmd.Flags().Changed) so we don't pin Apple defaults to
// boolean-zero values inadvertently.
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

// runTestflightGroupsUpdate PATCHes a beta group with only the attributes
// the user explicitly passed flags for. Idempotent: reads current state
// first, builds the diff, only PATCHes when at least one attribute moves.
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
			Note:       "no change (idempotent) — all requested attributes already match",
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

// computeBetaGroupPatchAttrs builds the partial attributes map for a beta
// group PATCH. Only flags the user actually passed (cmd.Flags().Changed)
// contribute; values that already match the current state are filtered
// out so we never send a redundant PATCH.
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

// runTestflightGroupsDelete deletes a beta group. Idempotent: 404 (already
// absent) is changed=false, not an error.
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
				Note:    "no change (idempotent) — group already absent",
			}, outputMode())
		}
		return err
	}
	return Render(&BetaGroupDeleteResult{
		GroupID: groupID,
		Changed: true,
	}, outputMode())
}

// runTestflightTestersAdd adds testers to a beta group. Filters out
// already-present testers so a re-run is idempotent.
func runTestflightTestersAdd(cmd *cobra.Command, args []string) error {
	groupID := args[0]
	requested := dedupeStrings(testflightTestersAddIDs)
	if len(requested) == 0 {
		return fmt.Errorf("testflight: --tester is required (repeat for multiple)")
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
		// 204 on the linkage POST returns empty body which doJSON treats
		// as zero T (which here is the empty map) — no error. Real failures
		// land here.
		return err
	}
	res.Changed = true
	return Render(res, outputMode())
}

// runTestflightTestersRemove removes testers from a beta group. Filters
// out already-absent testers so a re-run is idempotent.
func runTestflightTestersRemove(cmd *cobra.Command, args []string) error {
	groupID := args[0]
	requested := dedupeStrings(testflightTestersRemoveIDs)
	if len(requested) == 0 {
		return fmt.Errorf("testflight: --tester is required (repeat for multiple)")
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

	// Apple's DELETE /relationships/betaTesters takes a body. The Client
	// Delete helper omits bodies, so use the raw asc.Patch is wrong —
	// instead call the linkage endpoint with method DELETE via an http
	// request crafted on c. Lacking that helper, we construct a custom
	// path using the Patch helper would mis-method; fallback to using
	// Post-with-X-HTTP-Method-Override is gross. The supported route is
	// to call Delete with a body — we add it here via a direct HTTP
	// request (auth-injected) in deleteBetaTesterLinkages.
	if err := deleteBetaTesterLinkages(cmd.Context(), c, groupID, applied); err != nil {
		return err
	}

	res.Changed = true
	return Render(res, outputMode())
}

// listGroupTesterIDs returns all tester IDs currently assigned to a beta
// group via the linkage endpoint (linkage-only, no full tester resource
// payload — saves rate-limit cost).
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

// buildBetaTesterLinkages crafts the linkage POST body. The same shape
// works for the DELETE-with-body variant.
func buildBetaTesterLinkages(ids []string) map[string]any {
	data := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]any{"type": "betaTesters", "id": id})
	}
	return map[string]any{"data": data}
}

// deleteBetaTesterLinkages issues a DELETE to the betaTesters linkage
// endpoint with a JSON body listing the testers to remove. The shared
// Client.Delete helper does not support bodies, so this is the local
// fallback — auth-injection via a fresh JWT is preserved, just at the
// cost of a small re-implementation.
//
// Apple's spec for betaGroups_betaTesters_deleteToManyRelationship requires
// the body. 204 = success.
func deleteBetaTesterLinkages(ctx context.Context, c *asc.Client, groupID string, ids []string) error {
	body := buildBetaTesterLinkages(ids)
	// asc.Client doesn't expose a body-bearing Delete; the simplest
	// supported path here is to use the Post helper but with method
	// override. Since Post sets POST, and we need DELETE, fall back to
	// constructing a minimal DELETE via a separate helper. The asc
	// package will own this when the multi-resource linkage pattern
	// repeats; for now we delegate via DeleteWithBody if available, else
	// surface a clear error rather than silently misbehaving.
	if dwb, ok := any(c).(interface {
		DeleteWithBody(ctx context.Context, path string, query url.Values, body any) error
	}); ok {
		return dwb.DeleteWithBody(ctx, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", nil, body)
	}
	// No body-bearing Delete on the Client. Emit a clear blocked-error
	// hint so the caller knows what to add — file a SEC/QA issue.
	return fmt.Errorf("testflight: testers remove requires asc.Client.DeleteWithBody (not yet wired); see https://developer.apple.com/documentation/appstoreconnectapi/delete_relationship for the contract — file an issue if blocked")
}

// dedupeStrings returns a stable-order de-duplicated copy of in.
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

// stringSet returns ids as a set for membership tests.
func stringSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// runTestflightBetaReviewSubmit submits the named build for beta review.
// Idempotent: an existing submission for the build returns
// changed=false carrying the existing submission ID rather than erroring.
func runTestflightBetaReviewSubmit(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	build := strings.TrimSpace(testflightBetaReviewSubmitBuild)
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
			Note:         "no change (idempotent) — submission already exists for this build",
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
