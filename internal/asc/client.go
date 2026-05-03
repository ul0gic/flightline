// Package asc is Skipper's hand-rolled App Store Connect API client.
//
// The client wraps stdlib net/http with auth injection (ES256 JWT, IEEE P1363
// raw signature, fresh per request), Apple's errors[] envelope decoding, and
// generic JSON:API envelope handling (Resource[T], Single[T], Collection[T]).
//
// Apple's openapi.oas.json (committed at the project root) is the authoritative
// reference for endpoint shapes; query it via jq during command authoring. This
// package is NOT generated — see .project/issues/closed/ISSUE-001 for why.
package asc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ul0gic/skipper/internal/auth"
)

// baseURL is the only host this client talks to. Hardcoded to defeat
// accidental env-var hijack to a logging proxy or a test rig in production.
const baseURL = "https://api.appstoreconnect.apple.com"

// defaultUserAgent identifies skipper to Apple. Bumped per release.
const defaultUserAgent = "skipper/dev"

// defaultTimeout is generous because Apple's writes occasionally take 30+
// seconds during peak hours and async-poll endpoints can be slow.
const defaultTimeout = 60 * time.Second

// Options configures the client.
//
// KeyID, IssuerID, KeyPath are required. HTTPClient and UserAgent are optional
// and have sensible defaults.
type Options struct {
	// KeyID is the 10-character App Store Connect API key ID.
	KeyID string
	// IssuerID is the issuer UUID from ASC → Users and Access → Integrations.
	IssuerID string
	// KeyPath is the absolute path to AuthKey_<KEY_ID>.p8 (mode 0600).
	KeyPath string
	// UserAgent is sent on every request. Defaults to "skipper/dev".
	UserAgent string
	// HTTPClient overrides the default 60s-timeout client. Tests inject this
	// to point at httptest.NewServer.
	HTTPClient *http.Client
	// BaseURL overrides the production ASC base URL. Test-only — leave empty
	// in production callers; defaults to the hardcoded
	// "https://api.appstoreconnect.apple.com" when empty. Cross-package tests
	// in internal/cmd/ use this to point the client at an httptest.Server.
	BaseURL string
}

// Client is the concurrency-safe ASC API client.
//
// Construct one per process via New(). Each call mints a fresh JWT (Apple
// caps tokens at 20 minutes; caching across one request buys nothing).
type Client struct {
	keyID     string
	issuerID  string
	keyPath   string
	userAgent string
	http      *http.Client
	// baseURL is overridable for tests via the (unexported) WithBaseURL helper.
	baseURL string
}

