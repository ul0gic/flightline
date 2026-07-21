package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestRejectionReport_TableRows_FullReport(t *testing.T) {
	report := RejectionReport{
		BundleID: "com.example.alpha",
		Version: RejectionVersion{
			ID:            "8000000001",
			VersionString: "1.0.1",
			Platform:      "IOS",
			State:         "REJECTED",
			ReleaseType:   "MANUAL",
			BuildID:       "9000000001",
			BuildVersion:  "42",
			BuildState:    "VALID",
		},
		Submission: &RejectionSubmission{
			ID:            "rs-7700000001",
			State:         "UNRESOLVED_ISSUES",
			Platform:      "IOS",
			SubmittedDate: "2025-04-22T16:45:00-07:00",
			Items: []RejectionItem{
				{ID: "rsi-1", State: "REJECTED", ReferenceType: "appStoreVersions", ReferenceID: "8000000001"},
			},
		},
		Note: resolutionCenterDisclaimer,
	}
	headers, rows := report.TableRows()
	if len(headers) != 2 {
		t.Errorf("headers = %d, want 2", len(headers))
	}
	// Spot-check critical rows.
	gotKeys := map[string]string{}
	for _, r := range rows {
		gotKeys[r[0]] = r[1]
	}
	for _, want := range []string{"BUNDLE_ID", "VERSION", "VERSION_STATE", "BUILD_STATE", "SUBMISSION_ID", "SUBMISSION_STATE", "ITEM_1_STATE"} {
		if _, ok := gotKeys[want]; !ok {
			t.Errorf("missing row %q. Got rows: %v", want, gotKeys)
		}
	}
	if gotKeys["VERSION_STATE"] != "REJECTED" {
		t.Errorf("VERSION_STATE = %q, want REJECTED", gotKeys["VERSION_STATE"])
	}
	if gotKeys["SUBMISSION_STATE"] != "UNRESOLVED_ISSUES" {
		t.Errorf("SUBMISSION_STATE = %q, want UNRESOLVED_ISSUES", gotKeys["SUBMISSION_STATE"])
	}
}

func TestRejectionReport_TableRows_NoBuildNoSubmission(t *testing.T) {
	report := RejectionReport{
		BundleID: "com.example.alpha",
		Version: RejectionVersion{
			ID:            "8000000001",
			VersionString: "1.0.1",
			Platform:      "IOS",
			State:         "PREPARE_FOR_SUBMISSION",
			ReleaseType:   "MANUAL",
		},
		Note: resolutionCenterDisclaimer,
	}
	_, rows := report.TableRows()
	gotKeys := map[string]string{}
	for _, r := range rows {
		gotKeys[r[0]] = r[1]
	}
	if gotKeys["BUILD"] != "<none attached>" {
		t.Errorf("BUILD row = %q, want <none attached>", gotKeys["BUILD"])
	}
	if gotKeys["SUBMISSION"] != "<none found referencing this version>" {
		t.Errorf("SUBMISSION row = %q, want <none found referencing this version>", gotKeys["SUBMISSION"])
	}
}

