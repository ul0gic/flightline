package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// ReviewView is one row of the reviews list/get output.
type ReviewView struct {
	ID         string                       `json:"id"`
	Type       string                       `json:"type"`
	Attributes asc.CustomerReviewAttributes `json:"attributes"`
	Response   *ReviewResponseView          `json:"response,omitempty"`
}

// ReviewResponseView is the developer's response to a customer review.
type ReviewResponseView struct {
	ID         string                               `json:"id"`
	Type       string                               `json:"type"`
	Attributes asc.CustomerReviewResponseAttributes `json:"attributes"`
}

// ReviewList is the table-aware view for `reviews list`.
type ReviewList struct {
	Reviews []ReviewView `json:"reviews"`
}

func (l ReviewList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"RATING", "DATE", "TERRITORY", "TITLE", "ID"}
	rows = make([][]string, 0, len(l.Reviews))
	for i := range l.Reviews {
		r := &l.Reviews[i]
		rows = append(rows, []string{
			renderStars(r.Attributes.Rating),
			truncDate(r.Attributes.CreatedDate),
			r.Attributes.Territory,
			truncTitle(r.Attributes.Title, 60),
			r.ID,
		})
	}
	return headers, rows
}

func (v *ReviewView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", v.ID},
		{"RATING", renderStars(v.Attributes.Rating)},
		{"TITLE", v.Attributes.Title},
		{"REVIEWER", v.Attributes.ReviewerNickname},
		{"TERRITORY", v.Attributes.Territory},
		{"CREATED_DATE", v.Attributes.CreatedDate},
		{"BODY", v.Attributes.Body},
	}
	if v.Response != nil {
		rows = append(rows,
			[]string{"RESPONSE_ID", v.Response.ID},
			[]string{"RESPONSE_STATE", v.Response.Attributes.State},
			[]string{"RESPONSE_DATE", v.Response.Attributes.LastModifiedDate},
			[]string{"RESPONSE_BODY", v.Response.Attributes.ResponseBody},
		)
	}
	return headers, rows
}

// renderStars renders a 1..5 rating as filled/empty stars. Out-of-range
// values render the integer so an unexpected value isn't silently clamped.
func renderStars(n int) string {
	if n < 1 || n > 5 {
		return strconv.Itoa(n)
	}
	const filled = "★"
	const empty = "☆"
	return strings.Repeat(filled, n) + strings.Repeat(empty, 5-n)
}

// truncDate returns the YYYY-MM-DD prefix of an ISO-8601 timestamp.
func truncDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// truncTitle truncates s to maxRunes with an ellipsis, operating on runes so
// multi-byte titles aren't chopped mid-codepoint.
func truncTitle(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}

// ReviewSummaryView is the read-side view for `reviews summary`.
type ReviewSummaryView struct {
	BundleID       string                    `json:"bundleId"`
	Summarizations []ReviewSummarizationItem `json:"summarizations"`
	Note           string                    `json:"note,omitempty"`
}

// ReviewSummarizationItem wraps one summarization resource.
type ReviewSummarizationItem struct {
	ID         string                                    `json:"id"`
	Type       string                                    `json:"type"`
	Attributes asc.CustomerReviewSummarizationAttributes `json:"attributes"`
}

// TableRows for the reviews summary view.
func (v *ReviewSummaryView) TableRows() (headers []string, rows [][]string) {
	if v.Note != "" && len(v.Summarizations) == 0 {
		return []string{"FIELD", "VALUE"}, [][]string{
			{"BUNDLE_ID", v.BundleID},
			{"NOTE", v.Note},
		}
	}
	headers = []string{"PLATFORM", "LOCALE", "DATE", "SUMMARY"}
	rows = make([][]string, 0, len(v.Summarizations))
	for i := range v.Summarizations {
		s := &v.Summarizations[i]
		rows = append(rows, []string{
			s.Attributes.Platform,
			s.Attributes.Locale,
			truncDate(s.Attributes.CreatedDate),
			truncTitle(s.Attributes.Text, 80),
		})
	}
	return headers, rows
}

var reviewsCmd = &cobra.Command{
	Use:   "reviews",
	Short: "Read App Store customer reviews and Apple's AI summaries",
	Long: `reviews groups read commands over Apple's customer-review surface:

  - list <bundleId>    : list reviews with optional territory/rating/since filters
  - get <reviewId>     : fetch a single review with the developer response (if any)
  - summary <bundleId> : read Apple's AI summarization of recent reviews`,
}

var reviewsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List customer reviews for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewsList,
	Example: `  flightline reviews list com.example.myapp
  flightline reviews list com.example.myapp --territory USA --rating 1..3
  flightline reviews list com.example.myapp --since 30d --output json | jq '.reviews[].attributes.body'`,
}

