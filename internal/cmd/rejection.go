package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// Apple does not expose resolution-center reviewer prose via the public API; surfacing this loudly is the command's whole point.
const resolutionCenterDisclaimer = `Apple's resolution-center reviewer text is NOT in the public API. Flightline shows the API-visible state. To read the actual reviewer message, log into App Store Connect.`

type RejectionItem struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	ReferenceType string `json:"referenceType,omitempty"`
	ReferenceID   string `json:"referenceId,omitempty"`
}

type RejectionSubmission struct {
	ID            string          `json:"id"`
	State         string          `json:"state"`
	Platform      string          `json:"platform,omitempty"`
	SubmittedDate string          `json:"submittedDate,omitempty"`
	Items         []RejectionItem `json:"items"`
}

type RejectionVersion struct {
	ID              string `json:"id"`
	VersionString   string `json:"versionString"`
	Platform        string `json:"platform,omitempty"`
	State           string `json:"state,omitempty"`
	AppStoreState   string `json:"appStoreState,omitempty"`
	AppVersionState string `json:"appVersionState,omitempty"`
	ReleaseType     string `json:"releaseType,omitempty"`
	BuildID         string `json:"buildId,omitempty"`
	BuildVersion    string `json:"buildVersion,omitempty"`
	BuildState      string `json:"buildState,omitempty"`
}

type RejectionReport struct {
	BundleID   string               `json:"bundleId"`
	Version    RejectionVersion     `json:"version"`
	Submission *RejectionSubmission `json:"submission,omitempty"`
	Note       string               `json:"note"`
}

func (r RejectionReport) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", r.BundleID},
		{"VERSION", r.Version.VersionString},
		{"VERSION_PLATFORM", r.Version.Platform},
		{"VERSION_STATE", r.Version.State},
		{"VERSION_RELEASE_TYPE", r.Version.ReleaseType},
	}
	if r.Version.BuildID != "" {
		rows = append(rows,
			[]string{"BUILD", r.Version.BuildVersion},
			[]string{"BUILD_STATE", r.Version.BuildState},
			[]string{"BUILD_ID", r.Version.BuildID},
		)
	} else {
		rows = append(rows, []string{"BUILD", "<none attached>"})
	}
	if r.Submission != nil {
		rows = append(rows,
			[]string{"SUBMISSION_ID", r.Submission.ID},
			[]string{"SUBMISSION_STATE", r.Submission.State},
			[]string{"SUBMISSION_SUBMITTED", r.Submission.SubmittedDate},
		)
		for i, it := range r.Submission.Items {
			rows = append(rows,
				[]string{fmt.Sprintf("ITEM_%d_STATE", i+1), it.State},
				[]string{fmt.Sprintf("ITEM_%d_REFERENCE", i+1), it.ReferenceType + "/" + it.ReferenceID},
			)
		}
	} else {
		rows = append(rows, []string{"SUBMISSION", "<none found referencing this version>"})
	}
	return headers, rows
}

var rejectionCmd = &cobra.Command{
	Use:          "rejection <bundleId>",
	Short:        "Compose a rejection report for a version (state + submission + items)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runRejection,
	Long: `rejection composes the API-visible signals around an App Store rejection
into one report: the version's state, the build attached to it (if any),
the matching review submission's state, and each review submission item's
state.

` + resolutionCenterDisclaimer + `

Examples:
  flightline rejection com.example.myapp --version 1.0.1
  flightline rejection com.example.myapp --version 1.0.1 --output json | jq .submission.state`,
	Example: `  flightline rejection com.example.myapp --version 1.0.1
  flightline rejection com.example.myapp --version 1.0.1 --output json`,
}

var (
	rejectionVersion  string
	rejectionPlatform string
)

