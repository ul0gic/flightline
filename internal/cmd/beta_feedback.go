package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// BetaFeedbackCrashView is one row of the beta-feedback crash list output.
type BetaFeedbackCrashView struct {
	ID         string                                    `json:"id"`
	Type       string                                    `json:"type"`
	Attributes asc.BetaFeedbackCrashSubmissionAttributes `json:"attributes"`
}

// BetaFeedbackCrashList is the table-aware view for `beta-feedback crash`.
type BetaFeedbackCrashList struct {
	Submissions []BetaFeedbackCrashView `json:"submissions"`
}

// TableRows implements TableRenderable for the crash submissions list.
//
// Surface the pieces a triage operator wants at a glance: when the crash
// landed, what device, what OS, the tester's comment (truncated), and the
// id (for `beta-feedback download`).
func (l BetaFeedbackCrashList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"DATE", "DEVICE", "OS", "COMMENT", "ID"}
	rows = make([][]string, 0, len(l.Submissions))
	for i := range l.Submissions {
		s := &l.Submissions[i]
		rows = append(rows, []string{
			truncDate(s.Attributes.CreatedDate),
			s.Attributes.DeviceModel,
			s.Attributes.OsVersion,
			truncTitle(s.Attributes.Comment, 60),
			s.ID,
		})
	}
	return headers, rows
}

// BetaFeedbackScreenshotView is one row of the beta-feedback screenshot list.
type BetaFeedbackScreenshotView struct {
	ID         string                                         `json:"id"`
	Type       string                                         `json:"type"`
	Attributes asc.BetaFeedbackScreenshotSubmissionAttributes `json:"attributes"`
}

// BetaFeedbackScreenshotList is the table-aware view for `beta-feedback screenshot`.
type BetaFeedbackScreenshotList struct {
	Submissions []BetaFeedbackScreenshotView `json:"submissions"`
}

// TableRows implements TableRenderable for the screenshot submissions list.
func (l BetaFeedbackScreenshotList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"DATE", "DEVICE", "OS", "COMMENT", "IMAGES", "ID"}
	rows = make([][]string, 0, len(l.Submissions))
	for i := range l.Submissions {
		s := &l.Submissions[i]
		rows = append(rows, []string{
			truncDate(s.Attributes.CreatedDate),
			s.Attributes.DeviceModel,
			s.Attributes.OsVersion,
			truncTitle(s.Attributes.Comment, 50),
			fmt.Sprintf("%d", len(s.Attributes.Screenshots)),
			s.ID,
		})
	}
	return headers, rows
}

// BetaFeedbackDownloadView is the result of `beta-feedback download`.
//
// SavedTo names the local path that received the file. Type identifies
// which feedback resource the id resolved to — "crashLog" for crash log
// text, "screenshot" for screenshot bytes — so consumers can branch off
// the JSON contract.
type BetaFeedbackDownloadView struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	SavedTo string `json:"savedTo"`
	Bytes   int    `json:"bytes"`
}

// TableRows for the download result.
func (v *BetaFeedbackDownloadView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", v.ID},
		{"TYPE", v.Type},
		{"SAVED_TO", v.SavedTo},
		{"BYTES", fmt.Sprintf("%d", v.Bytes)},
	}
	return headers, rows
}

var betaFeedbackCmd = &cobra.Command{
	Use:   "beta-feedback",
	Short: "Read TestFlight beta feedback (crash submissions, screenshots)",
	Long: `beta-feedback groups read commands over Apple's TestFlight feedback resources:

  - crash <bundleId>           — list crash submissions, optionally filtered by build
  - screenshot <bundleId>      — list screenshot submissions, optionally filtered by build
  - download <feedbackId>      — download the crash log or screenshot to disk

Phase 3 has no write surface here — feedback is tester-authored.`,
}

var betaFeedbackCrashCmd = &cobra.Command{
	Use:          "crash <bundleId>",
	Short:        "List TestFlight crash submissions for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBetaFeedbackCrash,
	Example: `  fline beta-feedback crash com.example.myapp
  fline beta-feedback crash com.example.myapp --build 42
  fline beta-feedback crash com.example.myapp --output json | jq '.submissions[].attributes.deviceModel'`,
}

var betaFeedbackScreenshotCmd = &cobra.Command{
	Use:          "screenshot <bundleId>",
	Short:        "List TestFlight screenshot submissions for an app",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBetaFeedbackScreenshot,
	Example: `  fline beta-feedback screenshot com.example.myapp
  fline beta-feedback screenshot com.example.myapp --build 42 --output json`,
}

var betaFeedbackDownloadCmd = &cobra.Command{
	Use:          "download <feedbackId>",
	Short:        "Download the crash log or screenshot for a feedback submission",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runBetaFeedbackDownload,
	Example: `  fline beta-feedback download CRASH-1234 --out crash.txt
  fline beta-feedback download SCREENSHOT-5678 --type screenshot --out shot.png
  fline beta-feedback download CRASH-1234 --output json`,
}

