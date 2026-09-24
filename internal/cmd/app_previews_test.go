package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF6A_PreviewUploadBlocksUnresolvedSameFilenameWithoutPost(t *testing.T) {
	t.Setenv("FLIGHTLINE_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "preview.mov")
	if err := os.WriteFile(file, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts++
			t.Errorf("unexpected POST %s", r.URL)
			return
		}
		switch r.URL.Path {
		case "/v1/appStoreVersionLocalizations/L1/appPreviewSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"}}],"links":{}}`))
		case "/v1/appPreviewSets/S1/appPreviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P1","attributes":{"fileName":"preview.mov","videoDeliveryState":{"state":"AWAITING_UPLOAD"}}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := uploadPreviewWithClient(context.Background(), fixtureASCClient(t, srv), asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: "L1"}, "IPHONE_67", file, "", false, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "P1") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
	_, err = uploadPreviewWithClient(context.Background(), fixtureASCClient(t, srv), asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: "L1"}, "IPHONE_67", file, "", true, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "checkpoint") || posts != 0 {
		t.Fatalf("resume err=%v posts=%d", err, posts)
	}
}

func TestF6A_PreviewCommandRequiresExplicitMutationConfirmation(t *testing.T) {
	root := newAppPreviewsCommand()
	for _, name := range []string{"upload", "delete"} {
		child, _, err := root.Find([]string{name})
		if err != nil || child == nil || child.Flags().Lookup("confirm") == nil {
			t.Fatalf("%s lacks confirm: %v", name, err)
		}
	}
}

func TestF6A_PreviewFrameUpdateReportsChangedAndWaitsAgain(t *testing.T) {
	gets, patches := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/appPreviewSets/S1/appPreviews":
			_, _ = w.Write([]byte(`{"data":[{"type":"appPreviews","id":"P1","attributes":{"fileName":"one.mov","sourceFileChecksum":"ABC","previewFrameTimeCode":"old","videoDeliveryState":{"state":"COMPLETE"}}}],"links":{}}`))
		case "/v1/appPreviews/P1":
			if r.Method == http.MethodPatch {
				patches++
				_, _ = w.Write([]byte(`{"data":{"type":"appPreviews","id":"P1","relationships":{"appPreviewSet":{"data":{"type":"appPreviewSets","id":"S1"}}},"attributes":{"previewFrameTimeCode":"new","videoDeliveryState":{"state":"PROCESSING"}}}}`))
				return
			}
			gets++
			frame := "old"
			if patches != 0 {
				frame = "new"
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appPreviews","id":"P1","relationships":{"appPreviewSet":{"data":{"type":"appPreviewSets","id":"S1"}}},"attributes":{"previewFrameTimeCode":"` + frame + `","videoDeliveryState":{"state":"COMPLETE"}}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	result, found, err := reusePreviewUpload(context.Background(), fixtureASCClient(t, srv), asc.AppPreviewSet{ID: "S1"}, "one.mov", "ABC", "new", false, asc.AssetPollOptions{MaxAttempts: 2})
	if err != nil || !found || !result.Changed || result.Action != "updated" || patches != 1 || gets < 3 {
		t.Fatalf("result=%+v found=%v err=%v gets=%d patches=%d", result, found, err, gets, patches)
	}
}

func TestF6A_InvalidPollPreventsPreviewSetCreation(t *testing.T) {
	if _, err := uploadPreviewWithClient(context.Background(), nil, asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: "L1"}, "IPHONE_67", "missing.mov", "", false, asc.AssetPollOptions{}); err == nil {
		t.Fatal("invalid polling reached client")
	}
}

func TestF6A_CPPPreviewMutationRejectsPublishedOnlyBeforeWrites(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes++
			t.Errorf("unexpected write %s", r.URL)
			return
		}
		switch r.URL.Path {
		case "/v1/apps/APP1/appCustomProductPages":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPages","id":"PAGE1","attributes":{"name":"Holiday"}}],"links":{}}`))
		case "/v1/appCustomProductPages/PAGE1/appCustomProductPageVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appCustomProductPageVersions","id":"V1","attributes":{"version":"1","state":"APPROVED"}}],"links":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := resolveEditableCPPPreviewParent(context.Background(), fixtureASCClient(t, srv), "APP1", "Holiday", "en-US")
	if err == nil || !strings.Contains(err.Error(), "no editable version") || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestF6A_PendingAssetResumeUsesSameIDWithoutReserve(t *testing.T) {
	for _, tc := range []pendingResumeCase{
		{"preview", asc.AssetKindAppPreview, "appPreviews", "appPreviewSets", "S1", "P1", "preview.mov", "/v1/appPreviewSets/S1/appPreviews"},
		{"review attachment", asc.AssetKindReviewAttachment, "appStoreReviewAttachments", "appStoreReviewDetails", "D1", "A1", "review.pdf", "/v1/appStoreReviewDetails/D1/appStoreReviewAttachments"},
	} {
		t.Run(tc.name, func(t *testing.T) { runPendingResumeCase(t, tc) })
	}
}

type pendingResumeCase struct {
	name                                                            string
	kind                                                            asc.AssetKind
	resourceType, parentType, parentID, assetID, fileName, listPath string
}

