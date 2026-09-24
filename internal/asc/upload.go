// Chunk PUTs carry NO Authorization header: Apple's CDN signs the URL; any extra header flips the signature.
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

// UploadCheckpointSchemaVersion is the on-disk JSON schema version for upload checkpoints.
// Bump on shape changes; the loader rejects every other version (no implicit migration).
const UploadCheckpointSchemaVersion = 2

// Bounds a single chunk read into memory against a runaway operation.length.
const uploadDownloadCapBytes = 64 << 20

// AssetKind selects which Apple endpoint to reserve against.
type AssetKind int

// iota+1 so the zero value is invalid and forces callers to pick a kind.
const (
	AssetKindAppScreenshot AssetKind = iota + 1
	AssetKindIAPReviewScreenshot
	AssetKindAppPreview
	AssetKindReviewAttachment
	AssetKindIAPPromotionalImage
	AssetKindAppEventCardScreenshot
	AssetKindAppEventDetailsScreenshot
	AssetKindAppEventCardVideo
	AssetKindAppEventDetailsVideo
)

// String is the canonical name in checkpoint files; renames break checkpoint compat.
func (k AssetKind) String() string {
	switch k {
	case AssetKindAppScreenshot:
		return "appScreenshot"
	case AssetKindIAPReviewScreenshot:
		return "iapReviewScreenshot"
	case AssetKindReviewAttachment:
		return "appStoreReviewAttachment"
	case AssetKindIAPPromotionalImage:
		return "inAppPurchaseImage"
	case AssetKindAppPreview:
		return "appPreview"
	case AssetKindAppEventCardScreenshot:
		return "appEventCardScreenshot"
	case AssetKindAppEventDetailsScreenshot:
		return "appEventDetailsScreenshot"
	case AssetKindAppEventCardVideo:
		return "appEventCardVideo"
	case AssetKindAppEventDetailsVideo:
		return "appEventDetailsVideo"
	default:
		return fmt.Sprintf("AssetKind(%d)", int(k))
	}
}

type kindEndpoints struct {
	collectionPath string
	resourceType   string
	relationship   string
	parentType     string
	eventAssetType string
	omitChecksum   bool
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
	case AssetKindReviewAttachment:
		return kindEndpoints{collectionPath: "/v1/appStoreReviewAttachments", resourceType: "appStoreReviewAttachments", relationship: "appStoreReviewDetail", parentType: "appStoreReviewDetails"}, nil
	case AssetKindIAPPromotionalImage:
		return kindEndpoints{collectionPath: "/v1/inAppPurchaseImages", resourceType: "inAppPurchaseImages", relationship: "inAppPurchase", parentType: "inAppPurchases"}, nil
	case AssetKindAppEventCardScreenshot, AssetKindAppEventDetailsScreenshot, AssetKindAppEventCardVideo, AssetKindAppEventDetailsVideo:
		resource := "appEventScreenshots"
		if k == AssetKindAppEventCardVideo || k == AssetKindAppEventDetailsVideo {
			resource = "appEventVideoClips"
		}
		assetType := "EVENT_CARD"
		if k == AssetKindAppEventDetailsScreenshot || k == AssetKindAppEventDetailsVideo {
			assetType = "EVENT_DETAILS_PAGE"
		}
		return kindEndpoints{collectionPath: "/v1/" + resource, resourceType: resource, relationship: "appEventLocalization", parentType: "appEventLocalizations", eventAssetType: assetType, omitChecksum: true}, nil
	default:
		return kindEndpoints{}, fmt.Errorf("asc: unknown AssetKind %d (unsupported upload resource)", int(k))
	}
}

// UploadAsset describes one file to upload.
// FileSize defaults to on-disk size when zero; FileName defaults to filepath.Base(Path).
type UploadAsset struct {
	Path     string
	FileSize int64
	FileName string
}

// UploadOptions configures one upload session.
// ResumeFromCheckpoint=true reads the on-disk checkpoint and skips already-uploaded chunks.
type UploadOptions struct {
	Kind                 AssetKind
	ParentID             string
	Asset                UploadAsset
	ResumeFromCheckpoint bool
}

