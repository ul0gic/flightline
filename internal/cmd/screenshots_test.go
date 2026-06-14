package cmd

import (
	"context"
	"crypto/md5" //nolint:gosec // Apple's API requires MD5 for upload integrity checks; tests assert that contract
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestScreenshotsCmd_Registered(t *testing.T) {
	var sc *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "screenshots" {
			sc = c
			break
		}
	}
	if sc == nil {
		t.Fatal("screenshots not registered on rootCmd")
	}
	var up *cobra.Command
	for _, c := range sc.Commands() {
		if c.Name() == "upload" {
			up = c
			break
		}
	}
	if up == nil {
		t.Fatal("screenshots upload not registered")
	}
	for _, want := range []string{"version", "platform", "locale", "device-set", "resume"} {
		if up.Flag(want) == nil {
			t.Errorf("screenshots upload: missing --%s flag", want)
		}
	}
}

func TestIsValidDeviceSet(t *testing.T) {
	good := []string{
		"APP_IPHONE_67", "APP_IPAD_PRO_3GEN_129", "APP_DESKTOP",
		"APP_WATCH_ULTRA", "APP_APPLE_TV", "APP_APPLE_VISION_PRO",
		"IMESSAGE_APP_IPHONE_67",
	}
	for _, v := range good {
		if !isValidDeviceSet(v) {
			t.Errorf("isValidDeviceSet(%q) = false, want true", v)
		}
	}
	bad := []string{"", "APP_IPHONE_99", "iphone-67", "APP_IPHONE_67 "}
	for _, v := range bad {
		if isValidDeviceSet(v) {
			t.Errorf("isValidDeviceSet(%q) = true, want false", v)
		}
	}
}

func TestValidateScreenshotFile_Errors(t *testing.T) {
	if err := validateScreenshotFile(""); err == nil {
		t.Error("empty path: want error")
	}
	if err := validateScreenshotFile("/nonexistent/flightline-test-foo.png"); err == nil {
		t.Error("nonexistent path: want error")
	}
	dir := t.TempDir()
	zero := filepath.Join(dir, "zero.png")
	if err := os.WriteFile(zero, nil, 0o600); err != nil {
		t.Fatalf("write zero file: %v", err)
	}
	if err := validateScreenshotFile(zero); err == nil {
		t.Error("zero-length file: want error")
	}
	good := filepath.Join(dir, "good.png")
	if err := os.WriteFile(good, []byte("PNG"), 0o600); err != nil {
		t.Fatalf("write good file: %v", err)
	}
	if err := validateScreenshotFile(good); err != nil {
		t.Errorf("good file: %v", err)
	}
	if err := validateScreenshotFile(dir); err == nil {
		t.Error("directory path: want 'not a regular file' error")
	}
}

func TestMD5HexOfFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := md5HexOfFile(path)
	if err != nil {
		t.Fatalf("md5HexOfFile: %v", err)
	}
	want := "5d41402abc4b2a76b9719d911017c592"
	if got != want {
		t.Errorf("md5 = %q, want %q", got, want)
	}
}

