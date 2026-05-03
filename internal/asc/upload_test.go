package asc

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // matches Apple's API contract under test
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// uploadTestPayload is the deterministic 16-byte body the upload tests
// stream through reserve → 2 chunks → commit. Two chunks of 8 bytes each
// matches the offsets/lengths in the reserve fixtures.
var uploadTestPayload = []byte("ABCDEFGH01234567")

// expectedUploadMD5 is the MD5 hex of uploadTestPayload, computed once at
// init time. Used as a golden value the test asserts on rather than re-
// hashing in the test (which would tautologise the function under test).
var expectedUploadMD5 = func() string {
	h := md5.New() //nolint:gosec // MD5 is the wire format under test
	_, _ = h.Write(uploadTestPayload)
	return hex.EncodeToString(h.Sum(nil))
}()

// uploadFixture is a minimal harness that:
//   - Serves the reserve POST + commit PATCH for one of the asset kinds.
//   - Serves the chunk PUTs at /chunks/<idx>, asserting that NO
//     Authorization header arrived on inbound chunk requests.
type uploadFixture struct {
	t   *testing.T
	srv *httptest.Server

	// reserveFixture is the testdata/golden/upload/<file>.json basename
	// (sans extension) the harness loads on the reserve POST. The
	// __SIGNED_URL_BASE__ token is replaced with srv.URL at serve time.
	reserveFixture string

	// commitPath is the resource URL the commit PATCH targets, e.g.
	// "/v1/appScreenshots/upload-asset-test-1".
	commitPath string

	// reservePath is the create endpoint, e.g. "/v1/appScreenshots".
	reservePath string

	// failChunkIdx, when non-negative, makes /chunks/<idx> return 500
	// instead of 200 on its first hit. Used to simulate a mid-upload
	// failure for the resume tests.
	failChunkIdx int

	// chunkBytes records the body of each PUT, keyed by chunk index, so
	// tests can verify the right slice landed on the right chunk.
	chunkBytes map[int][]byte

	// chunkAuthSeen flips to true if any chunk PUT carried an
	// Authorization header (a contract violation).
	chunkAuthSeen atomic.Bool

	// chunkPutCount counts inbound chunk PUTs (used by the resume test
	// to assert chunk 0 is NOT re-PUT after resume).
	chunkPutCount [2]atomic.Int32
}