// New constructs a Client. Returns an error if any required field is empty.
//
// The returned client does NOT validate the .p8 path eagerly — that happens
// per-request inside auth.Mint, which surfaces ErrPermsTooWide / ErrKeyNotFound
// at first use with a chmod hint.
func New(opts Options) (*Client, error) {
	if opts.KeyID == "" {
		return nil, errors.New("asc: Options.KeyID is required")
	}
	if opts.IssuerID == "" {
		return nil, errors.New("asc: Options.IssuerID is required")
	}
	if opts.KeyPath == "" {
		return nil, errors.New("asc: Options.KeyPath is required")
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	base := opts.BaseURL
	if base == "" {
		base = baseURL
	}

	return &Client{
		keyID:     opts.KeyID,
		issuerID:  opts.IssuerID,
		keyPath:   opts.KeyPath,
		userAgent: ua,
		http:      httpClient,
		baseURL:   base,
	}, nil
}

// Get is the typed JSON GET against the ASC API.
//
// path is rooted (e.g. "/v1/apps") and may already contain a query string,
// or query may be supplied separately. T is the response envelope type:
// usually Collection[Attrs] for list endpoints or Single[Attrs] for get-by-id.
//
// Generic methods aren't allowed in Go, so Get is a package-level function
// taking *Client. Same shape as the legacy Pages helper.
func Get[T any](ctx context.Context, c *Client, path string, query url.Values) (T, error) {
	return doJSON[T](ctx, c, http.MethodGet, path, query, nil)
}

// Post is the typed JSON POST. body is marshaled as application/json.
func Post[T any](ctx context.Context, c *Client, path string, query url.Values, body any) (T, error) {
	return doJSON[T](ctx, c, http.MethodPost, path, query, body)
}

// Patch is the typed JSON PATCH. body is marshaled as application/json.
func Patch[T any](ctx context.Context, c *Client, path string, query url.Values, body any) (T, error) {
	return doJSON[T](ctx, c, http.MethodPatch, path, query, body)
}

// Delete issues a DELETE. Apple typically returns 204 No Content on success;
// any 2xx is treated as success and no body is decoded.
func (c *Client) Delete(ctx context.Context, path string, query url.Values) error {
	return c.deleteCommon(ctx, path, query, nil)
}

// DeleteWithBody issues a DELETE with a JSON request body. Apple's
// "delete to-many relationship" endpoints require this shape — e.g.
// /v1/betaGroups/{id}/relationships/betaTesters expects the linkages list
// in the body, not the URL.
//
// Behavior matches Delete on the response side: 2xx success, body
// drained and discarded, 4xx/5xx surfaced as a typed *APIError.
func (c *Client) DeleteWithBody(ctx context.Context, path string, query url.Values, body any) error {
	return c.deleteCommon(ctx, path, query, body)
}

// deleteCommon shares the body-or-not DELETE flow.
//
// On non-2xx the response body is read by errorFromResponse to populate
// Apple's errors[] envelope; on 2xx the body is drained and discarded for
// keep-alive reuse. The order matters — draining before status check
// would silently drop the errors[] payload (closed QA-007).
func (c *Client) deleteCommon(ctx context.Context, path string, query url.Values, body any) error {
	resp, err := c.do(ctx, http.MethodDelete, path, query, body, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.errorFromResponse(resp)
	}
	// 2xx: drain body for keep-alive reuse.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// doJSON shared the request → status-check → JSON-decode flow used by every
// typed verb helper.
func doJSON[T any](ctx context.Context, c *Client, method, path string, query url.Values, body any) (T, error) {
	var zero T
	resp, err := c.do(ctx, method, path, query, body, "")
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, c.errorFromResponse(resp)
	}

	// 204 No Content with a typed return is unusual but cope: return zero.
	if resp.StatusCode == http.StatusNoContent {
		return zero, nil
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("asc: decode response: %w", err)
	}
	return out, nil
}

// do is the inner request executor. It builds the request, mints a fresh JWT,
// sets headers, and returns the *http.Response without touching the body.
// Caller owns Close.
//
// accept lets callers override the Accept header. Pass "" for the default
// "application/json"; pass "application/a-gzip" for endpoints that stream
// gzipped reports (sales, finance) — Apple returns 406 for those when Accept
// is JSON.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, accept string) (*http.Response, error) {
	target, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("asc: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("asc: build request: %w", err)
	}

	token, err := auth.Mint(c.keyID, c.issuerID, c.keyPath)
	if err != nil {
		return nil, fmt.Errorf("asc: mint JWT: %w", err)
	}
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asc: %s %s: %w", method, redactPath(path), err)
	}
	return resp, nil
}

// buildURL composes the absolute URL from base + path + query. path may be
// either an absolute URL (paging follow-up) — in which case base is ignored
// and host is checked — or a rooted path like "/v1/apps".
func (c *Client) buildURL(path string, query url.Values) (string, error) {
	var u *url.URL
	var err error
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err = url.Parse(path)
		if err != nil {
			return "", fmt.Errorf("asc: parse URL: %w", err)
		}
		// Lock to the configured host to defeat redirect-style hijacks.
		base, err := url.Parse(c.baseURL)
		if err != nil {
			return "", fmt.Errorf("asc: parse base URL: %w", err)
		}
		if u.Host != base.Host {
			return "", fmt.Errorf("asc: refusing absolute URL for foreign host %q", u.Host)
		}
	} else {
		u, err = url.Parse(c.baseURL + path)
		if err != nil {
			return "", fmt.Errorf("asc: parse URL: %w", err)
		}
	}
	if len(query) > 0 {
		// Merge: explicit query overrides anything embedded in path.
		merged := u.Query()
		for k, vs := range query {
			merged.Del(k)
			for _, v := range vs {
				merged.Add(k, v)
			}
		}
		u.RawQuery = merged.Encode()
	}
	return u.String(), nil
}

// errorFromResponse parses Apple's errors[] payload (if any) and returns a
// typed *APIError that supports errors.Is for ErrUnauthorized / ErrForbidden.
// On 429 it also captures the Retry-After header (closed QA-008).
func (c *Client) errorFromResponse(resp *http.Response) error {
	apiErr := &APIError{HTTPStatus: resp.StatusCode}
	// LimitReader caps a runaway error body at 1 MiB.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if len(body) > 0 {
		var envelope struct {
			Errors []ErrorItem `json:"errors"`
		}
		if err := json.Unmarshal(body, &envelope); err == nil {
			apiErr.Errors = envelope.Errors
		}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		apiErr.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	}
	return apiErr
}

// parseRetryAfter handles both Retry-After formats: integer seconds and
// HTTP-date. Returns 0 if header is missing or unparseable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// redactPath strips query strings from a path before it reaches an error
// message — query strings sometimes carry filter values that include personal
// data, and we never want them in log lines.
func redactPath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i] + "?…"
	}
	return p
}
