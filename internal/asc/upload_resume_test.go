package asc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 3.4.3 — Multipart upload resume tests.
//
// upload_test.go already covers the per-step contracts: happy path, manual
// checkpoint seed → resume skips chunk 0, MD5 mismatch returns
// ErrCheckpointMismatch, commit removes the checkpoint, mid-upload PUT
// failure persists [0] in UploadedChunks. This file adds the full
// end-to-end resume cycle the build plan calls out:
//
//   1. Fresh upload that fails on chunk 1. Verify checkpoint persists
//      with UploadedChunks=[0].
//   2. Second invocation against the same Asset.Path with
//      ResumeFromCheckpoint=true. Verify chunk 0 is NOT re-PUT and the
//      upload completes successfully.
//
// Plus a few hardening cases on the checkpoint-loader path that the
// existing tests don't cover:
//   - Corrupt checkpoint surfaces ErrCheckpointCorrupt and prevents
//     accidental re-upload of mismatched bytes.
//   - Schema-version forward-incompat is rejected with
//     ErrCheckpointCorrupt.
//   - Resume with no on-disk checkpoint falls back to a fresh reserve.

// ---------------------------------------------------------------------------
// End-to-end resume cycle: fail-then-succeed.
//
// Round 1: fixture fails chunk 1. Upload returns an error, checkpoint
// persists with UploadedChunks=[0].
// Round 2: same UploadOptions plus ResumeFromCheckpoint=true. Fixture
// recovers (failChunkIdx clears after the first hit). Upload should:
//   - NOT POST a new reserve (asset already exists).
//   - GET the existing asset to refresh upload operations.
//   - PUT only chunk 1. Chunk 0's PUT count must remain at 1 from round 1.
//   - PATCH commit, remove checkpoint.
// ---------------------------------------------------------------------------

func TestUpload_EndToEndResume_FailThenSucceed(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	// Round 1: chunk 1 fails (one-shot — server clears the trip after firing).
	f.failChunkIdx = 1
	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:     AssetKindAppScreenshot,
		ParentID: "screenshot-set-1",
		Asset:    UploadAsset{Path: path},
	})
	if err == nil {
		t.Fatal("round 1 Upload returned nil; expected chunk 1 PUT failure")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("round 1 err = %v, want substring HTTP 500", err)
	}

	// Checkpoint must exist with UploadedChunks=[0].
	cpPath := filepath.Join(root, "uploads", "upload-asset-test-1.json")
	buf, readErr := os.ReadFile(cpPath) //nolint:gosec // test-only
	if readErr != nil {
		t.Fatalf("read checkpoint after round 1: %v", readErr)
	}
	var cp UploadCheckpoint
	if err := json.Unmarshal(buf, &cp); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	if cp.AssetID != "upload-asset-test-1" {
		t.Errorf("checkpoint AssetID = %q, want upload-asset-test-1", cp.AssetID)
	}
	if len(cp.UploadedChunks) != 1 || cp.UploadedChunks[0] != 0 {
		t.Errorf("checkpoint UploadedChunks = %v, want [0]", cp.UploadedChunks)
	}
	if cp.Md5Hex != expectedUploadMD5 {
		t.Errorf("checkpoint Md5Hex = %q, want %q", cp.Md5Hex, expectedUploadMD5)
	}

	// Capture chunk 0's pre-resume PUT count so we can prove the resume
	// did NOT re-PUT it.
	chunk0Before := f.chunkPutCount[0].Load()
	if chunk0Before != 1 {
		t.Fatalf("round 1: chunk 0 PUT count = %d, want 1", chunk0Before)
	}

	// Round 2: resume. failChunkIdx already cleared by the one-shot.
	got, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "screenshot-set-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err != nil {
		t.Fatalf("round 2 Upload: %v", err)
	}
	if got.ID != "upload-asset-test-1" {
		t.Errorf("round 2 ID = %q, want upload-asset-test-1", got.ID)
	}
	if got.Checksum != expectedUploadMD5 {
		t.Errorf("round 2 Checksum = %q, want %q", got.Checksum, expectedUploadMD5)
	}

	// Chunk 0 must NOT have been re-PUT.
	if got := f.chunkPutCount[0].Load(); got != chunk0Before {
		t.Errorf("chunk 0 PUT count = %d after resume; want unchanged at %d", got, chunk0Before)
	}
	// Chunk 1 was tried in round 1 (failed) and again in round 2 (succeeded).
	if got := f.chunkPutCount[1].Load(); got != 2 {
		t.Errorf("chunk 1 PUT count = %d, want 2 (failed once, succeeded once)", got)
	}

	// Stale checkpoint cleanup: successful commit removed the file.
	if _, err := os.Stat(cpPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("checkpoint at %s persists after successful resume (err=%v)", cpPath, err)
	}
}