func newUploadFixture(t *testing.T, reserveFixture, reservePath, commitPath string) *uploadFixture {
	t.Helper()
	f := &uploadFixture{
		t:              t,
		reserveFixture: reserveFixture,
		commitPath:     commitPath,
		reservePath:    reservePath,
		failChunkIdx:   -1,
		chunkBytes:     make(map[int][]byte),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *uploadFixture) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == f.reservePath:
		body, err := readFixture("upload/" + f.reserveFixture)
		if err != nil {
			f.t.Errorf("load reserve fixture: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body = bytes.ReplaceAll(body, []byte("__SIGNED_URL_BASE__"), []byte(f.srv.URL))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
		return

	case r.Method == http.MethodGet && r.URL.Path == f.commitPath:
		// Resume path: GET /v1/<kind>/<id> to refresh upload operations.
		body, err := readFixture("upload/" + f.reserveFixture)
		if err != nil {
			f.t.Errorf("load reserve fixture (refresh): %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body = bytes.ReplaceAll(body, []byte("__SIGNED_URL_BASE__"), []byte(f.srv.URL))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return

	case r.Method == http.MethodPatch && r.URL.Path == f.commitPath:
		body, err := readFixture("upload/commit_response")
		if err != nil {
			f.t.Errorf("load commit fixture: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return

	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/chunks/"):
		var idx int
		switch r.URL.Path {
		case "/chunks/0":
			idx = 0
		case "/chunks/1":
			idx = 1
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "" {
			f.chunkAuthSeen.Store(true)
		}
		buf, _ := readAllLimit(r.Body, 1<<20)
		f.chunkBytes[idx] = buf
		f.chunkPutCount[idx].Add(1)

		if f.failChunkIdx == idx {
			// One-shot failure: clear the trip after firing once so a
			// subsequent retry under the same fixture succeeds.
			f.failChunkIdx = -1
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"errors":[{"id":"upload-fixture-no-route","title":"no route"}]}`))
}

// readAllLimit drains up to n bytes off r. Used in tests where we know the
// body is bounded.
func readAllLimit(r interface{ Read([]byte) (int, error) }, n int) ([]byte, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, 4096)
	for len(out) < n {
		nn, err := r.Read(buf)
		if nn > 0 {
			out = append(out, buf[:nn]...)
		}
		if err != nil {
			return out, nil
		}
	}
	return out, nil
}

// uploadFixtureClient returns a Client wired to f.srv with an ephemeral
// .p8. Mirrors fixtureClient but accepts an *uploadFixture wrapper.
func uploadFixtureClient(t *testing.T, f *uploadFixture) *Client {
	t.Helper()
	return fixtureClient(t, f.srv)
}

// withUploadCacheRoot points uploadCacheRoot() at t.TempDir() for the
// duration of the test via the FLINE_CACHE_HOME escape hatch.
func withUploadCacheRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FLINE_CACHE_HOME", dir)
	return dir
}

// writeUploadPayload writes uploadTestPayload to t.TempDir()/<name> and
// returns the absolute path.
func writeUploadPayload(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, uploadTestPayload, 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Happy-path: reserve → 2 chunks → commit, with the no-Authorization-header
// invariant on chunk PUTs.
// ---------------------------------------------------------------------------

func TestUpload_HappyPath_AppScreenshot(t *testing.T) {
	withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	got, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindAppScreenshot,
		ParentID: "screenshot-set-1",
		Asset:    UploadAsset{Path: path},
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if got.ID != "upload-asset-test-1" {
		t.Errorf("ID = %q, want upload-asset-test-1", got.ID)
	}
	if got.Type != "appScreenshots" {
		t.Errorf("Type = %q, want appScreenshots", got.Type)
	}
	if got.Checksum != expectedUploadMD5 {
		t.Errorf("Checksum = %q, want %q", got.Checksum, expectedUploadMD5)
	}
	if f.chunkAuthSeen.Load() {
		t.Fatal("chunk PUT carried Authorization header — Apple's CDN would reject")
	}
	if !bytes.Equal(f.chunkBytes[0], uploadTestPayload[0:8]) {
		t.Errorf("chunk 0 bytes = %q, want %q", f.chunkBytes[0], uploadTestPayload[0:8])
	}
	if !bytes.Equal(f.chunkBytes[1], uploadTestPayload[8:16]) {
		t.Errorf("chunk 1 bytes = %q, want %q", f.chunkBytes[1], uploadTestPayload[8:16])
	}
}

func TestUpload_HappyPath_IAPReviewScreenshot(t *testing.T) {
	withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_iap_review",
		"/v1/inAppPurchaseAppStoreReviewScreenshots",
		"/v1/inAppPurchaseAppStoreReviewScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "iap_review.png")

	got, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindIAPReviewScreenshot,
		ParentID: "iap-product-1",
		Asset:    UploadAsset{Path: path},
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if got.Type != "inAppPurchaseAppStoreReviewScreenshots" {
		t.Errorf("Type = %q, want inAppPurchaseAppStoreReviewScreenshots", got.Type)
	}
	if got.Checksum != expectedUploadMD5 {
		t.Errorf("Checksum = %q, want %q", got.Checksum, expectedUploadMD5)
	}
	if f.chunkAuthSeen.Load() {
		t.Fatal("chunk PUT carried Authorization header — Apple's CDN would reject")
	}
}

// ---------------------------------------------------------------------------
// Resume from checkpoint — pre-write a checkpoint with chunk 0 already
// uploaded; assert only chunk 1 is PUT'd.
// ---------------------------------------------------------------------------

func TestUpload_ResumesFromCheckpoint(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	// Pre-seed a checkpoint that says chunk 0 is already uploaded.
	cpDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(cpDir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cp := UploadCheckpoint{
		SchemaVersion:  UploadCheckpointSchemaVersion,
		AssetID:        "upload-asset-test-1",
		Kind:           AssetKindAppScreenshot.String(),
		FilePath:       path,
		FileSize:       int64(len(uploadTestPayload)),
		Md5Hex:         expectedUploadMD5,
		UploadedChunks: []int{0},
	}
	cpBuf, _ := json.MarshalIndent(cp, "", "  ")
	if err := os.WriteFile(filepath.Join(cpDir, "upload-asset-test-1.json"), cpBuf, 0o600); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	got, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "screenshot-set-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if got.ID != "upload-asset-test-1" {
		t.Errorf("ID = %q, want upload-asset-test-1", got.ID)
	}
	if n := f.chunkPutCount[0].Load(); n != 0 {
		t.Errorf("chunk 0 was re-PUT %d times; resume must skip already-uploaded chunks", n)
	}
	if n := f.chunkPutCount[1].Load(); n != 1 {
		t.Errorf("chunk 1 PUT count = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// File changed since checkpoint — assert typed ErrCheckpointMismatch.
// ---------------------------------------------------------------------------

func TestUpload_RejectsCheckpointWithDifferentMD5(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	cpDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(cpDir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cp := UploadCheckpoint{
		SchemaVersion: UploadCheckpointSchemaVersion,
		AssetID:       "upload-asset-test-1",
		Kind:          AssetKindAppScreenshot.String(),
		FilePath:      path,
		FileSize:      int64(len(uploadTestPayload)),
		// Wrong MD5 on purpose: simulates a file that mutated since the
		// failed first-run checkpoint was written.
		Md5Hex:         "ffffffffffffffffffffffffffffffff",
		UploadedChunks: []int{0},
	}
	cpBuf, _ := json.MarshalIndent(cp, "", "  ")
	if err := os.WriteFile(filepath.Join(cpDir, "upload-asset-test-1.json"), cpBuf, 0o600); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "screenshot-set-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err == nil {
		t.Fatal("Upload accepted mismatched checkpoint; want ErrCheckpointMismatch")
	}
	if !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("err = %v, want errors.Is ErrCheckpointMismatch", err)
	}
}

// ---------------------------------------------------------------------------
// Successful commit removes the checkpoint.
// ---------------------------------------------------------------------------

func TestUpload_RemovesCheckpointAfterCommit(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	if _, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindAppScreenshot,
		ParentID: "screenshot-set-1",
		Asset:    UploadAsset{Path: path},
	}); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	cpPath := filepath.Join(root, "uploads", "upload-asset-test-1.json")
	if _, err := os.Stat(cpPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("checkpoint at %s persists after successful commit (err=%v)", cpPath, err)
	}
}

// ---------------------------------------------------------------------------
// Mid-upload failure persists a checkpoint; the on-disk shape exposes
// chunk 0 as already-uploaded so a resume can skip it.
// ---------------------------------------------------------------------------

func TestUpload_PersistsCheckpointOnChunkFailure(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	f.failChunkIdx = 1 // chunk 0 succeeds, chunk 1 returns 500
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindAppScreenshot,
		ParentID: "screenshot-set-1",
		Asset:    UploadAsset{Path: path},
	})
	if err == nil {
		t.Fatal("Upload returned nil; expected chunk-PUT failure error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want substring HTTP 500", err)
	}

	cpPath := filepath.Join(root, "uploads", "upload-asset-test-1.json")
	buf, readErr := os.ReadFile(cpPath) //nolint:gosec // test-only
	if readErr != nil {
		t.Fatalf("read checkpoint: %v", readErr)
	}
	var cp UploadCheckpoint
	if err := json.Unmarshal(buf, &cp); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	if cp.AssetID != "upload-asset-test-1" {
		t.Errorf("checkpoint AssetID = %q, want upload-asset-test-1", cp.AssetID)
	}
	if cp.Md5Hex != expectedUploadMD5 {
		t.Errorf("checkpoint Md5Hex = %q, want %q", cp.Md5Hex, expectedUploadMD5)
	}
	if len(cp.UploadedChunks) != 1 || cp.UploadedChunks[0] != 0 {
		t.Errorf("checkpoint UploadedChunks = %v, want [0]", cp.UploadedChunks)
	}
}

// ---------------------------------------------------------------------------
// Validation paths.
// ---------------------------------------------------------------------------

func TestUpload_RejectsZeroAssetKind(t *testing.T) {
	withUploadCacheRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	_, err := c.Upload(context.Background(), UploadOptions{
		ParentID: "screenshot-set-1",
		Asset:    UploadAsset{Path: writeUploadPayload(t, "x.png")},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown AssetKind") {
		t.Fatalf("err = %v, want substring 'unknown AssetKind'", err)
	}
}

func TestUpload_RejectsEmptyParentID(t *testing.T) {
	withUploadCacheRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:  AssetKindAppScreenshot,
		Asset: UploadAsset{Path: writeUploadPayload(t, "x.png")},
	})
	if err == nil || !strings.Contains(err.Error(), "ParentID is required") {
		t.Fatalf("err = %v, want substring 'ParentID is required'", err)
	}
}

func TestUpload_RejectsEmptyPath(t *testing.T) {
	withUploadCacheRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindAppScreenshot,
		ParentID: "screenshot-set-1",
	})
	if err == nil || !strings.Contains(err.Error(), "Asset.Path is required") {
		t.Fatalf("err = %v, want substring 'Asset.Path is required'", err)
	}
}

func TestUpload_RejectsMissingFile(t *testing.T) {
	withUploadCacheRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindAppScreenshot,
		ParentID: "screenshot-set-1",
		Asset:    UploadAsset{Path: filepath.Join(t.TempDir(), "does-not-exist.png")},
	})
	if err == nil {
		t.Fatal("Upload accepted missing file; want stat error")
	}
}

// ---------------------------------------------------------------------------
// MD5 computation — golden vector against a known input.
// ---------------------------------------------------------------------------

func TestComputeFileMD5_GoldenVector(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "input.bin")
	if err := os.WriteFile(tmp, uploadTestPayload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := computeFileMD5(tmp)
	if err != nil {
		t.Fatalf("computeFileMD5: %v", err)
	}
	if got != expectedUploadMD5 {
		t.Errorf("md5 = %q, want %q", got, expectedUploadMD5)
	}
}

// ---------------------------------------------------------------------------
// Atomic checkpoint write — same chmod-0500 torture as AsyncState.
// Locks the upload cache subdir read-only so CreateTemp + Rename fail; the
// pre-existing checkpoint file must be byte-equivalent to its prior state.
// ---------------------------------------------------------------------------

func TestPersistCheckpoint_AtomicWriteFailurePreservesOriginal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	root := withUploadCacheRoot(t)
	uploadsDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Seed a known-good checkpoint.
	original := UploadCheckpoint{
		SchemaVersion:  UploadCheckpointSchemaVersion,
		AssetID:        "upload-asset-test-1",
		Kind:           AssetKindAppScreenshot.String(),
		FilePath:       "/tmp/screenshot.png",
		FileSize:       16,
		Md5Hex:         expectedUploadMD5,
		UploadedChunks: []int{0},
	}
	if err := persistCheckpoint(original); err != nil {
		t.Fatalf("seed persistCheckpoint: %v", err)
	}
	cpPath := filepath.Join(uploadsDir, "upload-asset-test-1.json")
	before, err := os.ReadFile(cpPath) //nolint:gosec // test-only
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}

	// Lock down the directory so CreateTemp inside it fails.
	if err := os.Chmod(uploadsDir, 0o500); err != nil {
		t.Fatalf("chmod 0500: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(uploadsDir, 0o700) })

	mutation := original
	mutation.Md5Hex = "ffffffffffffffffffffffffffffffff"
	if err := persistCheckpoint(mutation); err == nil {
		t.Fatal("persistCheckpoint succeeded despite read-only dir")
	}

	// Restore + verify the original file is byte-equivalent.
	if err := os.Chmod(uploadsDir, 0o700); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	after, err := os.ReadFile(cpPath) //nolint:gosec // test-only
	if err != nil {
		t.Fatalf("read after failed persist: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("failed persist corrupted the original checkpoint\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// ---------------------------------------------------------------------------
// Path-traversal rejection on asset IDs.
// ---------------------------------------------------------------------------

func TestUploadCheckpointPath_RejectsTraversal(t *testing.T) {
	withUploadCacheRoot(t)
	cases := []string{"", "../etc/passwd", "a/b", `a\b`, ".", ".."}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			if _, err := uploadCheckpointPath(id); err == nil {
				t.Fatalf("uploadCheckpointPath(%q) accepted; want error", id)
			}
		})
	}
}