var reviewsGetCmd = &cobra.Command{
	Use:          "get <reviewId>",
	Short:        "Get a single customer review with the developer response (if any)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewsGet,
	Example: `  flightline reviews get 6e2b9b14-1234-4567-8910-abcdef012345
  flightline reviews get 6e2b9b14-1234-4567-8910-abcdef012345 --output json`,
}

var reviewsSummaryCmd = &cobra.Command{
	Use:          "summary <bundleId>",
	Short:        "Read Apple's per-locale AI summary of recent reviews",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewsSummary,
	Example: `  flightline reviews summary com.example.myapp
  flightline reviews summary com.example.myapp --output json | jq '.summarizations[].attributes.text'`,
}

var (
	reviewsListTerritory string
	reviewsListRating    string
	reviewsListSince     string
	reviewsListLimit     int
)

func init() {
	reviewsListCmd.Flags().StringVar(&reviewsListTerritory, "territory", "", "filter by ISO 3166-1 alpha-3 territory (e.g. USA, GBR); empty = all")
	reviewsListCmd.Flags().StringVar(&reviewsListRating, "rating", "", "filter by rating: single (e.g. 1) or range (e.g. 1..3); empty = all")
	reviewsListCmd.Flags().StringVar(&reviewsListSince, "since", "", "only reviews newer than this duration (e.g. 30d, 7d) or ISO date (2026-04-01)")
	reviewsListCmd.Flags().IntVar(&reviewsListLimit, "limit", 0, "max reviews to emit (0 = no cap)")

	reviewsCmd.AddCommand(reviewsListCmd)
	reviewsCmd.AddCommand(reviewsGetCmd)
	reviewsCmd.AddCommand(reviewsSummaryCmd)
	rootCmd.AddCommand(reviewsCmd)
}

func runReviewsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	q := url.Values{
		"limit":   {"200"},
		"include": {"response"},
	}
	if t := strings.TrimSpace(reviewsListTerritory); t != "" {
		q.Set("filter[territory]", t)
	}
	if r := strings.TrimSpace(reviewsListRating); r != "" {
		ratings, err := parseRatingFilter(r)
		if err != nil {
			return err
		}
		// Apple's filter[rating] takes a comma-separated list of ints.
		q.Set("filter[rating]", strings.Join(ratings, ","))
	}

	since, err := parseSince(reviewsListSince)
	if err != nil {
		return err
	}

	views, err := collectReviews(cmd.Context(), c, "/v1/apps/"+appID+"/customerReviews", q, reviewsListLimit, since)
	if err != nil {
		return err
	}
	return Render(ReviewList{Reviews: views}, outputMode())
}

func runReviewsGet(cmd *cobra.Command, args []string) error {
	reviewID := strings.TrimSpace(args[0])
	if reviewID == "" {
		return errors.New("reviews: review id is required")
	}
	c, err := newClient()
	if err != nil {
		return err
	}

	resp, err := asc.Get[asc.Single[asc.CustomerReviewAttributes]](
		cmd.Context(), c, "/v1/customerReviews/"+reviewID, url.Values{"include": {"response"}},
	)
	if err != nil {
		return err
	}
	if resp.Data.ID == "" {
		return fmt.Errorf("reviews: no review found with id %q", reviewID)
	}

	view := &ReviewView{
		ID:         resp.Data.ID,
		Type:       resp.Data.Type,
		Attributes: resp.Data.Attributes,
	}
	if rr, ok := decodeReviewResponseFromIncluded(resp.Included); ok {
		view.Response = rr
	}
	return Render(view, outputMode())
}

func runReviewsSummary(cmd *cobra.Command, args []string) error {
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
	view := &ReviewSummaryView{BundleID: bundleID, Summarizations: []ReviewSummarizationItem{}}

	for page, err := range asc.Pages[asc.CustomerReviewSummarizationAttributes](
		cmd.Context(), c, "/v1/apps/"+appID+"/customerReviewSummarizations", q,
	) {
		if err != nil {
			// Apple 404s this endpoint for apps not enrolled in summaries;
			// surface a typed note so callers see a stable shape, not a fatal.
			view.Note = "no review summarizations available for this app (Apple may not have generated one yet, or the feature is not enabled)"
			return Render(view, outputMode())
		}
		for _, r := range page.Data {
			view.Summarizations = append(view.Summarizations, ReviewSummarizationItem{
				ID:         r.ID,
				Type:       r.Type,
				Attributes: r.Attributes,
			})
		}
	}
	if len(view.Summarizations) == 0 {
		view.Note = "no review summarizations available for this app yet"
	}
	return Render(view, outputMode())
}