var (
	betaFeedbackCrashBuild      string
	betaFeedbackCrashSince      string
	betaFeedbackCrashLimit      int
	betaFeedbackScreenshotBuild string
	betaFeedbackScreenshotSince string
	betaFeedbackScreenshotLimit int
	betaFeedbackDownloadOut     string
	betaFeedbackDownloadType    string
)

func init() {
	betaFeedbackCrashCmd.Flags().StringVar(&betaFeedbackCrashBuild, "build", "", "filter by build id (Apple's build resource id; not the build number)")
	betaFeedbackCrashCmd.Flags().StringVar(&betaFeedbackCrashSince, "since", "", "only submissions newer than this duration (e.g. 30d) or ISO date (2026-04-01)")
	betaFeedbackCrashCmd.Flags().IntVar(&betaFeedbackCrashLimit, "limit", 0, "max submissions to emit (0 = no cap)")

	betaFeedbackScreenshotCmd.Flags().StringVar(&betaFeedbackScreenshotBuild, "build", "", "filter by build id (Apple's build resource id; not the build number)")
	betaFeedbackScreenshotCmd.Flags().StringVar(&betaFeedbackScreenshotSince, "since", "", "only submissions newer than this duration (e.g. 30d) or ISO date (2026-04-01)")
	betaFeedbackScreenshotCmd.Flags().IntVar(&betaFeedbackScreenshotLimit, "limit", 0, "max submissions to emit (0 = no cap)")

	betaFeedbackDownloadCmd.Flags().StringVar(&betaFeedbackDownloadOut, "out", "", "destination path for the downloaded bytes (default: <feedbackId>.<ext>)")
	betaFeedbackDownloadCmd.Flags().StringVar(&betaFeedbackDownloadType, "type", "crash", "feedback type: crash | screenshot")

	betaFeedbackCmd.AddCommand(betaFeedbackCrashCmd)
	betaFeedbackCmd.AddCommand(betaFeedbackScreenshotCmd)
	betaFeedbackCmd.AddCommand(betaFeedbackDownloadCmd)
	rootCmd.AddCommand(betaFeedbackCmd)
}

func runBetaFeedbackCrash(cmd *cobra.Command, args []string) error {
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
	if b := strings.TrimSpace(betaFeedbackCrashBuild); b != "" {
		q.Set("filter[build]", b)
	}
	if q.Get("sort") == "" {
		q.Set("sort", "-createdDate")
	}

	since, err := parseSince(betaFeedbackCrashSince)
	if err != nil {
		return err
	}

	views, err := collectBetaFeedbackCrashes(
		cmd.Context(), c,
		"/v1/apps/"+appID+"/betaFeedbackCrashSubmissions",
		q, betaFeedbackCrashLimit, since,
	)
	if err != nil {
		return err
	}
	return Render(BetaFeedbackCrashList{Submissions: views}, outputMode())
}

func runBetaFeedbackScreenshot(cmd *cobra.Command, args []string) error {
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
	if b := strings.TrimSpace(betaFeedbackScreenshotBuild); b != "" {
		q.Set("filter[build]", b)
	}
	if q.Get("sort") == "" {
		q.Set("sort", "-createdDate")
	}

	since, err := parseSince(betaFeedbackScreenshotSince)
	if err != nil {
		return err
	}

	views, err := collectBetaFeedbackScreenshots(
		cmd.Context(), c,
		"/v1/apps/"+appID+"/betaFeedbackScreenshotSubmissions",
		q, betaFeedbackScreenshotLimit, since,
	)
	if err != nil {
		return err
	}
	return Render(BetaFeedbackScreenshotList{Submissions: views}, outputMode())
}

