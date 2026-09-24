package asc

import (
	"context"
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

func TestBUG075_CrossParentResumeReservesNewAsset(t *testing.T) {
	root := withUploadCacheRoot(t)
	path := writeUploadPayload(t, "shared.png")
	seedBUG075Checkpoint(t, root, UploadCheckpoint{
		SchemaVersion: UploadCheckpointSchemaVersion,
		AssetID:       "asset-a",
		Kind:          AssetKindAppScreenshot.String(),
		ResourceType:  "appScreenshots",
		ParentType:    "appScreenshotSets",
		ParentID:      "set-a",
		FilePath:      path,
		FileSize:      int64(len(uploadTestPayload)),
		Md5Hex:        expectedUploadMD5,
	})

	var reserveB, committedA, committedB atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appScreenshots":
			var body struct {
				Data struct {
					Relationships map[string]struct {
						Data struct {
							ID string `json:"id"`
						} `json:"data"`
					} `json:"relationships"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode reserve: %v", err)
			}
			if got := body.Data.Relationships["appScreenshotSet"].Data.ID; got != "set-b" {
				t.Errorf("reserve parent = %q, want set-b", got)
			}
			reserveB.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"type":"appScreenshots","id":"asset-b","attributes":{"uploadOperations":[{"method":"PUT","url":"` + srv.URL + `/chunk-b","length":16,"offset":0}]}}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/chunk-b":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appScreenshots/asset-a":
			t.Fatal("cross-parent resume read asset-a")
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshots/asset-a":
			committedA.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appScreenshots/asset-b":
			committedB.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"type":"appScreenshots","id":"asset-b"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	result, err := fixtureClient(t, srv).Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "set-b",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.ID != "asset-b" || reserveB.Load() != 1 || committedA.Load() != 0 || committedB.Load() != 1 {
		t.Fatalf("result=%+v reserveB=%d committedA=%d committedB=%d", result, reserveB.Load(), committedA.Load(), committedB.Load())
	}
	if _, err := os.Stat(filepath.Join(root, "uploads", "asset-a.json")); err != nil {
		t.Fatalf("cross-parent checkpoint was modified: %v", err)
	}
}

func TestBUG075_LegacyCheckpointFailsBeforeMutation(t *testing.T) {
	root := withUploadCacheRoot(t)
	path := writeUploadPayload(t, "legacy.png")
	seedBUG075Checkpoint(t, root, UploadCheckpoint{
		SchemaVersion: 1,
		AssetID:       "legacy-asset",
		Kind:          AssetKindAppScreenshot.String(),
		FilePath:      path,
		FileSize:      int64(len(uploadTestPayload)),
		Md5Hex:        expectedUploadMD5,
	})

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "must not mutate", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := fixtureClient(t, srv).Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "set-a",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if !errors.Is(err, ErrCheckpointCorrupt) {
		t.Fatalf("Upload error = %v, want ErrCheckpointCorrupt", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("legacy checkpoint made %d HTTP calls", calls.Load())
	}
}

func TestBUG075_AmbiguousSameTargetCheckpointsFailBeforeMutation(t *testing.T) {
	root := withUploadCacheRoot(t)
	path := writeUploadPayload(t, "ambiguous.png")
	for _, id := range []string{"asset-a", "asset-b"} {
		seedBUG075Checkpoint(t, root, UploadCheckpoint{
			SchemaVersion: UploadCheckpointSchemaVersion,
			AssetID:       id,
			Kind:          AssetKindAppScreenshot.String(),
			ResourceType:  "appScreenshots",
			ParentType:    "appScreenshotSets",
			ParentID:      "set-a",
			FilePath:      path,
			FileSize:      int64(len(uploadTestPayload)),
			Md5Hex:        expectedUploadMD5,
		})
	}

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "must not mutate", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := fixtureClient(t, srv).Upload(context.Background(), UploadOptions{
		Kind:                 AssetKindAppScreenshot,
		ParentID:             "set-a",
		Asset:                UploadAsset{Path: path},
		ResumeFromCheckpoint: true,
	})
	if err == nil || !strings.Contains(err.Error(), "multiple checkpoints") {
		t.Fatalf("Upload error = %v, want ambiguous checkpoint error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("ambiguous checkpoints made %d HTTP calls", calls.Load())
	}
}

func seedBUG075Checkpoint(t *testing.T, root string, cp UploadCheckpoint) {
	t.Helper()
	dir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir checkpoint dir: %v", err)
	}
	encoded, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, cp.AssetID+".json"), encoded, 0o600); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
}