// UploadResult names the created Apple resource after a successful commit.
// Checksum is the local hex-encoded MD5; most asset types also send it as
// sourceFileChecksum. Event assets omit that unsupported field on commit.
type UploadResult struct {
	ID       string
	Type     string
	Checksum string
}

// UploadCheckpoint is the on-disk shape of an in-progress upload (stable JSON contract).
// FilePath is absolute. FilePath/FileSize/Md5Hex pin the local source, while
// Kind/ResourceType/ParentType/ParentID bind the checkpoint to one ASC target.
// A mismatched file returns ErrCheckpointMismatch instead of re-uploading wrong bytes.
type UploadCheckpoint struct {
	SchemaVersion  int       `json:"schemaVersion"`
	AssetID        string    `json:"assetId"`
	Kind           string    `json:"kind"`
	ResourceType   string    `json:"resourceType"`
	ParentType     string    `json:"parentType"`
	ParentID       string    `json:"parentId"`
	FilePath       string    `json:"filePath"`
	FileSize       int64     `json:"fileSize"`
	Md5Hex         string    `json:"md5Hex"`
	UploadedChunks []int     `json:"uploadedChunks"`
	StartedAt      time.Time `json:"startedAt"`
	LastUpdate     time.Time `json:"lastUpdate"`
}

// ErrCheckpointMismatch is returned by Upload when ResumeFromCheckpoint=true and the local file's MD5 changed.
// Loud by design: silently re-uploading mutated bytes to a half-written Apple asset is worse than refusing to continue.
var ErrCheckpointMismatch = errors.New("asc: upload checkpoint does not match local file (file changed since checkpoint)")

// ErrCheckpointCorrupt is returned when an upload checkpoint exists but cannot be decoded; same semantics as ErrStateCorrupt.
var ErrCheckpointCorrupt = errors.New("asc: upload checkpoint file is corrupt or unreadable")

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

// uploadAssetAttributes is the upload-specific subset shared across all three asset kinds;
// other attribute structs omit uploadOperations because their consumers don't need it.
type uploadAssetAttributes struct {
	FileSize         int64             `json:"fileSize,omitempty"`
	FileName         string            `json:"fileName,omitempty"`
	UploadOperations []uploadOperation `json:"uploadOperations,omitempty"`
}

// reserveRequest is the JSON:API create body; all three asset kinds share this shape.
type reserveRequest struct {
	Data reserveRequestData `json:"data"`
}

type reserveRequestData struct {
	Type          string                       `json:"type"`
	Attributes    reserveRequestAttributes     `json:"attributes"`
	Relationships map[string]reserveRequestRel `json:"relationships"`
}

type reserveRequestAttributes struct {
	FileSize          int64  `json:"fileSize"`
	FileName          string `json:"fileName"`
	AppEventAssetType string `json:"appEventAssetType,omitempty"`
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
	SourceFileChecksum string `json:"sourceFileChecksum,omitempty"`
}