func runPendingResumeCase(t *testing.T, tc pendingResumeCase) {
	t.Helper()
	file := seedPendingResumeCheckpoint(t, tc)
	fixture := &pendingResumeFixture{t: t, tc: tc}
	srv := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer srv.Close()
	fixture.serverURL = srv.URL
	client := fixtureASCClient(t, srv)
	if tc.kind == asc.AssetKindAppPreview {
		result, err := uploadPreviewWithClient(context.Background(), client, asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: "L1"}, "IPHONE_67", file, "", true, asc.AssetPollOptions{MaxAttempts: 2})
		if err != nil || result.PreviewID != tc.assetID || result.Action != "resumed" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	} else {
		result, err := uploadReviewAttachmentWithClient(context.Background(), client, tc.parentID, file, true, asc.AssetPollOptions{MaxAttempts: 2})
		if err != nil || result.AttachmentID != tc.assetID || result.Action != "resumed" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	if fixture.posts != 0 || fixture.chunks != 1 || fixture.commits != 1 {
		t.Fatalf("posts=%d chunks=%d commits=%d", fixture.posts, fixture.chunks, fixture.commits)
	}
}

func seedPendingResumeCheckpoint(t *testing.T, tc pendingResumeCase) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("FLIGHTLINE_CACHE_HOME", root)
	file := filepath.Join(t.TempDir(), tc.fileName)
	if err := os.WriteFile(file, []byte("ABCDEFGH01234567"), 0o600); err != nil {
		t.Fatal(err)
	}
	checksum, err := md5HexOfFile(file)
	if err != nil {
		t.Fatal(err)
	}
	cp := asc.UploadCheckpoint{SchemaVersion: asc.UploadCheckpointSchemaVersion, AssetID: tc.assetID, Kind: tc.kind.String(), ResourceType: tc.resourceType, ParentType: tc.parentType, ParentID: tc.parentID, FilePath: file, FileSize: 16, Md5Hex: checksum}
	cpData, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uploads", tc.assetID+".json"), cpData, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

type pendingResumeFixture struct {
	t                      *testing.T
	tc                     pendingResumeCase
	serverURL              string
	posts, chunks, commits int
}

func (f *pendingResumeFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost:
		f.posts++
		f.t.Errorf("unexpected reserve POST %s", r.URL)
	case r.Method == http.MethodPut && r.URL.Path == "/chunk":
		f.chunks++
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPatch && r.URL.Path == f.resourcePath():
		f.commits++
		_, _ = fmt.Fprintf(w, `{"data":{"type":%q,"id":%q}}`, f.tc.resourceType, f.tc.assetID)
	case r.URL.Path == "/v1/appStoreVersionLocalizations/L1/appPreviewSets":
		_, _ = w.Write([]byte(`{"data":[{"type":"appPreviewSets","id":"S1","attributes":{"previewType":"IPHONE_67"}}],"links":{}}`))
	case r.URL.Path == f.tc.listPath:
		f.writeList(w)
	case r.URL.Path == f.resourcePath():
		f.writeResource(w)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}
}

func (f *pendingResumeFixture) resourcePath() string {
	return "/v1/" + f.tc.resourceType + "/" + f.tc.assetID
}
func (f *pendingResumeFixture) state() string {
	if f.commits != 0 {
		return "COMPLETE"
	}
	return "AWAITING_UPLOAD"
}
func (f *pendingResumeFixture) stateField() string {
	if f.tc.kind == asc.AssetKindReviewAttachment {
		return "assetDeliveryState"
	}
	return "videoDeliveryState"
}

func (f *pendingResumeFixture) writeList(w http.ResponseWriter) {
	_, _ = fmt.Fprintf(w, `{"data":[{"type":%q,"id":%q,"attributes":{"fileName":%q,%q:{"state":%q}}}],"links":{}}`, f.tc.resourceType, f.tc.assetID, f.tc.fileName, f.stateField(), f.state())
}

func (f *pendingResumeFixture) writeResource(w http.ResponseWriter) {
	rel := "appPreviewSet"
	if f.tc.kind == asc.AssetKindReviewAttachment {
		rel = "appStoreReviewDetail"
	}
	_, _ = fmt.Fprintf(w, `{"data":{"type":%q,"id":%q,"relationships":{%q:{"data":{"type":%q,"id":%q}}},"attributes":{"fileName":%q,%q:{"state":%q},"uploadOperations":[{"method":"PUT","url":%q,"length":16,"offset":0}]}}}`, f.tc.resourceType, f.tc.assetID, rel, f.tc.parentType, f.tc.parentID, f.tc.fileName, f.stateField(), f.state(), f.serverURL+"/chunk")
}

func TestF6A_ResumeWithoutObservedPendingAssetNeverReserves(t *testing.T) {
	file := filepath.Join(t.TempDir(), "preview.mov")
	if err := os.WriteFile(file, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			t.Errorf("unexpected POST %s", r.URL)
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
	}))
	defer srv.Close()
	_, err := uploadPreviewWithClient(context.Background(), fixtureASCClient(t, srv), asc.PreviewParent{Type: "appStoreVersionLocalizations", ID: "L1"}, "IPHONE_67", file, "", true, asc.AssetPollOptions{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "no matching pending") || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}
