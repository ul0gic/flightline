package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// ReviewSubmissionView is one row of the review-submissions list output.
type ReviewSubmissionView struct {
	ID         string                         `json:"id"`
	Type       string                         `json:"type"`
	Attributes asc.ReviewSubmissionAttributes `json:"attributes"`
}

// ReviewSubmissionList is the table-aware view for `review-submissions list`.
type ReviewSubmissionList struct {
	Submissions []ReviewSubmissionView `json:"submissions"`
}

// TableRows implements TableRenderable for the submissions list view.
func (l ReviewSubmissionList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"STATE", "PLATFORM", "SUBMITTED", "ID"}
	rows = make([][]string, 0, len(l.Submissions))
	for _, s := range l.Submissions {
		rows = append(rows, []string{
			s.Attributes.State,
			s.Attributes.Platform,
			s.Attributes.SubmittedDate,
			s.ID,
		})
	}
	return headers, rows
}

// ReviewSubmissionItemView is one item attached to a submission. The
// referenced resource type+id come from the JSON:API relationship; we
// flatten them so consumers don't have to parse the relationships block.
type ReviewSubmissionItemView struct {
	ID            string                             `json:"id"`
	Type          string                             `json:"type"`
	Attributes    asc.ReviewSubmissionItemAttributes `json:"attributes"`
	ReferenceType string                             `json:"referenceType,omitempty"`
	ReferenceID   string                             `json:"referenceId,omitempty"`
}

// ReviewSubmissionItemList is the table-aware view for items.
type ReviewSubmissionItemList struct {
	Items []ReviewSubmissionItemView `json:"items"`
}

// TableRows implements TableRenderable for the items view.
func (l ReviewSubmissionItemList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"STATE", "REFERENCE_TYPE", "REFERENCE_ID", "ITEM_ID"}
	rows = make([][]string, 0, len(l.Items))
	for _, it := range l.Items {
		rows = append(rows, []string{
			it.Attributes.State,
			it.ReferenceType,
			it.ReferenceID,
			it.ID,
		})
	}
	return headers, rows
}

var reviewSubmissionsCmd = &cobra.Command{
	Use:   "review-submissions",
	Short: "Inspect App Store review submissions (modern /v1/reviewSubmissions)",
	Long: `review-submissions reads from /v1/reviewSubmissions, the modern flow.
Apple's /v1/appStoreVersionSubmissions is deprecated; Flightline uses the
modern endpoint exclusively.`,
}

var reviewSubmissionsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List review submissions for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewSubmissionsList,
	Example: `  fline review-submissions list com.example.myapp
  fline review-submissions list com.example.myapp --output json | jq -r '.submissions[].attributes.state'`,
}

var reviewSubmissionsItemsCmd = &cobra.Command{
	Use:          "items <bundleId>",
	Short:        "List items in a review submission",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewSubmissionsItems,
	Example: `  fline review-submissions items com.example.myapp --submission abc123
  fline review-submissions items com.example.myapp --submission abc123 --output json`,
}

var reviewSubmissionsItemsID string

func init() {
	reviewSubmissionsItemsCmd.Flags().StringVar(&reviewSubmissionsItemsID, "submission", "", "review submission ID (from `review-submissions list`)")
	_ = reviewSubmissionsItemsCmd.MarkFlagRequired("submission")

	reviewSubmissionsCmd.AddCommand(reviewSubmissionsListCmd)
	reviewSubmissionsCmd.AddCommand(reviewSubmissionsItemsCmd)
	rootCmd.AddCommand(reviewSubmissionsCmd)
}

func runReviewSubmissionsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}

	views, err := listReviewSubmissions(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	return Render(ReviewSubmissionList{Submissions: views}, outputMode())
}

func runReviewSubmissionsItems(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	submissionID := strings.TrimSpace(reviewSubmissionsItemsID)
	if submissionID == "" {
		return fmt.Errorf("review-submissions: --submission is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	// Resolve bundleId so we surface a useful error if the bundle is unknown
	// before hitting the items endpoint with a possibly-stale submission ID.
	if _, err := resolveAppID(cmd.Context(), c, bundleID); err != nil {
		return err
	}

	views, err := listReviewSubmissionItems(cmd.Context(), c, submissionID)
	if err != nil {
		return err
	}
	return Render(ReviewSubmissionItemList{Items: views}, outputMode())
}

// listReviewSubmissions fetches every review submission for the app
// identified by bundleId. The /v1/reviewSubmissions endpoint requires
// filter[app] (per spec: required=true), so we resolve bundleId → appId
// before querying.
func listReviewSubmissions(ctx context.Context, c *asc.Client, bundleID string) ([]ReviewSubmissionView, error) {
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return nil, err
	}

	q := url.Values{
		"filter[app]": {appID},
		"limit":       {"200"},
	}
	out := make([]ReviewSubmissionView, 0, 8)
	for page, err := range asc.Pages[asc.ReviewSubmissionAttributes](ctx, c, "/v1/reviewSubmissions", q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, ReviewSubmissionView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
		}
	}
	return out, nil
}

// listReviewSubmissionItems fetches the items in a given review submission.
// We hit /v1/reviewSubmissions/{id}/items directly — Apple's documented
// to-many-related endpoint — and parse the JSON:API relationships block on
// each item to flatten the (type, id) reference for table display.
func listReviewSubmissionItems(ctx context.Context, c *asc.Client, submissionID string) ([]ReviewSubmissionItemView, error) {
	q := url.Values{"limit": {"200"}}
	path := "/v1/reviewSubmissions/" + submissionID + "/items"

	out := make([]ReviewSubmissionItemView, 0, 8)
	for page, err := range asc.Pages[asc.ReviewSubmissionItemAttributes](ctx, c, path, q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			refType, refID := extractItemReference(r.Relationships)
			out = append(out, ReviewSubmissionItemView{
				ID:            r.ID,
				Type:          r.Type,
				Attributes:    r.Attributes,
				ReferenceType: refType,
				ReferenceID:   refID,
			})
		}
	}
	return out, nil
}

// extractItemReference walks the relationships map looking for the first
// to-one relationship with a non-null data block, which is the resource
// the item is requesting review for (one of: appStoreVersion,
// appCustomProductPageVersion, appStoreVersionExperiment, appEvent, etc.).
//
// Apple guarantees an item has exactly one such reference.
func extractItemReference(rels map[string]asc.Relationship) (refType, refID string) {
	for _, rel := range rels {
		if len(rel.Data) == 0 || string(rel.Data) == "null" {
			continue
		}
		var ref struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(rel.Data, &ref); err != nil {
			continue
		}
		if ref.Type != "" || ref.ID != "" {
			return ref.Type, ref.ID
		}
	}
	return "", ""
}