// ---------------------------------------------------------------------------
// Resume with no on-disk checkpoint falls back to a fresh reserve.
// Sanity check the resolveUploadPlan branch when ResumeFromCheckpoint=true
// but no checkpoint exists.
// ---------------------------------------------------------------------------

func TestUpload_ResumeWithoutCheckpoint_FreshReserve(t *testing.T) {
	withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	got, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "screenshot-set-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true, // no checkpoint exists yet
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if got.ID != "upload-asset-test-1" {
		t.Errorf("ID = %q, want upload-asset-test-1", got.ID)
	}
	if f.chunkPutCount[0].Load() != 1 || f.chunkPutCount[1].Load() != 1 {
		t.Errorf("chunk PUT counts = (%d, %d), want (1, 1)",
			f.chunkPutCount[0].Load(), f.chunkPutCount[1].Load())
	}
}

// ---------------------------------------------------------------------------
// Corrupt checkpoint — JSON garbage at the expected path. tryLoadCheckpoint
// ForAsset scans the directory; a single corrupt entry returns
// ErrCheckpointCorrupt rather than silently failing over.
// ---------------------------------------------------------------------------

func TestUpload_CorruptCheckpoint_SurfacesTypedError(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "screenshot.png")

	// Pre-seed an unparseable checkpoint at the expected path.
	cpDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(cpDir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	corrupt := []byte("not json at all")
	if err := os.WriteFile(filepath.Join(cpDir, "upload-asset-test-1.json"), corrupt, 0o600); err != nil {
		t.Fatalf("seed corrupt checkpoint: %v", err)
	}

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "screenshot-set-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err == nil {
		t.Fatal("Upload accepted corrupt checkpoint; want ErrCheckpointCorrupt")
	}
	if !errors.Is(err, ErrCheckpointCorrupt) {
		t.Fatalf("err = %v, want errors.Is ErrCheckpointCorrupt", err)
	}
}

// ---------------------------------------------------------------------------
// Schema-version forward-incompat — a checkpoint written by a future build
// is rejected with ErrCheckpointCorrupt rather than silently truncating
// fields it can't decode.
// ---------------------------------------------------------------------------

func TestUpload_FutureSchemaVersionCheckpoint_Rejected(t *testing.T) {
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
		SchemaVersion:  UploadCheckpointSchemaVersion + 1, // future version
		AssetID:        "upload-asset-test-1",
		Kind:           AssetKindAppScreenshot.String(),
		FilePath:       path,
		FileSize:       int64(len(uploadTestPayload)),
		Md5Hex:         expectedUploadMD5,
		UploadedChunks: []int{0},
	}
	cpBuf, _ := json.MarshalIndent(cp, "", "  ")
	if err := os.WriteFile(filepath.Join(cpDir, "upload-asset-test-1.json"), cpBuf, 0o600); err != nil {
		t.Fatalf("seed future-schema checkpoint: %v", err)
	}

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "screenshot-set-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err == nil {
		t.Fatal("Upload accepted future-schema checkpoint; want ErrCheckpointCorrupt")
	}
	if !errors.Is(err, ErrCheckpointCorrupt) {
		t.Fatalf("err = %v, want errors.Is ErrCheckpointCorrupt", err)
	}
}

// ---------------------------------------------------------------------------
// Checkpoint kind mismatch — the checkpoint records appScreenshot but the
// caller passes IAPReviewScreenshot. validateCheckpointForReuse rejects.
// ---------------------------------------------------------------------------

