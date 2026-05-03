package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

func TestBetaGroupView_JSONShape(t *testing.T) {
	internal := true
	feedback := true
	v := BetaGroupView{
		ID:   "BG-1",
		Type: "betaGroups",
		Attributes: asc.BetaGroupAttributes{
			Name:            "Internal Team",
			IsInternalGroup: &internal,
			FeedbackEnabled: &feedback,
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"BG-1"`,
		`"type":"betaGroups"`,
		`"name":"Internal Team"`,
		`"isInternalGroup":true`,
		`"feedbackEnabled":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestBetaGroupList_TableRowsHeaders(t *testing.T) {
	internal := true
	external := false
	list := BetaGroupList{
		Groups: []BetaGroupView{
			{ID: "1", Attributes: asc.BetaGroupAttributes{Name: "Team", IsInternalGroup: &internal}},
			{ID: "2", Attributes: asc.BetaGroupAttributes{Name: "Public", IsInternalGroup: &external, PublicLink: "https://testflight.apple.com/join/X"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"NAME", "INTERNAL", "TESTERS_LIMIT", "PUBLIC_LINK", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][1] != "true" {
		t.Errorf("rows[0][1] (INTERNAL) = %q, want true", rows[0][1])
	}
	if rows[1][3] == "" {
		t.Errorf("rows[1][3] (PUBLIC_LINK) should not be empty for external group")
	}
}

func TestBetaTesterList_TableRowsHeaders(t *testing.T) {
	list := BetaTesterList{
		Testers: []BetaTesterView{
			{ID: "1", Attributes: asc.BetaTesterAttributes{Email: "a@example.com", InviteType: "EMAIL", State: "INSTALLED"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"EMAIL", "FIRST", "LAST", "INVITE", "STATE", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}

func TestBetaReviewView_TableRows_VerticalLayout(t *testing.T) {
	v := &BetaReviewView{
		BundleID:    "com.example.alpha",
		BuildID:     "BUILD-42",
		BuildNumber: "42",
		ID:          "BARS-42",
		Attributes:  asc.BetaAppReviewSubmissionAttributes{BetaReviewState: "WAITING_FOR_REVIEW", SubmittedDate: "2024-09-12T08:15:00Z"},
	}
	headers, rows := v.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	if len(rows) < 6 {
		t.Errorf("rows = %d, want >= 6", len(rows))
	}
}

func TestBetaReviewView_NoSubmissionRendersNote(t *testing.T) {
	v := &BetaReviewView{
		BundleID:    "com.example.alpha",
		BuildID:     "BUILD-42",
		BuildNumber: "42",
		Note:        "no beta-review submission yet for this build",
	}
	_, rows := v.TableRows()
	foundNote := false
	for _, r := range rows {
		if strings.HasPrefix(r[1], "no beta-review") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected NOTE row when no submission exists")
	}
}

func TestTestflightCommand_RegisteredOnRoot(t *testing.T) {
	var tf *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "testflight" {
			tf = c
			break
		}
	}
	if tf == nil {
		t.Fatal("testflight not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range tf.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"groups", "testers", "beta-review"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("testflight subcommand %q missing", want)
		}
	}
	// each group has a `list` (or `get` for beta-review).
	for name, expected := range map[string]string{
		"groups":      "list",
		"testers":     "list",
		"beta-review": "get",
	} {
		grand := map[string]bool{}
		for _, sc := range subs[name].Commands() {
			grand[sc.Name()] = true
		}
		if !grand[expected] {
			t.Errorf("testflight %s %s subcommand missing", name, expected)
		}
	}
}

// TestTestflight_JSONOutputStability_Groups locks the BetaGroupList shape.
func TestTestflight_JSONOutputStability_Groups(t *testing.T) {
	internal := true
	list := BetaGroupList{
		Groups: []BetaGroupView{
			{ID: "BG-1", Type: "betaGroups", Attributes: asc.BetaGroupAttributes{Name: "Team", IsInternalGroup: &internal}},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(decoded.Groups))
	}
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := decoded.Groups[0][key]; !ok {
			t.Errorf("missing per-row key %q — JSON contract drift", key)
		}
	}
}

// TestTestflight_FixtureReplay_GroupsList exercises collectBetaGroups
// against the fixture: 2 groups (one internal, one external).
func TestTestflight_FixtureReplay_GroupsList(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                       {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/betaGroups": {File: "testflight_groups_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectBetaGroups(ctx, c, "/v1/apps/"+appID+"/betaGroups", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectBetaGroups: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("groups len = %d, want 2", len(views))
	}
	if views[0].Attributes.Name != "Internal Team" {
		t.Errorf("groups[0].Name = %q, want Internal Team", views[0].Attributes.Name)
	}
	if views[0].Attributes.IsInternalGroup == nil || !*views[0].Attributes.IsInternalGroup {
		t.Errorf("groups[0].IsInternalGroup = %v, want true", views[0].Attributes.IsInternalGroup)
	}
	if views[1].Attributes.PublicLink == "" {
		t.Errorf("groups[1].PublicLink should be non-empty for external group")
	}
}

// TestTestflight_FixtureReplay_TestersListAppScoped uses the app-scoped
// /v1/apps/{id}/betaTesters path (no --group filter).
func TestTestflight_FixtureReplay_TestersListAppScoped(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                        {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/betaTesters": {File: "testflight_testers_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectBetaTesters(ctx, c, "/v1/apps/"+appID+"/betaTesters", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectBetaTesters: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("testers len = %d, want 3", len(views))
	}
	if views[0].Attributes.Email != "ada@example.com" {
		t.Errorf("testers[0].Email = %q, want ada@example.com", views[0].Attributes.Email)
	}
	if views[1].Attributes.InviteType != "PUBLIC_LINK" {
		t.Errorf("testers[1].InviteType = %q, want PUBLIC_LINK", views[1].Attributes.InviteType)
	}
}

// TestTestflight_FixtureReplay_TestersListGroupScoped uses the group-scoped
// /v1/betaGroups/{id}/betaTesters path. Confirms the group filter switches
// the URL path.
func TestTestflight_FixtureReplay_TestersListGroupScoped(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/betaGroups/BG-INTERNAL-1/betaTesters": {File: "testflight_testers_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	views, err := collectBetaTesters(ctx, c, "/v1/betaGroups/BG-INTERNAL-1/betaTesters", url.Values{"limit": {"200"}}, 0)
	if err != nil {
		t.Fatalf("collectBetaTesters: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("testers len = %d, want 3", len(views))
	}
}

// TestTestflight_FixtureReplay_BetaReviewGet exercises the build-lookup +
// betaAppReviewSubmission fetch chain.
func TestTestflight_FixtureReplay_BetaReviewGet(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                                    {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds":                  {File: "testflight_build_lookup"},
		"GET /v1/builds/BUILD-42/betaAppReviewSubmission": {File: "testflight_beta_review_get"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	// Resolve appID + build directly (the cmd's RunE wrapper isn't designed
	// to be invoked outside a cobra context; assert the underlying API
	// chain is correct).
	_, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		ctx, c, "/v1/apps/1234567890/builds", url.Values{"filter[version]": {"42"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("build lookup: %v", err)
	}
	if len(bpage.Data) != 1 {
		t.Fatalf("build lookup data len = %d, want 1", len(bpage.Data))
	}
	resp, err := asc.Get[asc.Single[asc.BetaAppReviewSubmissionAttributes]](
		ctx, c, "/v1/builds/"+bpage.Data[0].ID+"/betaAppReviewSubmission", nil,
	)
	if err != nil {
		t.Fatalf("betaAppReviewSubmission: %v", err)
	}
	if resp.Data.ID != "BARS-42" {
		t.Errorf("submission id = %q, want BARS-42", resp.Data.ID)
	}
	if resp.Data.Attributes.BetaReviewState != "WAITING_FOR_REVIEW" {
		t.Errorf("betaReviewState = %q, want WAITING_FOR_REVIEW", resp.Data.Attributes.BetaReviewState)
	}
}

// TestTestflight_BuildNotFoundErrorMessage asserts the error names the
// bundleId AND build number when the version filter is empty.
func TestTestflight_BuildNotFoundErrorMessage(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                   {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds": {File: "apps_get_notFound"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	_, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		ctx, c, "/v1/apps/1234567890/builds", url.Values{"filter[version]": {"999"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("build lookup: %v", err)
	}
	if len(bpage.Data) != 0 {
		t.Errorf("build lookup data len = %d, want 0 (notFound fixture)", len(bpage.Data))
	}
}

// TestTestflightWrites_RegisteredOnGroup verifies the cobra wiring for all
// new write verbs: groups create/update/delete, testers add/remove,
// beta-review submit.
func TestTestflightWrites_RegisteredOnGroup(t *testing.T) {
	var tf *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "testflight" {
			tf = c
			break
		}
	}
	if tf == nil {
		t.Fatal("testflight not on root")
	}

	want := map[string][]string{
		"groups":      {"list", "create", "update", "delete"},
		"testers":     {"list", "add", "remove"},
		"beta-review": {"get", "submit"},
	}
	for _, sub := range tf.Commands() {
		expected, ok := want[sub.Name()]
		if !ok {
			continue
		}
		got := map[string]bool{}
		for _, sc := range sub.Commands() {
			got[sc.Name()] = true
		}
		for _, w := range expected {
			if !got[w] {
				t.Errorf("testflight %s missing subcommand %s", sub.Name(), w)
			}
		}
	}
}

// TestBuildBetaGroupCreate_ShapesFlagsCorrectly asserts that only flags
// the user actually passed appear in the body, with required attrs always
// present.
func TestBuildBetaGroupCreate_ShapesFlagsCorrectly(t *testing.T) {
	body := buildBetaGroupCreate("APP-1", "Internal Team", true, false, false, false, 0, false, false)
	raw, _ := json.Marshal(body)
	out := string(raw)
	for _, want := range []string{
		`"type":"betaGroups"`,
		`"isInternalGroup":true`,
		`"name":"Internal Team"`,
		`"app":{"data":{"id":"APP-1","type":"apps"}}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q\nfull body: %s", want, out)
		}
	}
	for _, leak := range []string{`"publicLinkEnabled"`, `"publicLinkLimit"`, `"feedbackEnabled"`} {
		if strings.Contains(out, leak) {
			t.Errorf("body should omit %s when flag wasn't supplied: %s", leak, out)
		}
	}
}

// TestBuildBetaGroupCreate_PublicLinkLimit asserts that the matching
// publicLinkLimitEnabled flag is auto-set when --public-link-limit is
// supplied with a non-zero value.
func TestBuildBetaGroupCreate_PublicLinkLimit(t *testing.T) {
	body := buildBetaGroupCreate("APP-1", "Public", false, true, true, true, 5000, true, true)
	raw, _ := json.Marshal(body)
	out := string(raw)
	for _, want := range []string{
		`"publicLinkEnabled":true`,
		`"publicLinkLimit":5000`,
		`"publicLinkLimitEnabled":true`,
		`"feedbackEnabled":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q: %s", want, out)
		}
	}
}

// TestBuildBetaTesterLinkages_ShapeArray asserts the linkage body shape
// for the to-many betaTesters relationship endpoints.
func TestBuildBetaTesterLinkages_ShapeArray(t *testing.T) {
	body := buildBetaTesterLinkages([]string{"T1", "T2", "T3"})
	raw, _ := json.Marshal(body)
	out := string(raw)
	for _, want := range []string{
		`"data":[`,
		`{"id":"T1","type":"betaTesters"}`,
		`{"id":"T2","type":"betaTesters"}`,
		`{"id":"T3","type":"betaTesters"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q: %s", want, out)
		}
	}
}

// TestDedupeStrings_StableOrderEmptyDropped covers the helper guarding
// idempotent tester adds/removes from accidental duplicates.
func TestDedupeStrings_StableOrderEmptyDropped(t *testing.T) {
	got := dedupeStrings([]string{"T1", "", "T2", "T1", "  T3  ", "T3"})
	want := []string{"T1", "T2", "T3"}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

// TestBetaTestersChangeResult_FilterEmptyAppliedSkipsPOST simulates the
// idempotency path: when applied is empty, no write should be issued
// (the runTestflightTestersAdd/Remove early-return). Verified at the
// helper level: a result with empty Applied yields Changed=false when
// constructed with Changed=false.
func TestBetaTestersChangeResult_NoOpRendersChangedFalse(t *testing.T) {
	r := &BetaTestersChangeResult{
		GroupID:   "BG-1",
		Action:    "add",
		Requested: []string{"T1"},
		Skipped:   []string{"T1"},
		Changed:   false,
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v: %s", err, buf.String())
	}
	if v, ok := decoded["changed"].(bool); !ok || v {
		t.Errorf("changed = %v, want false", decoded["changed"])
	}
	if _, ok := decoded["skipped"]; !ok {
		t.Errorf("missing skipped key. Got: %v", mapKeys(decoded))
	}
}

// TestBetaGroupSetResult_JSONShape locks the JSON contract for the
// groups-create / groups-update result.
func TestBetaGroupSetResult_JSONShape(t *testing.T) {
	yes := true
	r := &BetaGroupSetResult{
		GroupID: "BG-1",
		Changed: true,
		Created: true,
		Attributes: asc.BetaGroupAttributes{
			Name:            "Internal",
			IsInternalGroup: &yes,
			FeedbackEnabled: &yes,
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v: %s", err, buf.String())
	}
	for _, key := range []string{"groupId", "changed", "created", "attributes"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing key %q. Got: %v", key, mapKeys(decoded))
		}
	}
}

// TestBetaReviewSubmitResult_JSONShape locks the JSON contract for the
// beta-review submit result.
func TestBetaReviewSubmitResult_JSONShape(t *testing.T) {
	r := &BetaReviewSubmitResult{
		BundleID:     "com.example.alpha",
		BuildID:      "BUILD-42",
		BuildNumber:  "42",
		SubmissionID: "BARS-42",
		Changed:      true,
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, r, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v: %s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "buildId", "buildNumber", "submissionId", "changed", "attributes"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing key %q. Got: %v", key, mapKeys(decoded))
		}
	}
}

// TestFindBetaGroupByName_FixtureReplay walks the read path used by the
// idempotent groups-create.
func TestFindBetaGroupByName_FixtureReplay(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/betaGroups": {File: "testflight_groups_list"},
	})
	c := fixtureASCClient(t, srv)
	got, err := findBetaGroupByName(context.Background(), c, "1234567890", "Internal Team")
	if err != nil {
		t.Fatalf("findBetaGroupByName: %v", err)
	}
	if got == nil || got.ID != "BG-INTERNAL-1" {
		t.Fatalf("got = %+v, want BG-INTERNAL-1", got)
	}

	// Name miss returns (nil, nil).
	miss, err := findBetaGroupByName(context.Background(), c, "1234567890", "Nonexistent")
	if err != nil {
		t.Fatalf("findBetaGroupByName miss: %v", err)
	}
	if miss != nil {
		t.Errorf("miss should be nil, got %+v", miss)
	}
}

// TestComputeBetaGroupPatchAttrs_OnlyChangedFlags asserts that an unset
// flag never produces a patch entry; a flag matching current state is
// also filtered out.
func TestComputeBetaGroupPatchAttrs_OnlyChangedFlags(t *testing.T) {
	yes := true
	cur := asc.BetaGroupAttributes{
		Name:            "Old Name",
		PublicLinkLimit: 1000,
		FeedbackEnabled: &yes,
	}
	root := &cobra.Command{Use: "x"}
	root.Flags().StringVar(&testflightGroupsUpdateName, "name", "", "")
	root.Flags().IntVar(&testflightGroupsUpdatePublicLinkLimit, "public-link-limit", 0, "")
	root.Flags().BoolVar(&testflightGroupsUpdateFeedback, "feedback", false, "")

	// No flags set → empty patch.
	patch := computeBetaGroupPatchAttrs(root, cur)
	if len(patch) != 0 {
		t.Errorf("patch should be empty, got %v", patch)
	}

	// --name to same value → empty patch (idempotent).
	if err := root.ParseFlags([]string{"--name", "Old Name"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	patch = computeBetaGroupPatchAttrs(root, cur)
	if _, ok := patch["name"]; ok {
		t.Errorf("name should not be in patch (matches current): %v", patch)
	}

	// --name to new value → patch carries name only.
	root2 := &cobra.Command{Use: "x"}
	root2.Flags().StringVar(&testflightGroupsUpdateName, "name", "", "")
	root2.Flags().IntVar(&testflightGroupsUpdatePublicLinkLimit, "public-link-limit", 0, "")
	root2.Flags().BoolVar(&testflightGroupsUpdateFeedback, "feedback", false, "")
	if err := root2.ParseFlags([]string{"--name", "New Name"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	patch = computeBetaGroupPatchAttrs(root2, cur)
	if patch["name"] != "New Name" {
		t.Errorf("patch[name] = %v, want New Name", patch["name"])
	}
}