func TestBasename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/abs/path/to/file.png", "file.png"},
		{"file.png", "file.png"},
		{"./relative/file.png", "file.png"},
		{`C:\windows\path\file.png`, "file.png"},
	}
	for _, c := range cases {
		if got := basename(c.in); got != c.want {
			t.Errorf("basename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestActionForUpload(t *testing.T) {
	if got := actionForUpload(0); got != "noop" {
		t.Errorf("actionForUpload(0) = %q, want 'noop'", got)
	}
	if got := actionForUpload(3); got != "uploaded" {
		t.Errorf("actionForUpload(3) = %q, want 'uploaded'", got)
	}
}

func TestValidDeviceSetsSorted_Stable(t *testing.T) {
	a := validDeviceSetsSorted()
	b := validDeviceSetsSorted()
	if len(a) != len(validScreenshotDeviceSets) {
		t.Errorf("len = %d, want %d", len(a), len(validScreenshotDeviceSets))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("non-stable order at %d: %q vs %q", i, a[i], b[i])
		}
	}
}

func TestScreenshotUploadResult_JSONShape(t *testing.T) {
	r := ScreenshotUploadResult{
		Action:        "uploaded",
		Changed:       true,
		BundleID:      "com.example.alpha",
		Version:       "1.0.1",
		Locale:        "en-US",
		DeviceSet:     "APP_IPHONE_67",
		SetID:         "SS000000001",
		UploadedCount: 1,
		SkippedCount:  1,
		Files: []ScreenshotUploadResultEntry{
			{Path: "01.png", FileName: "01.png", MD5: "abc", Action: "skipped", AssetID: "SH000000001", Reason: "checksum already present in target set"},
			{Path: "02.png", FileName: "02.png", MD5: "def", Action: "uploaded", AssetID: "SH000000002"},
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"action":"uploaded"`, `"changed":true`,
		`"bundleId":"com.example.alpha"`, `"version":"1.0.1"`,
		`"locale":"en-US"`, `"deviceSet":"APP_IPHONE_67"`,
		`"setId":"SS000000001"`, `"uploadedCount":1`, `"skippedCount":1`,
		`"files":`, `"path":"01.png"`, `"fileName":"01.png"`,
		`"md5":"abc"`, `"action":"skipped"`, `"reason":"checksum already present in target set"`,
		`"assetId":"SH000000001"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %s", want, out)
		}
	}
}

func TestScreenshotUploadResult_TableRows(t *testing.T) {
	r := &ScreenshotUploadResult{
		Files: []ScreenshotUploadResultEntry{
			{FileName: "01.png", Action: "skipped", MD5: "abc", AssetID: "SH1"},
			{FileName: "02.png", Action: "uploaded", MD5: "def", AssetID: "SH2"},
		},
	}
	headers, rows := r.TableRows()
	if len(headers) != 4 {
		t.Errorf("headers = %v, want 4 cols", headers)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][1] != "skipped" || rows[1][1] != "uploaded" {
		t.Errorf("rows = %v, want skipped/uploaded", rows)
	}
}

// TestFindOrCreateScreenshotSet_Existing: a set already exists, so no POST.
func TestFindOrCreateScreenshotSet_Existing(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersionLocalizations/AC000000001/appScreenshotSets": {File: "screenshot_set_list_existing"},
	})
	c := fixtureASCClient(t, srv)
	id, err := findOrCreateScreenshotSet(context.Background(), c, "AC000000001", "APP_IPHONE_67")
	if err != nil {
		t.Fatalf("findOrCreateScreenshotSet: %v", err)
	}
	if id != "SS000000001" {
		t.Errorf("setID = %q, want SS000000001", id)
	}
}

// TestFindOrCreateScreenshotSet_Create: no set exists, so POST creates one.
func TestFindOrCreateScreenshotSet_Create(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersionLocalizations/AC000000001/appScreenshotSets": {File: "screenshot_set_list_empty"},
		"POST /v1/appScreenshotSets":                                         {File: "screenshot_set_create", Status: http.StatusCreated},
	})
	c := fixtureASCClient(t, srv)
	id, err := findOrCreateScreenshotSet(context.Background(), c, "AC000000001", "APP_IPHONE_67")
	if err != nil {
		t.Fatalf("findOrCreateScreenshotSet: %v", err)
	}
	if id != "SS000000002" {
		t.Errorf("setID = %q, want SS000000002 (newly created)", id)
	}
}