func init() {
	rejectionCmd.Flags().StringVar(&rejectionVersion, "version", "", "version string (e.g. 1.0.1)")
	rejectionCmd.Flags().StringVar(&rejectionPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	_ = rejectionCmd.MarkFlagRequired("version")
	rootCmd.AddCommand(rejectionCmd)
}

func runRejection(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(rejectionVersion)
	platform := strings.TrimSpace(rejectionPlatform)
	if versionStr == "" {
		return errors.New("rejection: --version is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	report, err := composeRejectionReport(cmd.Context(), c, bundleID, versionStr, platform)
	if err != nil {
		return err
	}

	if err := Render(report, outputMode()); err != nil {
		return err
	}

	// Table mode repeats the disclaimer on stderr (survives a piped stdout); JSON carries it in .note instead.
	if outputMode() == "table" {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "NOTE: "+resolutionCenterDisclaimer)
	}
	return nil
}

func composeRejectionReport(ctx context.Context, c *asc.Client, bundleID, versionStr, platform string) (RejectionReport, error) {
	report := RejectionReport{
		BundleID: bundleID,
		Note:     resolutionCenterDisclaimer,
	}

	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return report, err
	}

	versionView, err := fetchVersion(ctx, c, appID, versionStr, platform)
	if err != nil {
		return report, err
	}

	report.Version = RejectionVersion{
		ID:              versionView.ID,
		VersionString:   versionView.Attributes.VersionString,
		Platform:        versionView.Attributes.Platform,
		State:           versionDisplayState(versionView.Attributes),
		AppStoreState:   versionView.Attributes.AppStoreState,
		AppVersionState: versionView.Attributes.AppVersionState,
		ReleaseType:     versionView.Attributes.ReleaseType,
	}

	build, attached, err := fetchRejectionVersionBuild(ctx, c, versionView.ID)
	if err != nil {
		return report, err
	}
	if attached {
		report.Version.BuildID = build.ID
		report.Version.BuildVersion = build.Attributes.Version
		report.Version.BuildState = build.Attributes.ProcessingState
	}

	submission, items, err := findSubmissionForVersion(ctx, c, appID, versionView.ID)
	if err != nil {
		return report, err
	}
	if submission != nil {
		s := &RejectionSubmission{
			ID:            submission.ID,
			State:         submission.Attributes.State,
			Platform:      submission.Attributes.Platform,
			SubmittedDate: submission.Attributes.SubmittedDate,
			Items:         make([]RejectionItem, 0, len(items)),
		}
		for _, it := range items {
			s.Items = append(s.Items, RejectionItem{
				ID:            it.ID,
				State:         it.Attributes.State,
				ReferenceType: it.ReferenceType,
				ReferenceID:   it.ReferenceID,
			})
		}
		report.Submission = s
	}

	return report, nil
}

type versionFull struct {
	asc.Resource[asc.VersionAttributes]
}

// Returns the full Resource (with relationships) needed to discover the linked build ID.
func fetchVersion(ctx context.Context, c *asc.Client, appID, versionStr, platform string) (versionFull, error) {
	q := url.Values{
		"filter[versionString]": {versionStr},
		"limit":                 {"1"},
	}
	if platform != "" {
		q.Set("filter[platform]", platform)
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return versionFull{}, err
	}
	if len(page.Data) == 0 {
		return versionFull{}, fmt.Errorf("rejection: no version %q found (platform=%s)", versionStr, platform)
	}
	return versionFull{Resource: page.Data[0]}, nil
}

func fetchRejectionVersionBuild(ctx context.Context, c *asc.Client, versionID string) (asc.Resource[asc.BuildAttributes], bool, error) {
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](ctx, c, "/v1/appStoreVersions/"+versionID+"/build", nil)
	if err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return asc.Resource[asc.BuildAttributes]{}, false, nil
		}
		return asc.Resource[asc.BuildAttributes]{}, false, err
	}
	return resp.Data, true, nil
}

// Returns (nil, nil, nil) when no submission references the version: a valid not-yet-submitted state.
func findSubmissionForVersion(ctx context.Context, c *asc.Client, appID, versionID string) (*ReviewSubmissionView, []ReviewSubmissionItemView, error) {
	q := url.Values{"filter[app]": {appID}, "limit": {"200"}}
	for page, err := range asc.Pages[asc.ReviewSubmissionAttributes](ctx, c, "/v1/reviewSubmissions", q) {
		if err != nil {
			return nil, nil, err
		}
		for _, sub := range page.Data {
			items, err := listReviewSubmissionItems(ctx, c, sub.ID)
			if err != nil {
				return nil, nil, err
			}
			if itemReferencesVersion(items, versionID) {
				v := &ReviewSubmissionView{ID: sub.ID, Type: sub.Type, Attributes: sub.Attributes}
				return v, items, nil
			}
		}
	}
	return nil, nil, nil
}

func itemReferencesVersion(items []ReviewSubmissionItemView, versionID string) bool {
	for _, it := range items {
		if it.ReferenceType == "appStoreVersions" && it.ReferenceID == versionID {
			return true
		}
	}
	return false
}

func relationshipID(rels map[string]asc.Relationship, name string) string {
	rel, ok := rels[name]
	if !ok {
		return ""
	}
	if len(rel.Data) == 0 || string(rel.Data) == "null" {
		return ""
	}
	var r struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(rel.Data, &r); err != nil {
		return ""
	}
	return r.ID
}
