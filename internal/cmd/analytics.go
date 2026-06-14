package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// AnalyticsRequestView is the JSON contract for `analytics request`.
// Reports is populated only under --wait.
type AnalyticsRequestView struct {
	BundleID    string                         `json:"bundleId"`
	RequestID   string                         `json:"requestId"`
	AccessType  string                         `json:"accessType"`
	Status      string                         `json:"status,omitempty"`
	SubmittedAt string                         `json:"submittedAt,omitempty"`
	LastPollAt  string                         `json:"lastPollAt,omitempty"`
	Reports     []asc.PersistedAnalyticsReport `json:"reports"`
}

func (v AnalyticsRequestView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"REQUEST_ID", v.RequestID},
		{"ACCESS_TYPE", v.AccessType},
		{"STATUS", v.Status},
		{"SUBMITTED_AT", v.SubmittedAt},
		{"LAST_POLL_AT", v.LastPollAt},
		{"REPORTS_OBSERVED", strconv.Itoa(len(v.Reports))},
	}
	return headers, rows
}

type AnalyticsInstancesView struct {
	BundleID  string                          `json:"bundleId"`
	RequestID string                          `json:"requestId"`
	Reports   []AnalyticsReportInstancesEntry `json:"reports"`
}

type AnalyticsReportInstancesEntry struct {
	Report    asc.PersistedAnalyticsReport  `json:"report"`
	Instances []asc.AnalyticsReportInstance `json:"instances"`
}

func (v AnalyticsInstancesView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"REPORT", "CATEGORY", "INSTANCE", "GRANULARITY", "DATE"}
	for _, entry := range v.Reports {
		for _, inst := range entry.Instances {
			rows = append(rows, []string{
				entry.Report.Name,
				string(entry.Report.Category),
				string(inst.ID),
				string(inst.Granularity),
				inst.ProcessingDate,
			})
		}
		if len(entry.Instances) == 0 {
			rows = append(rows, []string{
				entry.Report.Name,
				string(entry.Report.Category),
				"(no instances yet)",
				"",
				"",
			})
		}
	}
	return headers, rows
}

type AnalyticsDownloadView struct {
	BundleID   string                      `json:"bundleId"`
	InstanceID string                      `json:"instanceId"`
	Files      []string                    `json:"files"`
	ByteCount  int                         `json:"byteCount"`
	Segments   []asc.SegmentDownloadResult `json:"segments"`
}

func (v AnalyticsDownloadView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"SEGMENT", "BYTES", "FILE"}
	for i, seg := range v.Segments {
		file := ""
		if i < len(v.Files) {
			file = v.Files[i]
		}
		rows = append(rows, []string{
			seg.SegmentID,
			strconv.Itoa(seg.ByteCount),
			file,
		})
	}
	return headers, rows
}

type AnalyticsStatusView struct {
	BundleID    string                         `json:"bundleId"`
	StateFile   string                         `json:"stateFile"`
	RequestID   string                         `json:"requestId"`
	Status      string                         `json:"status"`
	SubmittedAt string                         `json:"submittedAt,omitempty"`
	LastPollAt  string                         `json:"lastPollAt,omitempty"`
	Reports     []asc.PersistedAnalyticsReport `json:"reports"`
	Downloaded  []string                       `json:"downloadedSegments"`
}

func (v AnalyticsStatusView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"STATE_FILE", v.StateFile},
		{"REQUEST_ID", v.RequestID},
		{"STATUS", v.Status},
		{"SUBMITTED_AT", v.SubmittedAt},
		{"LAST_POLL_AT", v.LastPollAt},
		{"REPORTS", strconv.Itoa(len(v.Reports))},
		{"DOWNLOADED_SEGMENTS", strconv.Itoa(len(v.Downloaded))},
	}
	return headers, rows
}

var analyticsCmd = &cobra.Command{
	Use:   "analytics",
	Short: "Request, track, and download Apple analytics reports",
	Long: `analytics drives Apple's asynchronous analytics report lifecycle:

	1. request : submit an analyticsReportRequests entry to Apple
	2. status  : read the persisted state file for an in-flight request
	3. list-instances: enumerate report instances for the active request
	4. download: pull every segment of an instance to local CSV files

State persists to $XDG_STATE_HOME/flightline/<bundleId>/analytics.json so a
Ctrl-C between submit and download resumes cleanly on the next run.`,
}

var analyticsRequestCmd = &cobra.Command{
	Use:          "request <bundleId>",
	Short:        "Submit a new analytics report request",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAnalyticsRequest,
	Example: `  flightline analytics request com.example.myapp --access-type ONE_TIME_SNAPSHOT
  flightline analytics request com.example.myapp --access-type ONE_TIME_SNAPSHOT --wait
  flightline analytics request com.example.myapp --access-type ONGOING --wait --max-duration 10m`,
}