// Upload runs the reserve → PUT chunks → commit lifecycle for one asset.
// A failure between PUTs preserves the checkpoint; no retry in v1, callers re-run with the resume flag.
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

	if err := putChunks(ctx, asset.Path, asset.FileSize, plan.operations, plan.uploaded, func(idx int) error {
		plan.uploaded[idx] = struct{}{}
		return persistCheckpoint(UploadCheckpoint{
			AssetID:        plan.assetID,
			Kind:           opts.Kind.String(),
			ResourceType:   endpoints.resourceType,
			ParentType:     endpoints.parentType,
			ParentID:       opts.ParentID,
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

	if err := commitAsset(ctx, c, endpoints, plan.assetID, md5Hex); err != nil {
		return UploadResult{}, err
	}

	// Best-effort cleanup; a stale checkpoint is overwritten on the next upload of this asset.
	_ = removeCheckpoint(plan.assetID)

	return UploadResult{
		ID:       plan.assetID,
		Type:     endpoints.resourceType,
		Checksum: md5Hex,
	}, nil
}

// prepareUpload validates the options, normalizes the asset descriptor, and hashes the local file.
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

type uploadPlan struct {
	assetID    string
	operations []uploadOperation
	uploaded   map[int]struct{}
	startedAt  time.Time
}

// resolveUploadPlan resumes an existing checkpoint or reserves a fresh asset.
// On resume it re-fetches the resource to refresh Apple's pre-signed chunk URLs, which expire.
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
		cp, found, err := tryLoadCheckpointForAsset(asset.Path, opts.Kind, endpoints, opts.ParentID)
		if err != nil {
			return uploadPlan{}, err
		}
		if found {
			if err := validateCheckpointForReuse(cp, opts.Kind, endpoints, opts.ParentID, asset, md5Hex); err != nil {
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

// validateCheckpointForReuse asserts a loaded checkpoint matches the caller's ASC target and local file.
func validateCheckpointForReuse(cp UploadCheckpoint, kind AssetKind, endpoints kindEndpoints, parentID string, asset UploadAsset, md5Hex string) error {
	if cp.Md5Hex != md5Hex {
		return fmt.Errorf("%w: %s (checkpoint md5 %s, file md5 %s)",
			ErrCheckpointMismatch, asset.Path, cp.Md5Hex, md5Hex)
	}
	if cp.Kind != kind.String() {
		return fmt.Errorf("asc: Upload: checkpoint kind %q does not match requested kind %q",
			cp.Kind, kind.String())
	}
	if cp.ResourceType != endpoints.resourceType || cp.ParentType != endpoints.parentType || cp.ParentID != parentID {
		return fmt.Errorf("asc: Upload: checkpoint target %s/%s/%s does not match requested %s/%s/%s",
			cp.ResourceType, cp.ParentType, cp.ParentID, endpoints.resourceType, endpoints.parentType, parentID)
	}
	if cp.FilePath != asset.Path || cp.FileSize != asset.FileSize {
		return fmt.Errorf("%w: checkpoint source %s/%d does not match requested %s/%d",
			ErrCheckpointMismatch, cp.FilePath, cp.FileSize, asset.Path, asset.FileSize)
	}
	return nil
}

// normalizeAsset fills FileSize from the on-disk stat and FileName from filepath.Base when unset.
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
	absPath, err := filepath.Abs(a.Path)
	if err != nil {
		return UploadAsset{}, fmt.Errorf("resolve absolute path %s: %w", a.Path, err)
	}
	a.Path = absPath
	return a, nil
}

// computeFileMD5 returns the lowercase hex digest. MD5 is Apple's sourceFileChecksum
// protocol requirement, not a security choice: integrity verification only.
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

type reservedAssetView struct {
	ID         string
	Attributes uploadAssetAttributes
}

// reserveAsset POSTs the create body and returns the assigned ID plus upload operations.
func reserveAsset(ctx context.Context, c *Client, ep kindEndpoints, parentID string, asset UploadAsset) (reservedAssetView, error) {
	body := reserveRequest{
		Data: reserveRequestData{
			Type: ep.resourceType,
			Attributes: reserveRequestAttributes{
				FileSize:          asset.FileSize,
				FileName:          asset.FileName,
				AppEventAssetType: ep.eventAssetType,
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

// getReservedAsset re-fetches a reserved asset for a fresh uploadOperations array;
// Apple's pre-signed URLs expire.
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

// commitAsset PATCHes uploaded=true and the checksum where supported.
func commitAsset(ctx context.Context, c *Client, ep kindEndpoints, assetID, md5Hex string) error {
	if ep.omitChecksum {
		md5Hex = ""
	}
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

// putChunks PUTs each chunk not in the uploaded set, calling onSuccess after each so the
// caller can checkpoint. Honours ctx.Done() between chunks; stops on the first PUT failure.
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

// putOneChunk PUTs a single chunk to its pre-signed URL via http.DefaultClient (no bearer token).
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
		// Only Apple-supplied headers; never inject Authorization: a bearer token on a
		// pre-signed URL makes the CDN reject the request.
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

// redactSignedURL drops the query string so pre-signed signature material stays out of error logs.
func redactSignedURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[unparseable URL]"
	}
	u.RawQuery = ""
	return u.String() + "?…"
}

// sortedIndices returns keys in ascending order so checkpoint JSON is deterministic across writes.
func sortedIndices(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// persistCheckpoint atomically writes cp to $XDG_CACHE_HOME/flightline/uploads/<assetId>.json.
// Atomic rename: a Ctrl-C mid-write leaves the previous checkpoint untouched.
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

// loadCheckpoint returns (zero, fs.ErrNotExist) when none exists and
// ErrCheckpointCorrupt for malformed or schema-incompatible files.
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
	if cp.SchemaVersion != UploadCheckpointSchemaVersion {
		return UploadCheckpoint{}, fmt.Errorf(
			"%w: %s: schemaVersion %d is unsupported (this build understands version %d)",
			ErrCheckpointCorrupt, path, cp.SchemaVersion, UploadCheckpointSchemaVersion,
		)
	}
	return cp, nil
}

type uploadCheckpointTarget struct {
	kind         string
	resourceType string
	parentType   string
	parentID     string
}

func newUploadCheckpointTarget(kind AssetKind, endpoints kindEndpoints, parentID string) uploadCheckpointTarget {
	return uploadCheckpointTarget{kind: kind.String(), resourceType: endpoints.resourceType, parentType: endpoints.parentType, parentID: parentID}
}

func (target uploadCheckpointTarget) matches(cp UploadCheckpoint) bool {
	return cp.Kind == target.kind && cp.ResourceType == target.resourceType && cp.ParentType == target.parentType && cp.ParentID == target.parentID
}

func checkpointMatchesPath(cp UploadCheckpoint, path string) bool {
	cpPath, err := filepath.Abs(cp.FilePath)
	return err == nil && cpPath == path
}

func loadCheckpointEntry(entry os.DirEntry) (UploadCheckpoint, bool, error) {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
		return UploadCheckpoint{}, false, nil
	}
	cp, err := loadCheckpoint(strings.TrimSuffix(entry.Name(), ".json"))
	if err == nil {
		return cp, true, nil
	}
	if errors.Is(err, ErrCheckpointCorrupt) {
		return UploadCheckpoint{}, false, err
	}
	return UploadCheckpoint{}, false, nil
}

// tryLoadCheckpointForAsset finds one checkpoint for the exact local source and ASC target.
// It never resumes an asset associated with a different parent; mismatched-target checkpoints
// are left intact and the caller reserves a new asset. Multiple exact matches are ambiguous.
func tryLoadCheckpointForAsset(path string, kind AssetKind, endpoints kindEndpoints, parentID string) (UploadCheckpoint, bool, error) {
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

	target := newUploadCheckpointTarget(kind, endpoints, parentID)
	return selectUploadCheckpoint(entries, absPath, target)
}

func selectUploadCheckpoint(entries []os.DirEntry, absPath string, target uploadCheckpointTarget) (UploadCheckpoint, bool, error) {
	var match *UploadCheckpoint
	for _, entry := range entries {
		cp, found, err := loadCheckpointEntry(entry)
		if err != nil {
			return UploadCheckpoint{}, false, err
		}
		if !found || !checkpointMatchesPath(cp, absPath) {
			continue
		}
		if cp.Kind != target.kind {
			return cp, true, nil
		}
		if !target.matches(cp) {
			continue
		}
		if match != nil {
			return UploadCheckpoint{}, false, fmt.Errorf("asc: Upload: multiple checkpoints match %s for %s/%s/%s", absPath, target.resourceType, target.parentType, target.parentID)
		}
		selected := cp
		match = &selected
	}
	if match == nil {
		return UploadCheckpoint{}, false, nil
	}
	return *match, true, nil
}

// removeCheckpoint deletes the checkpoint for assetID.
func removeCheckpoint(assetID string) error {
	path, err := uploadCheckpointPath(assetID)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// uploadCheckpointPath composes the on-disk path, rejecting path-traversal in the asset ID.
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

// validateAssetIDForPath rejects asset IDs that would escape the uploads/ subdirectory.
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
		return errors.New("asc: assetID contains NUL byte")
	}
	return nil
}

// uploadCacheRoot returns $XDG_CACHE_HOME/flightline, falling back to $HOME/.cache/flightline.
// FLIGHTLINE_CACHE_HOME overrides it: an undocumented test escape hatch only.
func uploadCacheRoot() (string, error) {
	if override := os.Getenv("FLIGHTLINE_CACHE_HOME"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "flightline"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("asc: resolve home dir: %w", err)
	}
	_ = runtime.GOOS
	return filepath.Join(home, ".cache", "flightline"), nil
}