func TestRejection_DisclaimerInJSON(t *testing.T) {
	report := RejectionReport{
		BundleID: "com.example.alpha",
		Version:  RejectionVersion{VersionString: "1.0.1"},
		Note:     resolutionCenterDisclaimer,
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, report, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"note"`) {
		t.Errorf("json missing .note field: %q", out)
	}
	if !strings.Contains(out, "Apple's resolution-center reviewer text is NOT in the public API") {
		t.Errorf("note value missing required disclaimer text: %q", out)
	}
}

func TestRejectionCommand_RegisteredOnRoot(t *testing.T) {
	var found *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "rejection" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("rejection not registered on rootCmd")
	}
	// --version flag must exist and be required.
	versionFlag := found.Flags().Lookup("version")
	if versionFlag == nil {
		t.Fatal("rejection --version flag missing")
	}
	platformFlag := found.Flags().Lookup("platform")
	if platformFlag == nil {
		t.Fatal("rejection --platform flag missing")
	}
	if platformFlag.DefValue != "IOS" {
		t.Errorf("rejection --platform default = %q, want IOS", platformFlag.DefValue)
	}
}

func TestItemReferencesVersion(t *testing.T) {
	items := []ReviewSubmissionItemView{
		{ID: "1", ReferenceType: "appStoreVersions", ReferenceID: "8000000001"},
		{ID: "2", ReferenceType: "appCustomProductPageVersions", ReferenceID: "cppv-1"},
	}
	if !itemReferencesVersion(items, "8000000001") {
		t.Error("expected match for version 8000000001")
	}
	if itemReferencesVersion(items, "cppv-1") {
		t.Error("custom product page ID should not match (wrong reference type)")
	}
	if itemReferencesVersion(items, "9999999999") {
		t.Error("unrelated version ID should not match")
	}
}

// TestRejection_JSONOutputStability locks the report shape contract.
func TestRejection_JSONOutputStability(t *testing.T) {
	report := RejectionReport{
		BundleID: "com.example.alpha",
		Version: RejectionVersion{
			ID:              "8000000001",
			VersionString:   "1.0.1",
			Platform:        "IOS",
			State:           "REJECTED",
			AppStoreState:   "REJECTED",
			AppVersionState: "REJECTED",
			ReleaseType:     "MANUAL",
			BuildID:         "9000000001",
			BuildVersion:    "42",
			BuildState:      "VALID",
		},
		Submission: &RejectionSubmission{
			ID:            "rs-7700000001",
			State:         "UNRESOLVED_ISSUES",
			Platform:      "IOS",
			SubmittedDate: "2025-04-22T16:45:00-07:00",
			Items: []RejectionItem{
				{ID: "rsi-1", State: "REJECTED", ReferenceType: "appStoreVersions", ReferenceID: "8000000001"},
			},
		},
		Note: resolutionCenterDisclaimer,
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, report, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"bundleId", "version", "submission", "note"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q. Got: %v", key, mapKeys(decoded))
		}
	}
	v, ok := decoded["version"].(map[string]any)
	if !ok {
		t.Fatalf("version is %T, want object", decoded["version"])
	}
	for _, key := range []string{"id", "versionString", "platform", "state", "appStoreState", "appVersionState", "releaseType", "buildId", "buildVersion", "buildState"} {
		if _, ok := v[key]; !ok {
			t.Errorf("missing version key %q. Got: %v", key, mapKeys(v))
		}
	}
	s, ok := decoded["submission"].(map[string]any)
	if !ok {
		t.Fatalf("submission is %T, want object", decoded["submission"])
	}
	for _, key := range []string{"id", "state", "platform", "submittedDate", "items"} {
		if _, ok := s[key]; !ok {
			t.Errorf("missing submission key %q. Got: %v", key, mapKeys(s))
		}
	}
}

// TestRejection_FixtureReplay_FullCompose verifies the multi-call orchestrator
// produces the expected composed report against the fixture corpus.
func TestRejection_FixtureReplay_FullCompose(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":      {File: "rejection_version_without_build_relationship"},
		"GET /v1/appStoreVersions/8000000001/build":     {File: "rejection_build_single"},
		"GET /v1/reviewSubmissions":                     {File: "review_submissions_list"},
		"GET /v1/reviewSubmissions/rs-7700000001/items": {File: "review_submissions_items"},
	})
	c := fixtureASCClient(t, srv)

	report, err := composeRejectionReport(context.Background(), c, "com.example.alpha", "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("composeRejectionReport: %v", err)
	}

	if report.BundleID != "com.example.alpha" {
		t.Errorf("bundleId = %q, want com.example.alpha", report.BundleID)
	}
	if report.Version.ID != "8000000001" {
		t.Errorf("version.ID = %q, want 8000000001", report.Version.ID)
	}
	if report.Version.State != "REJECTED" {
		t.Errorf("version.State = %q, want REJECTED", report.Version.State)
	}
	if report.Version.BuildID != "9000000001" {
		t.Errorf("version.BuildID = %q, want 9000000001", report.Version.BuildID)
	}
	if report.Version.BuildVersion != "42" {
		t.Errorf("version.BuildVersion = %q, want 42 (from build single endpoint)", report.Version.BuildVersion)
	}
	if report.Version.BuildState != "VALID" {
		t.Errorf("version.BuildState = %q, want VALID", report.Version.BuildState)
	}
	if report.Submission == nil {
		t.Fatal("submission is nil; expected match against rs-7700000001 via item reference")
	}
	if report.Submission.ID != "rs-7700000001" {
		t.Errorf("submission.ID = %q, want rs-7700000001", report.Submission.ID)
	}
	if report.Submission.State != "UNRESOLVED_ISSUES" {
		t.Errorf("submission.State = %q, want UNRESOLVED_ISSUES", report.Submission.State)
	}
	if len(report.Submission.Items) != 2 {
		t.Errorf("submission items len = %d, want 2", len(report.Submission.Items))
	}
	if report.Submission.Items[0].State != "REJECTED" {
		t.Errorf("first item state = %q, want REJECTED", report.Submission.Items[0].State)
	}
	if !strings.Contains(report.Note, "resolution-center reviewer text is NOT in the public API") {
		t.Errorf("note missing required disclaimer: %q", report.Note)
	}
}

func TestFetchRejectionVersionBuild_NotAttached(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/build": {File: "iap_get_notFound", Status: 404},
	})
	c := fixtureASCClient(t, srv)

	_, attached, err := fetchRejectionVersionBuild(context.Background(), c, "8000000001")
	if err != nil {
		t.Fatalf("fetchRejectionVersionBuild: %v", err)
	}
	if attached {
		t.Fatal("attached = true, want false for 404")
	}
}

// TestRejection_FixtureReplay_VersionNotFound asserts the typed not-found
// error fires when the version filter yields zero records.
func TestRejection_FixtureReplay_VersionNotFound(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_get_notFound"},
	})
	c := fixtureASCClient(t, srv)

	_, err := composeRejectionReport(context.Background(), c, "com.example.alpha", "9.9.9", "IOS")
	if err == nil {
		t.Fatal("want error for missing version")
	}
	msg := err.Error()
	for _, want := range []string{"rejection:", "no version", `"9.9.9"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("err.Error() = %q, missing %q", msg, want)
		}
	}
}

// TestRelationshipID_HandlesPresentMissingNull covers the three wire shapes:
// populated to-one, absent key, and explicit null Data.
func TestRelationshipID_HandlesPresentMissingNull(t *testing.T) {
	rels := map[string]asc.Relationship{
		"build":                     {Data: json.RawMessage(`{"type":"builds","id":"9000000001"}`)},
		"appStoreVersionSubmission": {Data: json.RawMessage(`null`)},
	}
	if got := relationshipID(rels, "build"); got != "9000000001" {
		t.Errorf("relationshipID(build) = %q, want 9000000001", got)
	}
	if got := relationshipID(rels, "appStoreVersionSubmission"); got != "" {
		t.Errorf("relationshipID(null relationship) = %q, want empty", got)
	}
	if got := relationshipID(rels, "missing"); got != "" {
		t.Errorf("relationshipID(missing relationship) = %q, want empty", got)
	}
}