var analyticsListInstancesCmd = &cobra.Command{
	Use:          "list-instances <bundleId>",
	Short:        "List instances of analytics reports for the active request",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAnalyticsListInstances,
	Example: `  flightline analytics list-instances com.example.myapp
  flightline analytics list-instances com.example.myapp --report-id RPT-1
  flightline analytics list-instances com.example.myapp --category APP_USAGE --name-contains daily`,
}

var analyticsDownloadCmd = &cobra.Command{
	Use:          "download <bundleId>",
	Short:        "Download every segment of an analytics report instance",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAnalyticsDownload,
	Example: `  flightline analytics download com.example.myapp --instance INST-1
  flightline analytics download com.example.myapp --instance INST-1 --out ./reports/`,
}

var analyticsStatusCmd = &cobra.Command{
	Use:          "status <bundleId>",
	Short:        "Show the persisted state for an in-flight analytics request",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAnalyticsStatus,
	Example: `  flightline analytics status com.example.myapp
  flightline analytics status com.example.myapp --output json | jq .requestId`,
}

var (
	analyticsRequestAccessType  string
	analyticsRequestWait        bool
	analyticsRequestMaxDuration time.Duration

	analyticsListReportID     string
	analyticsListCategory     string
	analyticsListNameContains string

	analyticsDownloadInstance string
	analyticsDownloadOut      string

	// Tests override with shorter intervals so the lifecycle runs in
	// sub-second wall time without leaving the production code path.
	analyticsPollOpts asc.PollOpts
)

func init() {
	analyticsRequestCmd.Flags().StringVar(&analyticsRequestAccessType, "access-type", "",
		"access type: ONE_TIME_SNAPSHOT or ONGOING")
	analyticsRequestCmd.Flags().BoolVar(&analyticsRequestWait, "wait", false,
		"block until reports are available; pair with --max-duration for ONGOING")
	analyticsRequestCmd.Flags().DurationVar(&analyticsRequestMaxDuration, "max-duration", 0,
		"upper bound on --wait (e.g. 10m); 0 = no bound: required for ONGOING with --wait")
	_ = analyticsRequestCmd.MarkFlagRequired("access-type")

	analyticsListInstancesCmd.Flags().StringVar(&analyticsListReportID, "report-id", "",
		"single report ID to expand instances for (default: every report in state)")
	analyticsListInstancesCmd.Flags().StringVar(&analyticsListCategory, "category", "",
		"filter reports by category (e.g. APP_USAGE, COMMERCE)")
	analyticsListInstancesCmd.Flags().StringVar(&analyticsListNameContains, "name-contains", "",
		"filter reports whose name contains this substring (case-insensitive)")

	analyticsDownloadCmd.Flags().StringVar(&analyticsDownloadInstance, "instance", "",
		"instance ID to download segments from")
	analyticsDownloadCmd.Flags().StringVar(&analyticsDownloadOut, "out", "",
		"output directory or file prefix; default is the working directory")
	_ = analyticsDownloadCmd.MarkFlagRequired("instance")

	analyticsCmd.AddCommand(analyticsRequestCmd)
	analyticsCmd.AddCommand(analyticsListInstancesCmd)
	analyticsCmd.AddCommand(analyticsDownloadCmd)
	analyticsCmd.AddCommand(analyticsStatusCmd)
	rootCmd.AddCommand(analyticsCmd)
}

// State is persisted immediately after Apple returns the request ID, so an
// interrupted --wait still resumes from the captured request ID.
func runAnalyticsRequest(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	access, err := parseAccessType(analyticsRequestAccessType)
	if err != nil {
		return err
	}
	if err := validateWaitFlags(access, analyticsRequestWait, analyticsRequestMaxDuration); err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	view, err := submitAndPersist(cmd.Context(), c, bundleID, appID, access)
	if err != nil {
		return err
	}

	if analyticsRequestWait {
		view, err = pollAndAppend(cmd.Context(), c, view, analyticsRequestMaxDuration)
		if err != nil {
			return err
		}
	}
	return Render(view, outputMode())
}

