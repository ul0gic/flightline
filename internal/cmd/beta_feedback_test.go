package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestBetaFeedbackCrashView_JSONShape(t *testing.T) {
	v := BetaFeedbackCrashView{
		ID:   "CRASH-001",
		Type: "betaFeedbackCrashSubmissions",
		Attributes: asc.BetaFeedbackCrashSubmissionAttributes{
			BetaFeedbackBaseAttributes: asc.BetaFeedbackBaseAttributes{
				CreatedDate: "2026-04-22T14:33:00Z",
				DeviceModel: "iPhone15,3",
				OsVersion:   "iOS 19.0",
				Comment:     "crashed",
				Email:       "tester@example.com",
			},
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"CRASH-001"`,
		`"type":"betaFeedbackCrashSubmissions"`,
		`"deviceModel":"iPhone15,3"`,
		`"osVersion":"iOS 19.0"`,
		`"createdDate":"2026-04-22T14:33:00Z"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestBetaFeedbackCrashList_TableRowsHeaders(t *testing.T) {
	list := BetaFeedbackCrashList{
		Submissions: []BetaFeedbackCrashView{
			{
				ID: "C1",
				Attributes: asc.BetaFeedbackCrashSubmissionAttributes{
					BetaFeedbackBaseAttributes: asc.BetaFeedbackBaseAttributes{
						CreatedDate: "2026-04-22T14:33:00Z", DeviceModel: "iPhone15,3", OsVersion: "iOS 19.0", Comment: "Tapped settings",
					},
				},
			},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"DATE", "DEVICE", "OS", "COMMENT", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][0] != "2026-04-22" {
		t.Errorf("rows[0][0] (DATE) = %q, want truncated to date", rows[0][0])
	}
}

func TestBetaFeedbackScreenshotList_TableRows(t *testing.T) {
	list := BetaFeedbackScreenshotList{
		Submissions: []BetaFeedbackScreenshotView{
			{
				ID: "S1",
				Attributes: asc.BetaFeedbackScreenshotSubmissionAttributes{
					BetaFeedbackBaseAttributes: asc.BetaFeedbackBaseAttributes{
						CreatedDate: "2026-04-22T14:40:00Z", DeviceModel: "iPad13,11", OsVersion: "iPadOS 19.0", Comment: "Save button cut off",
					},
					Screenshots: []asc.BetaFeedbackScreenshotImage{
						{URL: "https://example/a.png"},
						{URL: "https://example/b.png"},
					},
				},
			},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"DATE", "DEVICE", "OS", "COMMENT", "IMAGES", "ID"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][4] != "2" {
		t.Errorf("rows[0][4] (IMAGES) = %q, want 2", rows[0][4])
	}
}

func TestBetaFeedbackCommand_RegisteredOnRoot(t *testing.T) {
	var bf *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "beta-feedback" {
			bf = c
			break
		}
	}
	if bf == nil {
		t.Fatal("beta-feedback not registered on rootCmd")
	}
	subs := map[string]*cobra.Command{}
	for _, sc := range bf.Commands() {
		subs[sc.Name()] = sc
	}
	for _, want := range []string{"crash", "screenshot", "download"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("beta-feedback subcommand %q missing", want)
		}
	}
}

// TestBetaFeedback_JSONOutputStability_Crash locks the crash list shape.
func TestBetaFeedback_JSONOutputStability_Crash(t *testing.T) {
	list := BetaFeedbackCrashList{
		Submissions: []BetaFeedbackCrashView{
			{
				ID:   "C1",
				Type: "betaFeedbackCrashSubmissions",
				Attributes: asc.BetaFeedbackCrashSubmissionAttributes{
					BetaFeedbackBaseAttributes: asc.BetaFeedbackBaseAttributes{
						CreatedDate: "2026-04-22T14:33:00Z", DeviceModel: "iPhone15,3", OsVersion: "iOS 19.0",
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Submissions []map[string]any `json:"submissions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Submissions) != 1 {
		t.Fatalf("submissions len = %d, want 1", len(decoded.Submissions))
	}
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := decoded.Submissions[0][key]; !ok {
			t.Errorf("missing per-row key %q: JSON contract drift", key)
		}
	}
}

// TestBetaFeedback_FixtureReplay_CrashList exercises the crash list path.
func TestBetaFeedback_FixtureReplay_CrashList(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/betaFeedbackCrashSubmissions": {File: "beta_feedback_crash_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectBetaFeedbackCrashes(ctx, c, "/v1/apps/"+appID+"/betaFeedbackCrashSubmissions", url.Values{"limit": {"200"}}, 0, time.Time{})
	if err != nil {
		t.Fatalf("collectBetaFeedbackCrashes: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("crashes len = %d, want 2", len(views))
	}
	if views[0].Attributes.DeviceModel != "iPhone15,3" {
		t.Errorf("crashes[0].DeviceModel = %q, want iPhone15,3", views[0].Attributes.DeviceModel)
	}
}

// TestBetaFeedback_FixtureReplay_ScreenshotList exercises the screenshot
// list path including inline screenshots[] image URLs.
func TestBetaFeedback_FixtureReplay_ScreenshotList(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/betaFeedbackScreenshotSubmissions": {File: "beta_feedback_screenshot_list"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	views, err := collectBetaFeedbackScreenshots(ctx, c, "/v1/apps/"+appID+"/betaFeedbackScreenshotSubmissions", url.Values{"limit": {"200"}}, 0, time.Time{})
	if err != nil {
		t.Fatalf("collectBetaFeedbackScreenshots: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("screenshots len = %d, want 2", len(views))
	}
	if len(views[0].Attributes.Screenshots) != 1 {
		t.Errorf("views[0].Screenshots len = %d, want 1", len(views[0].Attributes.Screenshots))
	}
	if len(views[1].Attributes.Screenshots) != 2 {
		t.Errorf("views[1].Screenshots len = %d, want 2", len(views[1].Attributes.Screenshots))
	}
}

// TestBetaFeedback_FixtureReplay_DownloadCrashLog exercises the crashLog
// fetch + write-to-disk path.
func TestBetaFeedback_FixtureReplay_DownloadCrashLog(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/betaFeedbackCrashSubmissions/CRASH-001/crashLog": {File: "beta_feedback_download_metadata"},
	})
	c := fixtureASCClient(t, srv)
	ctx := context.Background()

	resp, err := asc.Get[asc.Single[asc.BetaCrashLogAttributes]](
		ctx, c, "/v1/betaFeedbackCrashSubmissions/CRASH-001/crashLog", nil,
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Data.Attributes.LogText == "" {
		t.Fatal("logText empty; expected fixture content")
	}
	if !strings.Contains(resp.Data.Attributes.LogText, "Incident Identifier") {
		t.Errorf("logText missing expected prefix: %q", resp.Data.Attributes.LogText[:40])
	}

	// writeBytes round-trips into a temp dir without touching network or
	// real fs locations.
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "crash.txt")
	if err := writeBytes(dest, []byte(resp.Data.Attributes.LogText)); err != nil {
		t.Fatalf("writeBytes: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != resp.Data.Attributes.LogText {
		t.Errorf("round-trip mismatch")
	}
}

// TestBetaFeedback_DownloadInvalidType asserts the --type guard rejects
// values other than crash | screenshot.
func TestBetaFeedback_DownloadInvalidType(t *testing.T) {
	prev := betaFeedbackDownloadType
	t.Cleanup(func() { betaFeedbackDownloadType = prev })

	betaFeedbackDownloadType = "video" // invalid
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := runBetaFeedbackDownload(cmd, []string{"CRASH-001"})
	if err == nil {
		t.Fatal("expected error for --type=video")
	}
	if !strings.Contains(err.Error(), "--type") {
		t.Errorf("error %q does not mention --type", err.Error())
	}
}

func TestScreenshotExt(t *testing.T) {
	cases := map[string]string{
		"https://cdn.example.com/a/b.png": ".png",
		"https://cdn.example.com/a/b.jpg": ".jpg",
		"https://cdn.example.com/a/b":     ".png", // fallback
		"::not a url::":                   ".png", // fallback
	}
	for in, want := range cases {
		if got := screenshotExt(in); got != want {
			t.Errorf("screenshotExt(%q) = %q, want %q", in, got, want)
		}
	}
}
