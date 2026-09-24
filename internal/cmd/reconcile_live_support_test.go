//go:build integration

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
)

const (
	c3bTotalLimit   = 300
	c3bForwardLimit = 180
	c3bRequestLimit = 15 * time.Second
	c3bTotalTime    = 8 * time.Minute
)

type c3bBudgetTransport struct {
	mu            sync.Mutex
	base          http.RoundTripper
	requestCount  int
	forwardWrites int
	restoreWrites int
	restoring     bool
	versionID     string
	baseline      string
	target        string
}

func (b *c3bBudgetTransport) setIntent(id, baseline, target string) {
	b.mu.Lock()
	b.versionID = id
	b.baseline = baseline
	b.target = target
	b.mu.Unlock()
}

func (b *c3bBudgetTransport) setRestoring() {
	b.mu.Lock()
	b.restoring = true
	b.mu.Unlock()
}

func (b *c3bBudgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	limit := c3bForwardLimit
	if b.restoring {
		limit = c3bTotalLimit
	}
	if b.requestCount >= limit {
		return nil, fmt.Errorf("live reconcile request budget exhausted at %d; inspect restoration manifest", b.requestCount)
	}
	if req.URL.Scheme != "https" || req.URL.Host != "api.appstoreconnect.apple.com" {
		return nil, errors.New("live reconcile refused non-Apple endpoint")
	}
	if req.Method != http.MethodGet {
		if req.Method != http.MethodPatch || b.versionID == "" || req.URL.Path != "/v1/appStoreVersions/"+b.versionID {
			return nil, fmt.Errorf("live reconcile refused mutation %s %s", req.Method, req.URL.Path)
		}
		expected := b.target
		if b.restoring {
			expected = b.baseline
			if b.restoreWrites >= 1 {
				return nil, errors.New("live reconcile restoration mutation already attempted")
			}
		} else if b.forwardWrites >= 1 {
			return nil, errors.New("live reconcile forward mutation already attempted")
		}
		if err := c3bCheckPatchBody(req, b.versionID, expected); err != nil {
			return nil, err
		}
		if b.restoring {
			b.restoreWrites++
		} else {
			b.forwardWrites++
		}
	}
	b.requestCount++
	return b.base.RoundTrip(req)
}

func c3bCheckPatchBody(req *http.Request, versionID, expected string) error {
	if expected == "" || req.Body == nil {
		return errors.New("live reconcile refused empty copyright mutation")
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 4097))
	if err != nil || len(body) > 4096 {
		return errors.New("live reconcile refused unreadable or oversized mutation body")
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope) != 1 {
		return errors.New("live reconcile refused malformed mutation envelope")
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(envelope["data"], &data); err != nil || len(data) != 3 {
		return errors.New("live reconcile refused mutation outside one version")
	}
	var resourceType, resourceID string
	if json.Unmarshal(data["type"], &resourceType) != nil || resourceType != "appStoreVersions" ||
		json.Unmarshal(data["id"], &resourceID) != nil || resourceID != versionID {
		return errors.New("live reconcile refused mutation of another resource")
	}
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(data["attributes"], &attrs); err != nil || len(attrs) != 1 {
		return errors.New("live reconcile refused mutation outside copyright")
	}
	var copyright string
	if json.Unmarshal(attrs["copyright"], &copyright) != nil || copyright != expected {
		return errors.New("live reconcile copyright value differs from approved intent")
	}
	return nil
}

func c3bEnvironment() map[string]string {
	keys := []string{
		"FLIGHTLINE_LIVE_OPT_IN", "FLIGHTLINE_LIVE_BUNDLE_ID", "FLIGHTLINE_LIVE_VERSION",
		"FLIGHTLINE_LIVE_PLATFORM", "FLIGHTLINE_LIVE_ALLOW_BUNDLE_ID", "FLIGHTLINE_LIVE_ALLOW_VERSION",
		"FLIGHTLINE_LIVE_ALLOW_PLATFORM", "FLIGHTLINE_LIVE_KEY_ID", "FLIGHTLINE_LIVE_ISSUER_ID",
		"FLIGHTLINE_LIVE_KEY_PATH", "FLIGHTLINE_LIVE_MANIFEST_PATH",
	}
	env := make(map[string]string, len(keys))
	for _, key := range keys {
		env[key] = os.Getenv(key)
	}
	return env
}

func c3bClient(target c3bTarget, budget *c3bBudgetTransport) (*asc.Client, error) {
	return asc.New(asc.Options{
		KeyID: target.KeyID, IssuerID: target.IssuerID, KeyPath: target.KeyPath,
		HTTPClient: &http.Client{Transport: budget, Timeout: c3bRequestLimit},
	})
}

type c3bFakeRoundTripper func(*http.Request) (*http.Response, error)

func (f c3bFakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestC3B_TransportReservesRestoreWriteAndChecksBody(t *testing.T) {
	requests := 0
	b := &c3bBudgetTransport{base: c3bFakeRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})}
	b.setIntent("version-1", "original", "changed")
	patch := func(value string, extra bool) (*http.Response, error) {
		t.Helper()
		attrs := map[string]any{"copyright": value}
		if extra {
			attrs["releaseType"] = "MANUAL"
		}
		body, err := json.Marshal(map[string]any{"data": map[string]any{
			"type": "appStoreVersions", "id": "version-1", "attributes": attrs,
		}})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch, "https://api.appstoreconnect.apple.com/v1/appStoreVersions/version-1", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return b.RoundTrip(req)
	}
	refused := func(value string, extra bool, wantRequests int) {
		t.Helper()
		resp, err := patch(value, extra)
		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		if err == nil || requests != wantRequests {
			t.Fatalf("mutation reached transport or was accepted: err=%v requests=%d", err, requests)
		}
	}
	refused("changed", true, 0)
	refused("wrong", false, 0)
	resp, err := patch("changed", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	refused("changed", false, 1)
	b.setRestoring()
	resp, err = patch("original", false)
	if err != nil {
		t.Fatalf("reserved restoration PATCH refused: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	refused("original", false, 2)
}
