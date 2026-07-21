package cmd

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ReviewSubmissionView struct {
	ID         string                         `json:"id"`
	Type       string                         `json:"type"`
	Attributes asc.ReviewSubmissionAttributes `json:"attributes"`
}

type ReviewSubmissionList struct {
	Submissions []ReviewSubmissionView `json:"submissions"`
}

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

// ReviewSubmissionItemView flattens the referenced resource type+id out of the
// JSON:API relationships block.
type ReviewSubmissionItemView struct {
	ID            string                             `json:"id"`
	Type          string                             `json:"type"`
	Attributes    asc.ReviewSubmissionItemAttributes `json:"attributes"`
	ReferenceType string                             `json:"referenceType,omitempty"`
	ReferenceID   string                             `json:"referenceId,omitempty"`
}

type ReviewSubmissionItemList struct {
	Items []ReviewSubmissionItemView `json:"items"`
}

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
	Example: `  flightline review-submissions list com.example.myapp
  flightline review-submissions list com.example.myapp --output json | jq -r '.submissions[].attributes.state'`,
}

var reviewSubmissionsItemsCmd = &cobra.Command{
	Use:          "items <bundleId>",
	Short:        "List items in a review submission",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewSubmissionsItems,
	Example: `  flightline review-submissions items com.example.myapp --submission abc123
  flightline review-submissions items com.example.myapp --submission abc123 --output json`,
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
		return errors.New("review-submissions: --submission is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	// Surface an unknown-bundle error before hitting the items endpoint.
	if _, err := resolveAppID(cmd.Context(), c, bundleID); err != nil {
		return err
	}

	views, err := listReviewSubmissionItems(cmd.Context(), c, submissionID)
	if err != nil {
		return err
	}
	return Render(ReviewSubmissionItemList{Items: views}, outputMode())
}

// listReviewSubmissions fetches every review submission for the app.
// /v1/reviewSubmissions requires filter[app], so bundleId is resolved first.
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

// listReviewSubmissionItems fetches a submission's items and flattens each
// item's (type, id) reference out of its relationships block.
func listReviewSubmissionItems(ctx context.Context, c *asc.Client, submissionID string) ([]ReviewSubmissionItemView, error) {
	// Relationship data blocks are only populated when the target is included; without this the reference is invisible.
	q := url.Values{
		"limit": {"200"},
		// appStoreVersionExperiment (v1) is omitted: Apple 400s when both experiment generations are included together.
		"include": {"appStoreVersion,appCustomProductPageVersion,appStoreVersionExperimentV2,appEvent,backgroundAssetVersion"},
	}
	path := "/v1/reviewSubmissions/" + submissionID + "/items"

	out := make([]ReviewSubmissionItemView, 0, 8)
	for page, err := range asc.Pages[asc.ReviewSubmissionItemAttributes](ctx, c, path, q) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			ref := asc.ResolveReviewSubmissionItemReference(r.ID, submissionID, r.Relationships)
			out = append(out, ReviewSubmissionItemView{
				ID:            r.ID,
				Type:          r.Type,
				Attributes:    r.Attributes,
				ReferenceType: ref.Type,
				ReferenceID:   ref.ID,
			})
		}
	}
	return out, nil
}

// extractItemReference returns the item's single non-null to-one reference:
// the resource it requests review for (appStoreVersion, appEvent, etc.).
func extractItemReference(rels map[string]asc.Relationship) (refType, refID string) {
	ref := asc.ResolveReviewSubmissionItemReference("", "", rels)
	if ref.Opaque {
		return "", ""
	}
	return ref.Type, ref.ID
}