func TestListScreenshotsByChecksum(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appScreenshotSets/SS000000001/appScreenshots": {File: "screenshots_list_with_checksum"},
	})
	c := fixtureASCClient(t, srv)
	got, err := listScreenshotsByChecksum(context.Background(), c, "SS000000001")
	if err != nil {
		t.Fatalf("listScreenshotsByChecksum: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	hello := got["5d41402abc4b2a76b9719d911017c592"]
	if hello.assetID != "SH000000002" {
		t.Errorf("hello assetID = %q, want SH000000002", hello.assetID)
	}
	if hello.fileName != "02.png" {
		t.Errorf("hello fileName = %q, want 02.png", hello.fileName)
	}
}

func TestListScreenshotsByChecksum_Empty(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appScreenshotSets/SS000000001/appScreenshots": {File: "screenshots_list_empty"},
	})
	c := fixtureASCClient(t, srv)
	got, err := listScreenshotsByChecksum(context.Background(), c, "SS000000001")
	if err != nil {
		t.Fatalf("listScreenshotsByChecksum: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestProcessOneScreenshot_Skip: matching MD5 returns "skipped" without
// calling Upload (no upload route registered, so a real call would 404).
func TestProcessOneScreenshot_Skip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.png")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// MD5 of "hello" matches the fixture's 02.png slot.
	helloHash := md5HexOfHelper(t, []byte("hello"))
	existing := map[string]existingScreenshotEntry{
		helloHash: {assetID: "SH000000002", fileName: "02.png"},
	}

	srv := startFixtureServer(t, map[string]fixtureRoute{})
	c := fixtureASCClient(t, srv)

	entry, err := processOneScreenshot(context.Background(), c, "SS000000001", path, existing)
	if err != nil {
		t.Fatalf("processOneScreenshot: %v", err)
	}
	if entry.Action != "skipped" {
		t.Errorf("action = %q, want skipped", entry.Action)
	}
	if entry.AssetID != "SH000000002" {
		t.Errorf("assetID = %q, want SH000000002", entry.AssetID)
	}
	if entry.MD5 != helloHash {
		t.Errorf("md5 = %q, want %q", entry.MD5, helloHash)
	}
}

// TestProcessOneScreenshot_Upload exercises reserve -> PUT -> commit against a
// fake Apple-side server.
func TestProcessOneScreenshot_Upload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "01.png")
	body := []byte("PNG-payload-bytes")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Chunk PUT URL is served by a separate test server so the route
	// table on the ASC server stays narrow.
	chunkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(chunkSrv.Close)

	// Reserve POST returns a single upload operation pointing at chunkSrv;
	// the commit PATCH echoes back the resource as uploaded=true.
	ascSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appScreenshots":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			payload := map[string]any{
				"data": map[string]any{
					"type": "appScreenshots",
					"id":   "SH-NEW",
					"attributes": map[string]any{
						"fileName": "01.png",
						"fileSize": len(body),
						"uploadOperations": []map[string]any{{
							"method": "PUT",
							"url":    chunkSrv.URL + "/upload-chunk",
							"length": len(body),
							"offset": 0,
							"requestHeaders": []map[string]any{{
								"name":  "Content-Type",
								"value": "image/png",
							}},
						}},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshots/SH-NEW":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"type":"appScreenshots","id":"SH-NEW","attributes":{"fileName":"01.png"}}}`))
		default:
			http.Error(w, "no route: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(ascSrv.Close)

	keyPath := writeEphemeralKey(t)
	c, err := asc.New(asc.Options{
		KeyID: "TEST123ABC", IssuerID: "11111111-2222-3333-4444-555555555555",
		KeyPath: keyPath, HTTPClient: ascSrv.Client(), BaseURL: ascSrv.URL,
		UserAgent: "flightline-test/1.0",
	})
	if err != nil {
		t.Fatalf("asc.New: %v", err)
	}

	t.Setenv("FLIGHTLINE_CACHE_HOME", t.TempDir())

	entry, err := processOneScreenshot(context.Background(), c, "SS000000001", path, map[string]existingScreenshotEntry{})
	if err != nil {
		t.Fatalf("processOneScreenshot: %v", err)
	}
	if entry.Action != "uploaded" {
		t.Errorf("action = %q, want uploaded", entry.Action)
	}
	if entry.AssetID != "SH-NEW" {
		t.Errorf("assetID = %q, want SH-NEW", entry.AssetID)
	}
}

func TestScreenshotSetCreate_WireBody(t *testing.T) {
	body := screenshotSetCreateRequest{
		Data: screenshotSetCreateData{
			Type: "appScreenshotSets",
			Attributes: screenshotSetCreateAttributes{
				ScreenshotDisplayType: "APP_IPHONE_67",
			},
			Relationships: screenshotSetCreateRels{
				AppStoreVersionLocalization: screenshotSetCreateRel{
					Data: screenshotSetCreateRelRef{
						Type: "appStoreVersionLocalizations",
						ID:   "AC000000001",
					},
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	out := string(b)
	for _, want := range []string{
		`"type":"appScreenshotSets"`,
		`"screenshotDisplayType":"APP_IPHONE_67"`,
		`"appStoreVersionLocalization":`,
		`"type":"appStoreVersionLocalizations"`,
		`"id":"AC000000001"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("body missing %q: %s", want, out)
		}
	}
}

func md5HexOfHelper(t *testing.T, b []byte) string {
	t.Helper()
	h := md5.Sum(b) //nolint:gosec // test fixture, not security-sensitive
	return hex.EncodeToString(h[:])
}
