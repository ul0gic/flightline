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

func TestReviewSubmissionView_JSONShape(t *testing.T) {
	v := ReviewSubmissionView{
		ID:   "rs-7700000001",
		Type: "reviewSubmissions",
		Attributes: asc.ReviewSubmissionAttributes{
			Platform:      "IOS",
			SubmittedDate: "2025-04-22T16:45:00-07:00",
			State:         "UNRESOLVED_ISSUES",
		},
	}
	b, _ := json.Marshal(v)
	out := string(b)
	for _, want := range []string{
		`"id":"rs-7700000001"`,
		`"type":"reviewSubmissions"`,
		`"platform":"IOS"`,
		`"submittedDate":"2025-04-22T16:45:00-07:00"`,
		`"state":"UNRESOLVED_ISSUES"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestReviewSubmissionList_TableRows(t *testing.T) {
	list := ReviewSubmissionList{
		Submissions: []ReviewSubmissionView{
			{ID: "rs-1", Attributes: asc.ReviewSubmissionAttributes{State: "UNRESOLVED_ISSUES", Platform: "IOS", SubmittedDate: "2025-04-22T16:45:00-07:00"}},
			{ID: "rs-2", Attributes: asc.ReviewSubmissionAttributes{State: "COMPLETE", Platform: "IOS", SubmittedDate: "2025-03-05T10:00:00-08:00"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"STATE", "PLATFORM", "SUBMITTED", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "UNRESOLVED_ISSUES" {
		t.Errorf("rows[0][0] = %q, want UNRESOLVED_ISSUES", rows[0][0])
	}
}

func TestReviewSubmissionItemList_TableRows(t *testing.T) {
	list := ReviewSubmissionItemList{
		Items: []ReviewSubmissionItemView{
			{ID: "rsi-1", Attributes: asc.ReviewSubmissionItemAttributes{State: "REJECTED"}, ReferenceType: "appStoreVersions", ReferenceID: "8000000001"},
			{ID: "rsi-2", Attributes: asc.ReviewSubmissionItemAttributes{State: "READY_FOR_REVIEW"}, ReferenceType: "appCustomProductPageVersions", ReferenceID: "cppv-1"},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"STATE", "REFERENCE_TYPE", "REFERENCE_ID", "ITEM_ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if rows[0][1] != "appStoreVersions" {
		t.Errorf("rows[0] reference type = %q, want appStoreVersions", rows[0][1])
	}
}

func TestReviewSubmissionsCommand_RegisteredOnRoot(t *testing.T) {
	var rsCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "review-submissions" {
			rsCmd = c
			break
		}
	}
	if rsCmd == nil {
		t.Fatal("review-submissions not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range rsCmd.Commands() {
		subs[sc.Name()] = true
	}
	for _, want := range []string{"list", "items"} {
		if !subs[want] {
			t.Errorf("review-submissions subcommand %q not registered", want)
		}
	}
}

func TestExtractItemReference_PicksFirstNonNullToOne(t *testing.T) {
	rels := map[string]asc.Relationship{
		"appStoreVersion": {Data: json.RawMessage(`{"type":"appStoreVersions","id":"8000000001"}`)},
		"appEvent":        {Data: json.RawMessage(`null`)},
	}
	refType, refID := extractItemReference(rels)
	if refType != "appStoreVersions" || refID != "8000000001" {
		t.Errorf("ref = (%q, %q), want (appStoreVersions, 8000000001)", refType, refID)
	}
}

func TestExtractItemReference_NoReferenceReturnsEmpty(t *testing.T) {
	rels := map[string]asc.Relationship{
		"appEvent": {Data: json.RawMessage(`null`)},
	}
	refType, refID := extractItemReference(rels)
	if refType != "" || refID != "" {
		t.Errorf("ref = (%q, %q), want both empty", refType, refID)
	}
}

func TestReviewSubmissions_JSONOutputStability_List(t *testing.T) {
	list := ReviewSubmissionList{Submissions: []ReviewSubmissionView{
		{
			ID:   "rs-7700000001",
			Type: "reviewSubmissions",
			Attributes: asc.ReviewSubmissionAttributes{
				Platform:      "IOS",
				SubmittedDate: "2025-04-22T16:45:00-07:00",
				State:         "UNRESOLVED_ISSUES",
			},
		},
	}}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Submissions []map[string]any `json:"submissions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if len(decoded.Submissions) != 1 {
		t.Fatalf("len = %d, want 1", len(decoded.Submissions))
	}
	row := decoded.Submissions[0]
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing key %q. Got %v", key, mapKeys(row))
		}
	}
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes is %T, want object", row["attributes"])
	}
	for _, key := range []string{"platform", "submittedDate", "state"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("missing attribute key %q. Got %v", key, mapKeys(attrs))
		}
	}
}

func TestReviewSubmissions_JSONOutputStability_Items(t *testing.T) {
	list := ReviewSubmissionItemList{Items: []ReviewSubmissionItemView{
		{
			ID:            "rsi-9900000001",
			Type:          "reviewSubmissionItems",
			Attributes:    asc.ReviewSubmissionItemAttributes{State: "REJECTED"},
			ReferenceType: "appStoreVersions",
			ReferenceID:   "8000000001",
		},
	}}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if len(decoded.Items) != 1 {
		t.Fatalf("len = %d, want 1", len(decoded.Items))
	}
	row := decoded.Items[0]
	for _, key := range []string{"id", "type", "attributes", "referenceType", "referenceId"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing key %q. Got %v", key, mapKeys(row))
		}
	}
}

func TestReviewSubmissions_FixtureReplay_List(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":              {File: "apps_get_byBundleId"},
		"GET /v1/reviewSubmissions": {File: "review_submissions_list"},
	})
	c := fixtureASCClient(t, srv)

	views, err := listReviewSubmissions(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("listReviewSubmissions: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("len = %d, want 2", len(views))
	}
	if views[0].Attributes.State != "UNRESOLVED_ISSUES" {
		t.Errorf("views[0].state = %q, want UNRESOLVED_ISSUES", views[0].Attributes.State)
	}
	if views[1].Attributes.State != "COMPLETE" {
		t.Errorf("views[1].state = %q, want COMPLETE", views[1].Attributes.State)
	}
}

func TestReviewSubmissions_FixtureReplay_Items(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/reviewSubmissions/rs-7700000001/items": {File: "review_submissions_items"},
	})
	c := fixtureASCClient(t, srv)

	views, err := listReviewSubmissionItems(context.Background(), c, "rs-7700000001")
	if err != nil {
		t.Fatalf("listReviewSubmissionItems: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("len = %d, want 2", len(views))
	}
	if views[0].Attributes.State != "REJECTED" {
		t.Errorf("views[0].state = %q, want REJECTED", views[0].Attributes.State)
	}
	if views[0].ReferenceType != "appStoreVersions" || views[0].ReferenceID != "8000000001" {
		t.Errorf("views[0] reference = (%q, %q), want (appStoreVersions, 8000000001)", views[0].ReferenceType, views[0].ReferenceID)
	}
	if views[1].ReferenceType != "appCustomProductPageVersions" {
		t.Errorf("views[1] reference type = %q, want appCustomProductPageVersions", views[1].ReferenceType)
	}
}
