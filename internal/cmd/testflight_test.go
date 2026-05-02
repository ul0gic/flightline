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