func TestUpload_CheckpointKindMismatch_Rejected(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_iap_review",
		"/v1/inAppPurchaseAppStoreReviewScreenshots",
		"/v1/inAppPurchaseAppStoreReviewScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)
	path := writeUploadPayload(t, "iap_review.png")

	cpDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(cpDir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cp := UploadCheckpoint{
		SchemaVersion: UploadCheckpointSchemaVersion,
		AssetID:       "upload-asset-test-1",
		// Wrong kind: was uploaded as appScreenshot, caller now wants
		// iapReviewScreenshot. Reject loudly.
		Kind:           AssetKindAppScreenshot.String(),
		FilePath:       path,
		FileSize:       int64(len(uploadTestPayload)),
		Md5Hex:         expectedUploadMD5,
		UploadedChunks: []int{0},
	}
	cpBuf, _ := json.MarshalIndent(cp, "", "  ")
	if err := os.WriteFile(filepath.Join(cpDir, "upload-asset-test-1.json"), cpBuf, 0o600); err != nil {
		t.Fatalf("seed wrong-kind checkpoint: %v", err)
	}

	_, err := c.Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindIAPReviewScreenshot,
		ParentID:             "iap-product-1",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err == nil {
		t.Fatal("Upload accepted wrong-kind checkpoint; want kind-mismatch error")
	}
	if !strings.Contains(err.Error(), "checkpoint kind") {
		t.Errorf("err = %v, want substring 'checkpoint kind'", err)
	}
}

// ---------------------------------------------------------------------------
// Stale-but-orphan checkpoint cleanup — when a checkpoint exists for a
// different file path, it does NOT match the current upload and the helper
// proceeds with a fresh reserve (rather than mistakenly resuming someone
// else's asset).
// ---------------------------------------------------------------------------

func TestUpload_OrphanCheckpoint_DoesNotInterfereWithFreshReserve(t *testing.T) {
	root := withUploadCacheRoot(t)
	f := newUploadFixture(t,
		"reserve_screenshot",
		"/v1/appScreenshots",
		"/v1/appScreenshots/upload-asset-test-1",
	)
	c := uploadFixtureClient(t, f)

	// Pre-seed a checkpoint that references a DIFFERENT local file path.
	// tryLoadCheckpointForAsset matches on absolute path; this one shouldn't
	// match, so resume path falls through to fresh reserve.
	cpDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(cpDir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	otherPath := filepath.Join(t.TempDir(), "some-other-file.png")
	if err := os.WriteFile(otherPath, []byte("other"), 0o600); err != nil {
		t.Fatalf("write other file: %v", err)
	}
	orphan := UploadCheckpoint{
		SchemaVersion:  UploadCheckpointSchemaVersion,
		AssetID:        "orphan-asset-id",
		Kind:           AssetKindAppScreenshot.String(),
		FilePath:       otherPath,
		FileSize:       5,
		Md5Hex:         "00000000000000000000000000000000",
		UploadedChunks: []int{0},
	}
	cpBuf, _ := json.MarshalIndent(orphan, "", "  ")
	if err := os.WriteFile(filepath.Join(cpDir, "orphan-asset-id.json"), cpBuf, 0o600); err != nil {
		t.Fatalf("seed orphan checkpoint: %v", err)
	}

	// Fresh upload of the real payload — should reserve a new asset and
	// upload both chunks normally.
	path := writeUploadPayload(t, "screenshot.png")
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
		t.Errorf("ID = %q, want upload-asset-test-1 (fresh reserve)", got.ID)
	}
	// Both chunks must have been PUT (not skipped via the orphan checkpoint).
	if f.chunkPutCount[0].Load() != 1 {
		t.Errorf("chunk 0 PUT count = %d, want 1 (orphan checkpoint must not skip)", f.chunkPutCount[0].Load())
	}
	if f.chunkPutCount[1].Load() != 1 {
		t.Errorf("chunk 1 PUT count = %d, want 1", f.chunkPutCount[1].Load())
	}

	// Orphan checkpoint should still be on disk — we didn't commit against
	// its asset ID.
	orphanPath := filepath.Join(cpDir, "orphan-asset-id.json")
	if _, err := os.Stat(orphanPath); err != nil {
		t.Errorf("orphan checkpoint disappeared (err=%v); fresh reserve should not have touched it", err)
	}
}