// parseRatingFilter accepts "3" or "1..3" and returns the comma-separated
// integer list Apple's filter[rating] expects.
func parseRatingFilter(in string) ([]string, error) {
	in = strings.TrimSpace(in)
	if i := strings.Index(in, ".."); i >= 0 {
		lo, err := strconv.Atoi(strings.TrimSpace(in[:i]))
		if err != nil {
			return nil, fmt.Errorf("reviews: --rating range %q: lower bound is not numeric", in)
		}
		hi, err := strconv.Atoi(strings.TrimSpace(in[i+2:]))
		if err != nil {
			return nil, fmt.Errorf("reviews: --rating range %q: upper bound is not numeric", in)
		}
		if lo < 1 || hi > 5 || lo > hi {
			return nil, fmt.Errorf("reviews: --rating range %q is out of bounds (valid: 1..5, lo<=hi)", in)
		}
		out := make([]string, 0, hi-lo+1)
		for n := lo; n <= hi; n++ {
			out = append(out, strconv.Itoa(n))
		}
		return out, nil
	}
	n, err := strconv.Atoi(in)
	if err != nil {
		return nil, fmt.Errorf("reviews: --rating %q is not numeric", in)
	}
	if n < 1 || n > 5 {
		return nil, fmt.Errorf("reviews: --rating %d is out of bounds (valid: 1..5)", n)
	}
	return []string{strconv.Itoa(n)}, nil
}

// parseSince accepts a duration ("30d"/"12h") or ISO date ("2026-04-01") and
// returns the cutoff. Empty returns zero time, which collectReviews treats as no filter.
func parseSince(in string) (time.Time, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", in); err == nil {
		return t, nil
	}
	// time.ParseDuration doesn't know "d", but operators expect it for windows.
	if strings.HasSuffix(in, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(in, "d"))
		if err != nil || days < 0 {
			return time.Time{}, fmt.Errorf("reviews: --since %q: not a valid day count", in)
		}
		return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
	}
	d, err := time.ParseDuration(in)
	if err != nil {
		return time.Time{}, fmt.Errorf("reviews: --since %q: not a valid duration or ISO date", in)
	}
	return time.Now().Add(-d), nil
}

// collectReviews applies the since cutoff client-side (Apple has no created-since
// filter); newest-first sorting lets the loop stop at the first too-old record.
func collectReviews(ctx context.Context, c *asc.Client, path string, query url.Values, limit int, since time.Time) ([]ReviewView, error) {
	out := make([]ReviewView, 0, defaultListCap(limit))
	if query.Get("sort") == "" {
		query.Set("sort", "-createdDate")
	}
	for page, err := range asc.Pages[asc.CustomerReviewAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		responses := decodeReviewResponseMap(page.Included)
		for _, r := range page.Data {
			if olderThan(r.Attributes.CreatedDate, since) {
				return out, nil
			}
			out = append(out, reviewToView(r, responses))
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// olderThan reports whether createdDate predates the cutoff. A zero cutoff or
// unparseable date is never "older", so the record is kept.
func olderThan(createdDate string, since time.Time) bool {
	if since.IsZero() {
		return false
	}
	t, ok := parseISO(createdDate)
	return ok && t.Before(since)
}

func reviewToView(r asc.Resource[asc.CustomerReviewAttributes], responses map[string]*ReviewResponseView) ReviewView {
	view := ReviewView{ID: r.ID, Type: r.Type, Attributes: r.Attributes}
	if respID := relationshipID(r.Relationships, "response"); respID != "" {
		if rr, ok := responses[respID]; ok {
			view.Response = rr
		}
	}
	return view
}

// parseISO parses an Apple ISO-8601 timestamp, returning zero+false on failure.
func parseISO(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// decodeReviewResponseMap returns id → ReviewResponseView for every
// customerReviewResponses entry in the Included array.
func decodeReviewResponseMap(included []json.RawMessage) map[string]*ReviewResponseView {
	out := make(map[string]*ReviewResponseView, len(included))
	for _, raw := range included {
		var head struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		if head.Type != "customerReviewResponses" || head.ID == "" {
			continue
		}
		var full struct {
			ID         string                               `json:"id"`
			Type       string                               `json:"type"`
			Attributes asc.CustomerReviewResponseAttributes `json:"attributes"`
		}
		if err := json.Unmarshal(raw, &full); err != nil {
			continue
		}
		out[head.ID] = &ReviewResponseView{
			ID:         full.ID,
			Type:       full.Type,
			Attributes: full.Attributes,
		}
	}
	return out
}

// decodeReviewResponseFromIncluded returns the first customerReviewResponses
// entry; the single-review get path includes at most one response.
func decodeReviewResponseFromIncluded(included []json.RawMessage) (*ReviewResponseView, bool) {
	m := decodeReviewResponseMap(included)
	for _, v := range m {
		return v, true
	}
	return nil, false
}
