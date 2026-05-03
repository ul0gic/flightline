package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestReviewView_JSONShape(t *testing.T) {
	v := ReviewView{
		ID:   "REVIEW-001",
		Type: "customerReviews",
		Attributes: asc.CustomerReviewAttributes{
			Rating:           5,
			Title:            "Great",
			Body:             "Loved it",
			ReviewerNickname: "alpha",
			CreatedDate:      "2026-04-22T14:33:00Z",
			Territory:        "USA",
		},
		Response: &ReviewResponseView{
			ID:         "RESP-001",
			Type:       "customerReviewResponses",
			Attributes: asc.CustomerReviewResponseAttributes{State: "PUBLISHED", ResponseBody: "thanks"},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"REVIEW-001"`,
		`"type":"customerReviews"`,
		`"rating":5`,
		`"territory":"USA"`,
		`"reviewerNickname":"alpha"`,
		`"response":{`,
		`"state":"PUBLISHED"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestReviewList_TableRowsHeaders(t *testing.T) {
	list := ReviewList{
		Reviews: []ReviewView{
			{ID: "R1", Attributes: asc.CustomerReviewAttributes{Rating: 5, CreatedDate: "2026-04-22T14:33:00Z", Territory: "USA", Title: "Great"}},
			{ID: "R2", Attributes: asc.CustomerReviewAttributes{Rating: 1, CreatedDate: "2026-04-21T10:00:00Z", Territory: "GBR", Title: "Bad"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"RATING", "DATE", "TERRITORY", "TITLE", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if !strings.Contains(rows[0][0], "★") {
		t.Errorf("rows[0][0] (RATING) = %q, want stars", rows[0][0])
	}
	if rows[0][1] != "2026-04-22" {
		t.Errorf("rows[0][1] (DATE) = %q, want truncated to date", rows[0][1])
	}
}

func TestRenderStars(t *testing.T) {
	cases := map[int]string{
		1: "★☆☆☆☆",
		3: "★★★☆☆",
		5: "★★★★★",
	}
	for in, want := range cases {
		if got := renderStars(in); got != want {
			t.Errorf("renderStars(%d) = %q, want %q", in, got, want)
		}
	}
	// Out-of-range falls back to int literal so weird values are visible.
	if got := renderStars(0); got != "0" {
		t.Errorf("renderStars(0) = %q, want %q", got, "0")
	}
	if got := renderStars(7); got != "7" {
		t.Errorf("renderStars(7) = %q, want %q", got, "7")
	}
}

func TestParseRatingFilter(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		got, err := parseRatingFilter("3")
		if err != nil {
			t.Fatalf("parseRatingFilter: %v", err)
		}
		if len(got) != 1 || got[0] != "3" {
			t.Errorf("got %v, want [3]", got)
		}
	})
	t.Run("range", func(t *testing.T) {
		got, err := parseRatingFilter("1..3")
		if err != nil {
			t.Fatalf("parseRatingFilter: %v", err)
		}
		if strings.Join(got, ",") != "1,2,3" {
			t.Errorf("got %v, want [1 2 3]", got)
		}
	})
	t.Run("out_of_range", func(t *testing.T) {
		if _, err := parseRatingFilter("0"); err == nil {
			t.Error("expected error for rating=0")
		}
		if _, err := parseRatingFilter("6"); err == nil {
			t.Error("expected error for rating=6")
		}
		if _, err := parseRatingFilter("3..1"); err == nil {
			t.Error("expected error for inverted range")
		}
	})
	t.Run("non_numeric", func(t *testing.T) {
		if _, err := parseRatingFilter("five"); err == nil {
			t.Error("expected error for non-numeric")
		}
	})
}

func TestParseSince(t *testing.T) {
	t.Run("empty_returns_zero", func(t *testing.T) {
		got, err := parseSince("")
		if err != nil {
			t.Fatalf("parseSince: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("got %v, want zero time", got)
		}
	})
	t.Run("days", func(t *testing.T) {
		got, err := parseSince("30d")
		if err != nil {
			t.Fatalf("parseSince: %v", err)
		}
		want := time.Now().Add(-30 * 24 * time.Hour)
		// allow 5s slack for execution time
		if diff := got.Sub(want); diff < -5*time.Second || diff > 5*time.Second {
			t.Errorf("got %v, want ~%v (diff %v)", got, want, diff)
		}
	})
	t.Run("iso_date", func(t *testing.T) {
		got, err := parseSince("2026-04-01")
		if err != nil {
			t.Fatalf("parseSince: %v", err)
		}
		want, _ := time.Parse("2006-01-02", "2026-04-01")
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		if _, err := parseSince("garbage"); err == nil {
			t.Error("expected error for garbage input")
		}
	})
}

func TestReviewsCommand_RegisteredOnRoot(t *testing.T) {
	var rev *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "reviews" {
			rev = c
			break
		}
	}
	if rev == nil {
		t.Fatal("reviews not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range rev.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"list", "get", "summary"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("reviews subcommand %q missing", want)
		}
	}
}

// TestReviews_JSONOutputStability_List locks the ReviewList JSON shape.
func TestReviews_JSONOutputStability_List(t *testing.T) {
	list := ReviewList{
		Reviews: []ReviewView{
			{
				ID:   "R1",
				Type: "customerReviews",
				Attributes: asc.CustomerReviewAttributes{
					Rating: 5, Title: "x", Territory: "USA", CreatedDate: "2026-04-22T14:33:00Z",
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Reviews []map[string]any `json:"reviews"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Reviews) != 1 {
		t.Fatalf("reviews len = %d, want 1", len(decoded.Reviews))
	}
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := decoded.Reviews[0][key]; !ok {
			t.Errorf("missing per-row key %q — JSON contract drift", key)
		}
	}
}

// TestReviews_FixtureReplay_ListWithResponseInclude exercises the include
// decoding path: 3 reviews, only the first has a response inline.
func TestReviews_FixtureReplay_ListWithResponseInclude(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/customerReviews": {File: "reviews_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectReviews(ctx, c, "/v1/apps/"+appID+"/customerReviews", url.Values{"limit": {"200"}, "include": {"response"}}, 0, time.Time{})
	if err != nil {
		t.Fatalf("collectReviews: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("reviews len = %d, want 3", len(views))
	}
	if views[0].Response == nil {
		t.Errorf("reviews[0].Response should be populated from included")
	} else if views[0].Response.Attributes.State != "PUBLISHED" {
		t.Errorf("reviews[0].Response.State = %q, want PUBLISHED", views[0].Response.Attributes.State)
	}
	if views[1].Response != nil {
		t.Errorf("reviews[1].Response should be nil (data: null in fixture)")
	}
	if views[2].Response != nil {
		t.Errorf("reviews[2].Response should be nil (no relationship in fixture)")
	}
}

// TestReviews_FixtureReplay_GetWithResponse exercises the single-review get
// with response include.
func TestReviews_FixtureReplay_GetWithResponse(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/customerReviews/REVIEW-001": {File: "reviews_get"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	resp, err := asc.Get[asc.Single[asc.CustomerReviewAttributes]](
		ctx, c, "/v1/customerReviews/REVIEW-001", url.Values{"include": {"response"}},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Data.ID != "REVIEW-001" {
		t.Errorf("id = %q, want REVIEW-001", resp.Data.ID)
	}
	if resp.Data.Attributes.Rating != 5 {
		t.Errorf("rating = %d, want 5", resp.Data.Attributes.Rating)
	}
	rr, ok := decodeReviewResponseFromIncluded(resp.Included)
	if !ok {
		t.Fatal("decodeReviewResponseFromIncluded returned false; expected response")
	}
	if rr.Attributes.State != "PUBLISHED" {
		t.Errorf("response.state = %q, want PUBLISHED", rr.Attributes.State)
	}
}

// TestReviews_FixtureReplay_Summary exercises the summarizations endpoint.
func TestReviews_FixtureReplay_Summary(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/customerReviewSummarizations": {File: "reviews_summary"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	page, err := asc.Get[asc.Collection[asc.CustomerReviewSummarizationAttributes]](
		ctx, c, "/v1/apps/"+appID+"/customerReviewSummarizations", url.Values{"limit": {"200"}},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("summarizations len = %d, want 2", len(page.Data))
	}
	if page.Data[0].Attributes.Locale != "en-US" {
		t.Errorf("summarizations[0].Locale = %q, want en-US", page.Data[0].Attributes.Locale)
	}
}

// TestReviews_SinceShortCircuit asserts the since cutoff stops walking
// records older than the cutoff.
func TestReviews_SinceShortCircuit(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/customerReviews": {File: "reviews_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}

	// Cutoff between REVIEW-002 (2026-04-15) and REVIEW-003 (2026-04-01).
	cutoff, _ := time.Parse("2006-01-02", "2026-04-10")
	views, err := collectReviews(ctx, c, "/v1/apps/"+appID+"/customerReviews", url.Values{"limit": {"200"}}, 0, cutoff)
	if err != nil {
		t.Fatalf("collectReviews: %v", err)
	}
	if len(views) != 2 {
		t.Errorf("views len = %d, want 2 (REVIEW-001 and REVIEW-002 — REVIEW-003 should be cut off)", len(views))
	}
}