func submitAndPersist(ctx context.Context, c *asc.Client, bundleID, appID string, access asc.AnalyticsAccessType) (AnalyticsRequestView, error) {
	id, err := c.RequestAnalyticsReport(ctx, asc.AnalyticsReportRequestParams{
		AppID:      appID,
		AccessType: access,
	})
	if err != nil {
		return AnalyticsRequestView{}, err
	}
	now := time.Now().UTC()
	state := asc.AsyncState{
		BundleID:    bundleID,
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   id,
		SubmittedAt: now,
		Status:      "queued",
	}
	if err := asc.PersistAsyncState(state); err != nil {
		return AnalyticsRequestView{}, fmt.Errorf("persist state: %w", err)
	}
	return AnalyticsRequestView{
		BundleID:    bundleID,
		RequestID:   string(id),
		AccessType:  string(access),
		Status:      state.Status,
		SubmittedAt: now.Format(time.RFC3339),
		Reports:     []asc.PersistedAnalyticsReport{},
	}, nil
}

func pollAndAppend(ctx context.Context, c *asc.Client, view AnalyticsRequestView, maxDuration time.Duration) (AnalyticsRequestView, error) {
	pollCtx := ctx
	if maxDuration > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, maxDuration)
		defer cancel()
	}

	view.Status = "processing"
	for r, err := range c.PollAnalyticsReport(pollCtx, asc.RequestID(view.RequestID), analyticsPollOpts) {
		if err != nil {
			return finishPoll(view, err)
		}
		view.Reports = append(view.Reports, asc.PersistedAnalyticsReport{
			ID:       r.ID,
			Name:     r.Name,
			Category: r.Category,
		})
	}
	return finishPoll(view, nil)
}

// Sentinel errors that signal a normal terminal are folded into Status
// rather than returned as failures.
func finishPoll(view AnalyticsRequestView, err error) (AnalyticsRequestView, error) {
	now := time.Now().UTC()
	view.LastPollAt = now.Format(time.RFC3339)
	switch {
	case err == nil:
		view.Status = "completed"
	case errors.Is(err, asc.ErrAnalyticsRequestStopped):
		view.Status = "stopped"
	case errors.Is(err, context.DeadlineExceeded):
		view.Status = "timeout"
	case errors.Is(err, context.Canceled):
		view.Status = "cancelled"
	default:
		view.Status = "failed"
	}
	persistErr := asc.PersistAsyncState(asc.AsyncState{
		BundleID:    view.BundleID,
		ReportClass: asc.ReportClassAnalytics,
		RequestID:   asc.RequestID(view.RequestID),
		SubmittedAt: parseRFC3339OrZero(view.SubmittedAt),
		LastPollAt:  now,
		Status:      view.Status,
		Reports:     view.Reports,
	})
	if err != nil && !errors.Is(err, asc.ErrAnalyticsRequestStopped) &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return view, err
	}
	if persistErr != nil {
		return view, fmt.Errorf("persist state: %w", persistErr)
	}
	return view, nil
}

func runAnalyticsListInstances(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	state, err := loadAnalyticsState(bundleID)
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	reports := filterReportsForList(state.Reports, analyticsListReportID, analyticsListCategory, analyticsListNameContains)
	view := AnalyticsInstancesView{
		BundleID:  bundleID,
		RequestID: string(state.RequestID),
		Reports:   make([]AnalyticsReportInstancesEntry, 0, len(reports)),
	}
	for _, r := range reports {
		instances, err := c.ListAnalyticsInstances(cmd.Context(), r.ID)
		if err != nil {
			return fmt.Errorf("list instances for %q: %w", r.ID, err)
		}
		view.Reports = append(view.Reports, AnalyticsReportInstancesEntry{
			Report:    r,
			Instances: instances,
		})
	}
	return Render(view, outputMode())
}