func runBetaFeedbackDownload(cmd *cobra.Command, args []string) error {
	feedbackID := strings.TrimSpace(args[0])
	if feedbackID == "" {
		return fmt.Errorf("beta-feedback: feedback id is required")
	}
	feedbackType := strings.ToLower(strings.TrimSpace(betaFeedbackDownloadType))
	if feedbackType != "crash" && feedbackType != "screenshot" {
		return fmt.Errorf("beta-feedback: --type %q invalid (expected: crash | screenshot)", feedbackType)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	switch feedbackType {
	case "crash":
		return downloadCrashLog(cmd.Context(), c, feedbackID, betaFeedbackDownloadOut)
	case "screenshot":
		return downloadScreenshot(cmd.Context(), c, feedbackID, betaFeedbackDownloadOut)
	}
	return nil
}

// downloadCrashLog fetches the crash log resource for a crash submission and
// writes the symbolicated text to disk. Returns the saved path.
func downloadCrashLog(ctx context.Context, c *asc.Client, feedbackID, outPath string) error {
	resp, err := asc.Get[asc.Single[asc.BetaCrashLogAttributes]](
		ctx, c, "/v1/betaFeedbackCrashSubmissions/"+feedbackID+"/crashLog", nil,
	)
	if err != nil {
		return err
	}
	if resp.Data.Attributes.LogText == "" {
		return fmt.Errorf("beta-feedback: crash log for %q has no text body", feedbackID)
	}

	dest := outPath
	if dest == "" {
		dest = feedbackID + ".crash.txt"
	}
	if err := writeBytes(dest, []byte(resp.Data.Attributes.LogText)); err != nil {
		return err
	}
	return Render(&BetaFeedbackDownloadView{
		ID:      feedbackID,
		Type:    "crashLog",
		SavedTo: dest,
		Bytes:   len(resp.Data.Attributes.LogText),
	}, outputMode())
}

// downloadScreenshot fetches the screenshot submission, picks the first
// image's pre-signed URL, and downloads its bytes to disk. The URL is
// transient (Apple sets an expiration date) so we never cache the URL,
// only the resolved bytes.
func downloadScreenshot(ctx context.Context, c *asc.Client, feedbackID, outPath string) error {
	resp, err := asc.Get[asc.Single[asc.BetaFeedbackScreenshotSubmissionAttributes]](
		ctx, c, "/v1/betaFeedbackScreenshotSubmissions/"+feedbackID, nil,
	)
	if err != nil {
		return err
	}
	if len(resp.Data.Attributes.Screenshots) == 0 {
		return fmt.Errorf("beta-feedback: screenshot submission %q has no images", feedbackID)
	}
	imgURL := resp.Data.Attributes.Screenshots[0].URL
	if imgURL == "" {
		return fmt.Errorf("beta-feedback: screenshot submission %q first image has empty url", feedbackID)
	}

	dest := outPath
	if dest == "" {
		dest = feedbackID + screenshotExt(imgURL)
	}

	body, err := fetchBytes(ctx, imgURL)
	if err != nil {
		return err
	}
	if err := writeBytes(dest, body); err != nil {
		return err
	}
	return Render(&BetaFeedbackDownloadView{
		ID:      feedbackID,
		Type:    "screenshot",
		SavedTo: dest,
		Bytes:   len(body),
	}, outputMode())
}

// screenshotExt returns the file extension implied by the URL path. Apple
// pre-signs CDN URLs that end in .png / .jpg; fall back to .png when no
// extension is detectable so the output is at least playable.
func screenshotExt(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ".png"
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	if ext == "" {
		return ".png"
	}
	return ext
}

// fetchBytes downloads bytes from a non-Apple-API URL (Apple's screenshot
// CDN). No JWT — the URL is already pre-signed; injecting one would
// invalidate the signature. Bounded read to 64 MiB to defend against a
// runaway response.
func fetchBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("beta-feedback: build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("beta-feedback: fetch screenshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("beta-feedback: screenshot fetch returned HTTP %d", resp.StatusCode)
	}
	const maxBytes = 64 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("beta-feedback: read screenshot body: %w", err)
	}
	return body, nil
}

// writeBytes writes b to path with mode 0644. Parent directories must
// already exist; this is intentional — silently creating directories is a
// surprise the caller didn't ask for.
func writeBytes(path string, b []byte) error {
	if err := os.WriteFile(path, b, 0o644); err != nil { //nolint:gosec // tester-supplied content; not a secret
		return fmt.Errorf("beta-feedback: write %s: %w", path, err)
	}
	return nil
}

// collectBetaFeedbackCrashes walks the paging iterator with optional
// since-cutoff (client-side because Apple has no created-since filter).
//
// Apple sorts submissions newest-first when the request asks for it, so the
// loop short-circuits as soon as a record predates the cutoff — avoiding
// the cost of paging through years of crashes when callers want the last
// 30 days.
func collectBetaFeedbackCrashes(ctx context.Context, c *asc.Client, path string, query url.Values, limit int, since time.Time) ([]BetaFeedbackCrashView, error) {
	out := make([]BetaFeedbackCrashView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.BetaFeedbackCrashSubmissionAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			if !since.IsZero() {
				if t, ok := parseISO(r.Attributes.CreatedDate); ok && t.Before(since) {
					return out, nil
				}
			}
			out = append(out, BetaFeedbackCrashView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// collectBetaFeedbackScreenshots is the screenshot equivalent of
// collectBetaFeedbackCrashes — same since-cutoff semantics, different
// resource type.
func collectBetaFeedbackScreenshots(ctx context.Context, c *asc.Client, path string, query url.Values, limit int, since time.Time) ([]BetaFeedbackScreenshotView, error) {
	out := make([]BetaFeedbackScreenshotView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.BetaFeedbackScreenshotSubmissionAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			if !since.IsZero() {
				if t, ok := parseISO(r.Attributes.CreatedDate); ok && t.Before(since) {
					return out, nil
				}
			}
			out = append(out, BetaFeedbackScreenshotView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}
