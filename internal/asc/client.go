// Package asc is Flightline's hand-rolled ASC API client: NOT generated (see ISSUE-001); query openapi.oas.json via jq.
package asc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ul0gic/flightline/internal/auth"
)

// baseURL is hardcoded to defeat env-var hijack to a logging proxy.
const baseURL = "https://api.appstoreconnect.apple.com"

const defaultUserAgent = "flightline/dev"

// Generous: Apple writes and async-poll endpoints can take 30+ seconds.
const defaultTimeout = 60 * time.Second

// Options configures the client; KeyID, IssuerID, KeyPath are required.
type Options struct {
	KeyID      string
	IssuerID   string
	KeyPath    string // path to AuthKey_<KEY_ID>.p8 (must be mode 0600)
	UserAgent  string
	HTTPClient *http.Client
	BaseURL    string // test-only host override; empty in production
}

// Client is the concurrency-safe ASC client; each call mints a fresh JWT (Apple caps tokens at 20 min).
type Client struct {
	keyID     string
	issuerID  string
	keyPath   string
	userAgent string
	http      *http.Client
	baseURL   string
}

// New validates required Options; the .p8 is checked per-request by auth.Mint, not here.
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

// Get is the typed JSON GET (free function, not method: Go lacks generic methods).
func Get[T any](ctx context.Context, c *Client, path string, query url.Values) (T, error) {
	return doJSON[T](ctx, c, http.MethodGet, path, query, nil)
}

func Post[T any](ctx context.Context, c *Client, path string, query url.Values, body any) (T, error) {
	return doJSON[T](ctx, c, http.MethodPost, path, query, body)
}

func Patch[T any](ctx context.Context, c *Client, path string, query url.Values, body any) (T, error) {
	return doJSON[T](ctx, c, http.MethodPatch, path, query, body)
}

// Delete treats any 2xx as success (Apple returns 204) and decodes no body.
func (c *Client) Delete(ctx context.Context, path string, query url.Values) error {
	return c.deleteCommon(ctx, path, query, nil)
}

// DeleteWithBody sends a JSON body: Apple's delete-to-many relationship endpoints
// expect the linkage list in the body, not the URL.
func (c *Client) DeleteWithBody(ctx context.Context, path string, query url.Values, body any) error {
	return c.deleteCommon(ctx, path, query, body)
}

func (c *Client) deleteCommon(ctx context.Context, path string, query url.Values, body any) error {
	resp, err := c.do(ctx, http.MethodDelete, path, query, body, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// Check status before draining to preserve Apple's errors[] payload.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.errorFromResponse(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body) // drain for keep-alive reuse
	return nil
}

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
	if resp.StatusCode == http.StatusNoContent {
		return zero, nil
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("asc: decode response: %w", err)
	}
	return out, nil
}

// do mints a fresh JWT per request; caller owns resp.Body.Close(). accept="" means application/json.
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
		if isCertError(err) {
			return nil, fmt.Errorf("asc: %s %s: %w\nhint: the TLS certificate is not trusted — likely a corporate proxy or sandbox intercepting HTTPS; export SSL_CERT_FILE with the proxy's CA bundle or run outside the intercepting environment", method, redactPath(path), err)
		}
		return nil, fmt.Errorf("asc: %s %s: %w", method, redactPath(path), err)
	}
	return resp, nil
}

func isCertError(err error) bool {
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var sysRoots x509.SystemRootsError
	var hostErr x509.HostnameError
	return errors.As(err, &certErr) || errors.As(err, &unknownAuth) ||
		errors.As(err, &sysRoots) || errors.As(err, &hostErr)
}

// buildURL composes base+path+query; an absolute path is host-locked against the configured base.
func (c *Client) buildURL(path string, query url.Values) (string, error) {
	var u *url.URL
	var err error
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err = url.Parse(path)
		if err != nil {
			return "", fmt.Errorf("asc: parse URL: %w", err)
		}
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
		merged := u.Query() // explicit query overrides path-embedded params
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

// errorFromResponse parses Apple's errors[] into *APIError; captures Retry-After on 429.
func (c *Client) errorFromResponse(resp *http.Response) error {
	apiErr := &APIError{HTTPStatus: resp.StatusCode}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap a runaway error body at 1 MiB
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

// parseRetryAfter accepts integer-seconds or HTTP-date; returns 0 if absent or unparseable.
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

// redactPath strips the query string: filter values can carry personal data into logs.
func redactPath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i] + "?…"
	}
	return p
}