func filterReportsForList(reports []asc.PersistedAnalyticsReport, reportID, category, nameContains string) []asc.PersistedAnalyticsReport {
	if reportID != "" {
		for _, r := range reports {
			if string(r.ID) == reportID {
				return []asc.PersistedAnalyticsReport{r}
			}
		}
		return nil
	}
	cat := strings.TrimSpace(category)
	needle := strings.ToLower(strings.TrimSpace(nameContains))
	if cat == "" && needle == "" {
		out := make([]asc.PersistedAnalyticsReport, len(reports))
		copy(out, reports)
		return out
	}
	out := make([]asc.PersistedAnalyticsReport, 0, len(reports))
	for _, r := range reports {
		if cat != "" && string(r.Category) != cat {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.Name), needle) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func runAnalyticsDownload(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	if _, err := loadAnalyticsState(bundleID); err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	instanceID := asc.InstanceID(strings.TrimSpace(analyticsDownloadInstance))
	segments, err := c.ListAnalyticsSegments(cmd.Context(), instanceID)
	if err != nil {
		return fmt.Errorf("list segments: %w", err)
	}

	view := AnalyticsDownloadView{
		BundleID:   bundleID,
		InstanceID: string(instanceID),
		Files:      make([]string, 0, len(segments)),
		Segments:   make([]asc.SegmentDownloadResult, 0, len(segments)),
	}

	for i, seg := range segments {
		body, derr := c.DownloadAnalyticsSegment(cmd.Context(), seg.ID)
		if derr != nil {
			return fmt.Errorf("download %s: %w", seg.ID, derr)
		}
		path, werr := writeSegmentFile(bundleID, string(instanceID), i, body, analyticsDownloadOut)
		if werr != nil {
			return werr
		}
		result := asc.ParseSegmentDownload(seg.ID, instanceID, body)
		view.Files = append(view.Files, path)
		view.Segments = append(view.Segments, result)
		view.ByteCount += result.ByteCount
	}
	return Render(view, outputMode())
}

func writeSegmentFile(bundleID, instanceID string, index int, body []byte, out string) (string, error) {
	name := fmt.Sprintf("%s-%s-segment%d.csv", bundleID, instanceID, index)
	dir := "."
	if out != "" {
		info, err := os.Stat(out)
		switch {
		case err == nil && info.IsDir():
			dir = out
		case err == nil:
			// Refuse an existing file: silently clobbering it would lose data.
			return "", fmt.Errorf("--out %q is an existing file; pass a directory or a non-existent prefix", out)
		default:
			// Treat a non-existent path as a directory to create.
			if err := os.MkdirAll(out, 0o700); err != nil {
				return "", fmt.Errorf("create --out %q: %w", out, err)
			}
			dir = out
		}
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func runAnalyticsStatus(_ *cobra.Command, args []string) error {
	bundleID := args[0]
	state, err := loadAnalyticsState(bundleID)
	if err != nil {
		return err
	}
	path, err := asc.StateFilePath(bundleID, asc.ReportClassAnalytics)
	if err != nil {
		return err
	}
	view := AnalyticsStatusView{
		BundleID:   bundleID,
		StateFile:  path,
		RequestID:  string(state.RequestID),
		Status:     state.Status,
		Reports:    state.Reports,
		Downloaded: state.DownloadedSegments,
	}
	if !state.SubmittedAt.IsZero() {
		view.SubmittedAt = state.SubmittedAt.UTC().Format(time.RFC3339)
	}
	if !state.LastPollAt.IsZero() {
		view.LastPollAt = state.LastPollAt.UTC().Format(time.RFC3339)
	}
	if view.Reports == nil {
		view.Reports = []asc.PersistedAnalyticsReport{}
	}
	if view.Downloaded == nil {
		view.Downloaded = []string{}
	}
	return Render(view, outputMode())
}

// All readers bottleneck through this so the no-prior-request hint stays
// consistent across commands.
func loadAnalyticsState(bundleID string) (asc.AsyncState, error) {
	state, err := asc.LoadAsyncState(bundleID, asc.ReportClassAnalytics)
	if errors.Is(err, os.ErrNotExist) {
		return asc.AsyncState{}, fmt.Errorf(
			"analytics: no active analytics request for %q: run `flightline analytics request %s --access-type ONE_TIME_SNAPSHOT` first",
			bundleID, bundleID,
		)
	}
	if err != nil {
		return asc.AsyncState{}, fmt.Errorf("load state: %w", err)
	}
	if state.RequestID == "" {
		return asc.AsyncState{}, fmt.Errorf(
			"analytics: state file for %q has no request ID: re-submit via `flightline analytics request %s`",
			bundleID, bundleID,
		)
	}
	return state, nil
}

func parseAccessType(s string) (asc.AnalyticsAccessType, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case string(asc.AccessTypeOneTimeSnapshot):
		return asc.AccessTypeOneTimeSnapshot, nil
	case string(asc.AccessTypeOngoing):
		return asc.AccessTypeOngoing, nil
	default:
		return "", fmt.Errorf(
			"analytics: --access-type must be ONE_TIME_SNAPSHOT or ONGOING (got %q)", s,
		)
	}
}

// ONGOING requests never auto-terminate, so --wait without --max-duration
// would hang the CLI indefinitely.
func validateWaitFlags(access asc.AnalyticsAccessType, wait bool, maxDuration time.Duration) error {
	if !wait {
		return nil
	}
	if access == asc.AccessTypeOngoing && maxDuration <= 0 {
		return errors.New(
			"analytics: --wait with --access-type ONGOING requires --max-duration (Apple's ONGOING requests never auto-terminate)",
		)
	}
	return nil
}

// SubmittedAt was minted via Format(time.RFC3339) in the same RunE, so a
// parse failure is programmer error; fall back to zero rather than abort.
func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
