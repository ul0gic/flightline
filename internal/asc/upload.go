// Package asc — multipart upload helper.
//
// Apple's media-asset upload (app screenshots, IAP review screenshots, app
// previews) is a 3-step dance:
//
//  1. Reserve — POST to the create endpoint with attributes.fileSize and
//     attributes.fileName plus the parent relationship (screenshot set, IAP,
//     preview set). Apple returns the new resource with attributes.
//     uploadOperations[], one entry per chunk: method, url (pre-signed CDN),
//     length, offset, and the exact requestHeaders Apple expects.
//  2. PUT chunks — for each upload operation, slice the file at
//     [offset, offset+length) and PUT it to the pre-signed URL with Apple's
//     headers. NO Authorization header on these PUTs; Apple's CDN signs the
//     URL with a query-string SHA and any extra header at the wrong layer
//     flips the signature. Same contract as DownloadAnalyticsSegment.
//  3. Commit — PATCH the resource with attributes.uploaded=true and
//     attributes.sourceFileChecksum=<md5-hex>. Apple computes the MD5 itself
//     and matches; a mismatch flips assetDeliveryState.state to FAILED.
//
// Resumable. Between chunk PUTs we persist a checkpoint to
// $XDG_CACHE_HOME/skipper/uploads/<assetId>.json (cache, not state — uploads
// are recoverable from the file on disk + the Apple-assigned asset ID, no
// need for state-tier durability). On Ctrl-C, a re-invocation with
// ResumeFromCheckpoint=true skips chunks already uploaded and re-PUTs only
// the failed ones.
//
// Public surface consumed by Phase 3.1.5 (screenshots upload) and 3.2.1
// (IAP review screenshot upload). Renaming or removing exported symbols
// here is a breaking change — see .project/build-plan.md.

package asc

import (
	"context"
	"crypto/md5" //nolint:gosec // Apple's API requires MD5 for upload integrity (sourceFileChecksum)
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// UploadCheckpointSchemaVersion is the on-disk JSON schema version for
// upload checkpoints. Bump when UploadCheckpoint's shape changes; loadUpload
// Checkpoint rejects files with an unrecognised SchemaVersion (forward-
// incompat by design — same gate as AsyncState).
const UploadCheckpointSchemaVersion = 1

// uploadDownloadCapBytes is the upper bound on a single chunk read into
// memory before PUTting. Apple's chunks are typically <16 MiB; 64 MiB is
// generous defense against a runaway operation.length value.
const uploadDownloadCapBytes = 64 << 20

// AssetKind selects which Apple endpoint to reserve against. Each kind
// pins the resource type wire-string, the create-endpoint path, the
// patch-endpoint path, and the relationship name.
type AssetKind int

// Asset kinds. Defined as iota+1 so the zero value is invalid and forces
// callers to pick one; we surface a typed error on AssetKind(0).
const (
	// AssetKindAppScreenshot reserves under /v1/appScreenshots with a
	// parent appScreenshotSet.
	AssetKindAppScreenshot AssetKind = iota + 1
	// AssetKindIAPReviewScreenshot reserves under
	// /v1/inAppPurchaseAppStoreReviewScreenshots with a parent inAppPurchaseV2.
	AssetKindIAPReviewScreenshot
	// AssetKindAppPreview reserves under /v1/appPreviews with a parent
	// appPreviewSet.
	AssetKindAppPreview
)

// String returns the canonical name used in checkpoint files and error
// messages. Stable JSON contract — renames break checkpoint compat.
func (k AssetKind) String() string {
	switch k {
	case AssetKindAppScreenshot:
		return "appScreenshot"
	case AssetKindIAPReviewScreenshot:
		return "iapReviewScreenshot"
	case AssetKindAppPreview:
		return "appPreview"
	default:
		return fmt.Sprintf("AssetKind(%d)", int(k))
	}
}

// kindEndpoints captures the per-kind wire details: which collection path
// to POST against, which resource path to PATCH, the parent relationship
// name, the parent type literal, and the resource type literal.
type kindEndpoints struct {
	collectionPath string
	resourceType   string
	relationship   string
	parentType     string
}

func (k AssetKind) endpoints() (kindEndpoints, error) {
	switch k {
	case AssetKindAppScreenshot:
		return kindEndpoints{
			collectionPath: "/v1/appScreenshots",
			resourceType:   "appScreenshots",
			relationship:   "appScreenshotSet",
			parentType:     "appScreenshotSets",
		}, nil
	case AssetKindIAPReviewScreenshot:
		return kindEndpoints{
			collectionPath: "/v1/inAppPurchaseAppStoreReviewScreenshots",
			resourceType:   "inAppPurchaseAppStoreReviewScreenshots",
			relationship:   "inAppPurchaseV2",
			parentType:     "inAppPurchases",
		}, nil
	case AssetKindAppPreview:
		return kindEndpoints{
			collectionPath: "/v1/appPreviews",
			resourceType:   "appPreviews",
			relationship:   "appPreviewSet",
			parentType:     "appPreviewSets",
		}, nil
	default:
		return kindEndpoints{}, fmt.Errorf("asc: unknown AssetKind %d (use AssetKindAppScreenshot, AssetKindIAPReviewScreenshot, or AssetKindAppPreview)", int(k))
	}
}

// UploadAsset describes one file to upload.
//
// Path is the local file path. FileSize defaults to the on-disk size when
// zero; pass non-zero only to override (rare — useful in tests). FileName
// defaults to filepath.Base(Path); Apple stores it on the resource and uses
// it for display in App Store Connect.
type UploadAsset struct {
	Path     string
	FileSize int64
	FileName string
}

// UploadOptions configures one upload session.
//
// Kind selects the Apple endpoint family. ParentID is the parent resource
// ID (an appScreenshotSet ID for screenshots, an inAppPurchase ID for IAP
// review screenshots, an appPreviewSet ID for previews). Asset names the
// local file. ResumeFromCheckpoint=true reads the on-disk checkpoint for
// the asset (if any) and skips already-uploaded chunks.
type UploadOptions struct {
	Kind                 AssetKind
	ParentID             string
	Asset                UploadAsset
	ResumeFromCheckpoint bool
}

// UploadResult names the created Apple resource after a successful commit.
// Checksum is the hex-encoded MD5 we computed locally and sent to Apple as
// sourceFileChecksum — exposed so callers can persist it alongside the
// asset ID for idempotency checks at the cmd layer.
type UploadResult struct {
	ID       string
	Type     string
	Checksum string
}

// UploadCheckpoint is the on-disk shape of an in-progress upload.
//
// Stable JSON contract:
//   - SchemaVersion gates forward-compat (see UploadCheckpointSchemaVersion).
//   - AssetID is the Apple-assigned ID returned by the reserve POST.
//   - Kind is the asset kind name (AssetKind.String()).
//   - FilePath / FileSize / Md5Hex pin the local source; a different file
//     with the same intended slot reports as a typed error rather than
//     silently re-uploading wrong bytes.
//   - UploadedChunks lists zero-based operation indices that have already
//     succeeded; the resume path skips these.
//   - StartedAt / LastUpdate are RFC3339 UTC timestamps.
type UploadCheckpoint struct {
	SchemaVersion  int       `json:"schemaVersion"`
	AssetID        string    `json:"assetId"`
	Kind           string    `json:"kind"`
	FilePath       string    `json:"filePath"`
	FileSize       int64     `json:"fileSize"`
	Md5Hex         string    `json:"md5Hex"`
	UploadedChunks []int     `json:"uploadedChunks"`
	StartedAt      time.Time `json:"startedAt"`
	LastUpdate     time.Time `json:"lastUpdate"`
}

// ErrCheckpointMismatch is returned by Upload when ResumeFromCheckpoint=true
// and the local file's MD5 differs from the checkpoint's stored hash.
// Surfacing this loudly is on purpose: silently re-uploading mutated bytes
// to a half-written Apple asset is worse than refusing to continue.
var ErrCheckpointMismatch = errors.New("asc: upload checkpoint does not match local file (file changed since checkpoint)")

// ErrCheckpointCorrupt is returned when an upload checkpoint exists but
// cannot be decoded (truncated write, JSON corruption, schema mismatch).
// Same semantics as ErrStateCorrupt.
var ErrCheckpointCorrupt = errors.New("asc: upload checkpoint file is corrupt or unreadable")

// ---------------------------------------------------------------------------
// Wire envelopes — typed against openapi.oas.json
// ---------------------------------------------------------------------------

// uploadOperation mirrors components.schemas.UploadOperation.
type uploadOperation struct {
	Method         string             `json:"method"`
	URL            string             `json:"url"`
	Length         int64              `json:"length"`
	Offset         int64              `json:"offset"`
	RequestHeaders []uploadHTTPHeader `json:"requestHeaders"`
}

// uploadHTTPHeader mirrors components.schemas.HttpHeader.
type uploadHTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// uploadAssetAttributes mirrors the subset of AppScreenshot /
// InAppPurchaseAppStoreReviewScreenshot / AppPreview attributes that the
// upload helper needs. Other shared attribute structs (e.g.
// IAPReviewScreenshotAttributes) intentionally don't carry uploadOperations
// because their consumers don't need it; we keep the upload-specific shape
// here.
type uploadAssetAttributes struct {
	FileSize         int64             `json:"fileSize,omitempty"`
	FileName         string            `json:"fileName,omitempty"`
	UploadOperations []uploadOperation `json:"uploadOperations,omitempty"`
}

// reserveRequest builds the JSON:API create-request body. Generic across
// all three asset kinds because Apple uses identical shape for each, just
// with different type/relationship literals.
type reserveRequest struct {
	Data reserveRequestData `json:"data"`
}

type reserveRequestData struct {
	Type          string                       `json:"type"`
	Attributes    reserveRequestAttributes     `json:"attributes"`
	Relationships map[string]reserveRequestRel `json:"relationships"`
}

type reserveRequestAttributes struct {
	FileSize int64  `json:"fileSize"`
	FileName string `json:"fileName"`
}

type reserveRequestRel struct {
	Data reserveRequestRelRef `json:"data"`
}

type reserveRequestRelRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// commitRequest patches the resource with uploaded=true and the MD5.
type commitRequest struct {
	Data commitRequestData `json:"data"`
}

type commitRequestData struct {
	Type       string                  `json:"type"`
	ID         string                  `json:"id"`
	Attributes commitRequestAttributes `json:"attributes"`
}

type commitRequestAttributes struct {
	Uploaded           bool   `json:"uploaded"`
	SourceFileChecksum string `json:"sourceFileChecksum"`
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// Upload performs the full reserve → PUT chunks → commit lifecycle for a
// single asset. On success returns the created resource's ID, type, and
// the MD5 hex Apple was told to verify against. On failure between PUTs,
// the on-disk checkpoint is preserved and a re-invocation with
// opts.ResumeFromCheckpoint=true skips chunks already uploaded.
//
// Validation:
//   - opts.Kind must be a known AssetKind.
//   - opts.ParentID must be non-empty (Apple's parent resource ID).
//   - opts.Asset.Path must point to a readable regular file.
//
// Wire flow:
//
//  1. POST opts.Kind.endpoints().collectionPath with attributes.fileSize,
//     attributes.fileName, and the parent relationship. Apple returns the
//     new resource with attributes.uploadOperations[].
//  2. For each upload operation, slice the file at [offset, offset+length)
//     and PUT to operation.url with operation.requestHeaders. NO bearer
//     token; uses http.DefaultClient (mirrors DownloadAnalyticsSegment).
//  3. PATCH the resource with uploaded=true + sourceFileChecksum=<md5-hex>.
//
// Errors:
//   - ErrCheckpointMismatch when ResumeFromCheckpoint=true and the local
//     file's MD5 differs from the on-disk checkpoint.
//   - ErrCheckpointCorrupt when a checkpoint exists but is unreadable.
//   - *APIError on Apple non-2xx (typed, with errors[] payload).
//   - Plain wrapped errors on chunk-PUT HTTP failures (status + redacted
//     URL host; pre-signed query strings are not echoed).
//
// No retry logic in v1 — a failed chunk PUT returns immediately. Callers
// drive resume by re-running the command with the resume flag.
func (c *Client) Upload(ctx context.Context, opts UploadOptions) (UploadResult, error) {
	endpoints, asset, md5Hex, err := prepareUpload(opts)
	if err != nil {
		return UploadResult{}, err
	}

	plan, err := resolveUploadPlan(ctx, c, endpoints, opts, asset, md5Hex)
	if err != nil {
		return UploadResult{}, err
	}
	if len(plan.operations) == 0 {
		return UploadResult{}, fmt.Errorf("asc: Upload: Apple returned zero upload operations for asset %s", plan.assetID)
	}

	// PUT each chunk that isn't already uploaded.
	if err := putChunks(ctx, asset.Path, asset.FileSize, plan.operations, plan.uploaded, func(idx int) error {
		plan.uploaded[idx] = struct{}{}
		return persistCheckpoint(UploadCheckpoint{
			AssetID:        plan.assetID,
			Kind:           opts.Kind.String(),
			FilePath:       asset.Path,
			FileSize:       asset.FileSize,
			Md5Hex:         md5Hex,
			UploadedChunks: sortedIndices(plan.uploaded),
			StartedAt:      plan.startedAt,
			LastUpdate:     time.Now().UTC(),
		})
	}); err != nil {
		return UploadResult{}, err
	}

	// Commit.
	if err := commitAsset(ctx, c, endpoints, plan.assetID, md5Hex); err != nil {
		return UploadResult{}, err
	}

	// Successful commit — the checkpoint is no longer needed. Best-effort
	// remove; failure to clean up is non-fatal (the user can rm it later
	// or a future invocation will overwrite).
	_ = removeCheckpoint(plan.assetID)

	return UploadResult{
		ID:       plan.assetID,
		Type:     endpoints.resourceType,
		Checksum: md5Hex,
	}, nil
}

// prepareUpload validates the options, normalizes the asset descriptor, and
// hashes the local file. Pulled out of Upload so the top-level function
// stays under the gocyclo bar.
func prepareUpload(opts UploadOptions) (kindEndpoints, UploadAsset, string, error) {
	endpoints, err := opts.Kind.endpoints()
	if err != nil {
		return kindEndpoints{}, UploadAsset{}, "", err
	}
	if opts.ParentID == "" {
		return kindEndpoints{}, UploadAsset{}, "", fmt.Errorf("asc: Upload: ParentID is required (the %s ID)", endpoints.parentType)
	}
	if opts.Asset.Path == "" {
		return kindEndpoints{}, UploadAsset{}, "", errors.New("asc: Upload: Asset.Path is required")
	}
	asset, err := normalizeAsset(opts.Asset)
	if err != nil {
		return kindEndpoints{}, UploadAsset{}, "", err
	}
	md5Hex, err := computeFileMD5(asset.Path)
	if err != nil {
		return kindEndpoints{}, UploadAsset{}, "", fmt.Errorf("asc: Upload: %w", err)
	}
	return endpoints, asset, md5Hex, nil
}

// uploadPlan is the resolved set of "what's left to upload" for one
// session: the assigned asset ID, the (still-valid) upload operations
// array, the set of chunk indices already uploaded (empty for a fresh
// reserve), and the original session start time (used by checkpoint
// persistence).
type uploadPlan struct {
	assetID    string
	operations []uploadOperation
	uploaded   map[int]struct{}
	startedAt  time.Time
}

// resolveUploadPlan picks between resuming an existing checkpoint and
// reserving a fresh asset, then returns either way the operations to
// upload and the set of chunks already done. Mid-resume re-fetches the
// resource to refresh Apple's pre-signed chunk URLs (they expire).
func resolveUploadPlan(
	ctx context.Context,
	c *Client,
	endpoints kindEndpoints,
	opts UploadOptions,
	asset UploadAsset,
	md5Hex string,
) (uploadPlan, error) {
	plan := uploadPlan{
		uploaded:  make(map[int]struct{}),
		startedAt: time.Now().UTC(),
	}

	if opts.ResumeFromCheckpoint {
		cp, found, err := tryLoadCheckpointForAsset(asset.Path)
		if err != nil {
			return uploadPlan{}, err
		}
		if found {
			if err := validateCheckpointForReuse(cp, opts.Kind, asset.Path, md5Hex); err != nil {
				return uploadPlan{}, err
			}
			plan.assetID = cp.AssetID
			plan.startedAt = cp.StartedAt
			for _, idx := range cp.UploadedChunks {
				plan.uploaded[idx] = struct{}{}
			}
		}
	}

	if plan.assetID == "" {
		reserved, err := reserveAsset(ctx, c, endpoints, opts.ParentID, asset)
		if err != nil {
			return uploadPlan{}, err
		}
		plan.assetID = reserved.ID
		plan.operations = reserved.Attributes.UploadOperations
		return plan, nil
	}

	got, err := getReservedAsset(ctx, c, endpoints, plan.assetID)
	if err != nil {
		return uploadPlan{}, err
	}
	plan.operations = got.Attributes.UploadOperations
	return plan, nil
}

// validateCheckpointForReuse asserts that a loaded checkpoint matches the
// caller's intent: same kind, same file MD5. Surfaces typed errors so
// callers can branch on errors.Is(err, ErrCheckpointMismatch).
func validateCheckpointForReuse(cp UploadCheckpoint, kind AssetKind, path, md5Hex string) error {
	if cp.Md5Hex != md5Hex {
		return fmt.Errorf("%w: %s (checkpoint md5 %s, file md5 %s)",
			ErrCheckpointMismatch, path, cp.Md5Hex, md5Hex)
	}
	if cp.Kind != kind.String() {
		return fmt.Errorf("asc: Upload: checkpoint kind %q does not match requested kind %q",
			cp.Kind, kind.String())
	}
	return nil
}

// normalizeAsset fills FileSize from the on-disk stat when unset, and
// FileName from filepath.Base(Path) when unset. Returns a typed error if
// the file can't be stat'd or isn't a regular file.
func normalizeAsset(a UploadAsset) (UploadAsset, error) {
	info, err := os.Stat(a.Path)
	if err != nil {
		return UploadAsset{}, fmt.Errorf("stat %s: %w", a.Path, err)
	}
	if !info.Mode().IsRegular() {
		return UploadAsset{}, fmt.Errorf("%s: not a regular file", a.Path)
	}
	if a.FileSize == 0 {
		a.FileSize = info.Size()
	}
	if a.FileName == "" {
		a.FileName = filepath.Base(a.Path)
	}
	return a, nil
}

// computeFileMD5 streams the file through md5 and returns the lowercase
// hex digest. MD5 is required by Apple's sourceFileChecksum protocol; it
// is NOT used for security here, only for upload integrity verification.
func computeFileMD5(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path supplied by trusted caller
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := md5.New() //nolint:gosec // Apple's API contract requires MD5
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// reservedAssetView is the slice of the reserve / get response shape that
// Upload cares about: the assigned ID and the upload operations.
type reservedAssetView struct {
	ID         string
	Attributes uploadAssetAttributes
}

// reserveAsset POSTs the create-request body and returns the assigned ID
// plus the upload operations array.
func reserveAsset(ctx context.Context, c *Client, ep kindEndpoints, parentID string, asset UploadAsset) (reservedAssetView, error) {
	body := reserveRequest{
		Data: reserveRequestData{
			Type: ep.resourceType,
			Attributes: reserveRequestAttributes{
				FileSize: asset.FileSize,
				FileName: asset.FileName,
			},
			Relationships: map[string]reserveRequestRel{
				ep.relationship: {
					Data: reserveRequestRelRef{
						Type: ep.parentType,
						ID:   parentID,
					},
				},
			},
		},
	}
	resp, err := Post[Single[uploadAssetAttributes]](ctx, c, ep.collectionPath, nil, body)
	if err != nil {
		return reservedAssetView{}, err
	}
	if resp.Data.ID == "" {
		return reservedAssetView{}, fmt.Errorf("asc: reserve %s: empty id in response", ep.resourceType)
	}
	return reservedAssetView{ID: resp.Data.ID, Attributes: resp.Data.Attributes}, nil
}

// getReservedAsset re-fetches an existing reserved asset to get a fresh
// uploadOperations array (Apple's pre-signed URLs expire).
func getReservedAsset(ctx context.Context, c *Client, ep kindEndpoints, assetID string) (reservedAssetView, error) {
	path := ep.collectionPath + "/" + url.PathEscape(assetID)
	resp, err := Get[Single[uploadAssetAttributes]](ctx, c, path, nil)
	if err != nil {
		return reservedAssetView{}, err
	}
	if resp.Data.ID == "" {
		return reservedAssetView{}, fmt.Errorf("asc: refresh %s/%s: empty id in response", ep.resourceType, assetID)
	}
	return reservedAssetView{ID: resp.Data.ID, Attributes: resp.Data.Attributes}, nil
}

// commitAsset PATCHes the resource with uploaded=true + the MD5 checksum.
func commitAsset(ctx context.Context, c *Client, ep kindEndpoints, assetID, md5Hex string) error {
	body := commitRequest{
		Data: commitRequestData{
			Type: ep.resourceType,
			ID:   assetID,
			Attributes: commitRequestAttributes{
				Uploaded:           true,
				SourceFileChecksum: md5Hex,
			},
		},
	}
	path := ep.collectionPath + "/" + url.PathEscape(assetID)
	if _, err := Patch[Single[uploadAssetAttributes]](ctx, c, path, nil, body); err != nil {
		return err
	}
	return nil
}

// putChunks iterates the upload operations array and PUTs each chunk that
// isn't in the uploaded set. After each successful PUT, calls onSuccess
// with the chunk index so the caller can persist a checkpoint.
//
// Honours ctx.Done() between chunks. Stops on the first PUT failure.
func putChunks(
	ctx context.Context,
	path string,
	fileSize int64,
	ops []uploadOperation,
	uploaded map[int]struct{},
	onSuccess func(int) error,
) error {
	f, err := os.Open(path) //nolint:gosec // path supplied by trusted caller
	if err != nil {
		return fmt.Errorf("asc: open %s for chunked PUT: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	for idx, op := range ops {
		if _, done := uploaded[idx]; done {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := putOneChunk(ctx, f, fileSize, idx, op); err != nil {
			return err
		}
		if err := onSuccess(idx); err != nil {
			return fmt.Errorf("asc: persist checkpoint after chunk %d: %w", idx, err)
		}
	}
	return nil
}

// putOneChunk PUTs a single chunk to its pre-signed URL. NO Authorization
// header — uses http.DefaultClient, mirroring DownloadAnalyticsSegment.
func putOneChunk(ctx context.Context, f *os.File, fileSize int64, idx int, op uploadOperation) error {
	if !strings.EqualFold(op.Method, http.MethodPut) {
		return fmt.Errorf("asc: chunk %d: unexpected method %q (Apple uses PUT)", idx, op.Method)
	}
	if op.Offset < 0 || op.Length <= 0 {
		return fmt.Errorf("asc: chunk %d: invalid offset/length (offset=%d length=%d)", idx, op.Offset, op.Length)
	}
	if op.Length > uploadDownloadCapBytes {
		return fmt.Errorf("asc: chunk %d: length %d exceeds %d-byte cap", idx, op.Length, uploadDownloadCapBytes)
	}
	if op.Offset+op.Length > fileSize {
		return fmt.Errorf("asc: chunk %d: range [%d,%d) exceeds file size %d",
			idx, op.Offset, op.Offset+op.Length, fileSize)
	}

	if _, err := f.Seek(op.Offset, io.SeekStart); err != nil {
		return fmt.Errorf("asc: chunk %d: seek %d: %w", idx, op.Offset, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, op.URL, io.LimitReader(f, op.Length))
	if err != nil {
		return fmt.Errorf("asc: chunk %d: build request: %w", idx, err)
	}
	req.ContentLength = op.Length
	for _, h := range op.RequestHeaders {
		// Authorization is explicitly NOT set — Apple's CDN will reject a
		// request that pairs a pre-signed URL with a bearer token. If
		// Apple ever instructs us to set Authorization via requestHeaders
		// we forward it (rare but documented), but we never inject one
		// of our own.
		req.Header.Set(h.Name, h.Value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("asc: chunk %d: PUT failed: %w", idx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain to keep the connection reusable.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("asc: chunk %d: PUT %s returned HTTP %d",
			idx, redactSignedURL(op.URL), resp.StatusCode)
	}
	return nil
}

// redactSignedURL strips the query string of a pre-signed URL so the
// signature material doesn't leak into error logs.
func redactSignedURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[unparseable URL]"
	}
	u.RawQuery = ""
	return u.String() + "?…"
}

// sortedIndices returns the keys of a map[int]struct{} in ascending order
// so checkpoint files are deterministic across writes (idempotent JSON).
func sortedIndices(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// ---------------------------------------------------------------------------
// Checkpoint persistence (cache tier — recoverable, not state-tier durable)
// ---------------------------------------------------------------------------

// persistCheckpoint atomically writes cp to
// $XDG_CACHE_HOME/skipper/uploads/<assetId>.json. Same atomic-rename
// torture as PersistAsyncState — a Ctrl-C mid-write leaves the previous
// checkpoint untouched.
func persistCheckpoint(cp UploadCheckpoint) error {
	if cp.AssetID == "" {
		return errors.New("asc: persistCheckpoint: AssetID is required")
	}
	cp.SchemaVersion = UploadCheckpointSchemaVersion

	path, err := uploadCheckpointPath(cp.AssetID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("asc: create upload cache dir: %w", err)
	}

	buf, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("asc: marshal upload checkpoint: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("asc: create temp checkpoint file: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("asc: write temp checkpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("asc: fsync temp checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("asc: close temp checkpoint: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("asc: chmod temp checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("asc: rename checkpoint: %w", err)
	}
	committed = true
	return nil
}

// loadCheckpoint reads the checkpoint at $XDG_CACHE_HOME/skipper/uploads/
// <assetId>.json. Returns (zero, fs.ErrNotExist) when no checkpoint exists.
// Returns ErrCheckpointCorrupt for malformed / future-schema files.
func loadCheckpoint(assetID string) (UploadCheckpoint, error) {
	if assetID == "" {
		return UploadCheckpoint{}, errors.New("asc: loadCheckpoint: assetID is required")
	}
	path, err := uploadCheckpointPath(assetID)
	if err != nil {
		return UploadCheckpoint{}, err
	}
	buf, err := os.ReadFile(path) //nolint:gosec // path composed from validated components
	if err != nil {
		return UploadCheckpoint{}, err
	}

	var cp UploadCheckpoint
	if err := json.Unmarshal(buf, &cp); err != nil {
		return UploadCheckpoint{}, fmt.Errorf("%w: %s: %w", ErrCheckpointCorrupt, path, err)
	}
	if cp.SchemaVersion == 0 || cp.SchemaVersion > UploadCheckpointSchemaVersion {
		return UploadCheckpoint{}, fmt.Errorf(
			"%w: %s: schemaVersion %d is unsupported (this build understands version %d)",
			ErrCheckpointCorrupt, path, cp.SchemaVersion, UploadCheckpointSchemaVersion,
		)
	}
	return cp, nil
}

// tryLoadCheckpointForAsset scans the upload cache directory for a
// checkpoint whose FilePath matches path. Returns (cp, true, nil) on a
// match, (zero, false, nil) when no checkpoint references the file, and
// (zero, false, err) on a real I/O error.
//
// We index by file path rather than asset ID because the cmd-layer caller
// (3.1.5 screenshots upload) only knows the local path on resume — Apple's
// asset ID was issued during the failed first run and is stored only in
// the checkpoint we're trying to find.
func tryLoadCheckpointForAsset(path string) (UploadCheckpoint, bool, error) {
	root, err := uploadCacheRoot()
	if err != nil {
		return UploadCheckpoint{}, false, err
	}
	dir := filepath.Join(root, "uploads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UploadCheckpoint{}, false, nil
		}
		return UploadCheckpoint{}, false, fmt.Errorf("asc: read upload cache dir: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return UploadCheckpoint{}, false, fmt.Errorf("asc: resolve absolute path %s: %w", path, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		assetID := strings.TrimSuffix(e.Name(), ".json")
		cp, err := loadCheckpoint(assetID)
		if err != nil {
			// A corrupt checkpoint at the wrong asset ID shouldn't break
			// resume of an unrelated asset. Forward only the typed
			// corruption signal so callers can surface it.
			if errors.Is(err, ErrCheckpointCorrupt) {
				return UploadCheckpoint{}, false, err
			}
			continue
		}
		cpAbs, err := filepath.Abs(cp.FilePath)
		if err != nil {
			continue
		}
		if cpAbs == absPath {
			return cp, true, nil
		}
	}
	return UploadCheckpoint{}, false, nil
}

// removeCheckpoint deletes the checkpoint for assetID. Best-effort —
// callers ignore the error; the file will be overwritten on the next
// upload of the same asset ID.
func removeCheckpoint(assetID string) error {
	path, err := uploadCheckpointPath(assetID)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// uploadCheckpointPath composes the absolute on-disk path for a given
// asset ID. Validates the asset ID against the same path-traversal rules
// as bundle IDs (no separators, no "..").
func uploadCheckpointPath(assetID string) (string, error) {
	if err := validateAssetIDForPath(assetID); err != nil {
		return "", err
	}
	root, err := uploadCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "uploads", assetID+".json"), nil
}

// validateAssetIDForPath rejects asset IDs that would escape the
// uploads/ subdirectory. Apple's asset IDs are opaque short strings;
// anything with a path separator or ".." is hostile.
func validateAssetIDForPath(assetID string) error {
	if assetID == "" {
		return errors.New("asc: assetID is required")
	}
	if strings.ContainsAny(assetID, `/\`) {
		return fmt.Errorf("asc: assetID %q contains a path separator", assetID)
	}
	if assetID == "." || assetID == ".." || strings.Contains(assetID, "..") {
		return fmt.Errorf("asc: assetID %q contains path-traversal segments", assetID)
	}
	if strings.ContainsRune(assetID, 0) {
		return fmt.Errorf("asc: assetID contains NUL byte")
	}
	return nil
}

// uploadCacheRoot returns $XDG_CACHE_HOME/skipper, falling back to
// $HOME/.cache/skipper when XDG_CACHE_HOME is unset. Tests override via
// SKIPPER_CACHE_HOME for hermetic behavior; that env var mirrors
// SKIPPER_STATE_HOME (used by AsyncState) and is intentionally undocumented
// in user-facing surfaces — it's a test escape hatch only.
func uploadCacheRoot() (string, error) {
	if override := os.Getenv("SKIPPER_CACHE_HOME"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "skipper"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("asc: resolve home dir: %w", err)
	}
	_ = runtime.GOOS
	return filepath.Join(home, ".cache", "skipper"), nil
}
