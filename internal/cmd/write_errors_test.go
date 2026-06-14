package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

// errorFixtureServer serves one static error fixture for any path, counting
// requests so tests can assert "no retry."
type errorFixtureServer struct {
	srv             *httptest.Server
	count           atomic.Int32
	bodyFixture     string
	statusCode      int
	retryAfterValue string
}

func newErrorFixtureServer(t *testing.T, fixture string, status int, retryAfter string) *errorFixtureServer {
	t.Helper()
	e := &errorFixtureServer{
		bodyFixture:     fixture,
		statusCode:      status,
		retryAfterValue: retryAfter,
	}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		e.count.Add(1)
		body, err := readGoldenFixture(e.bodyFixture)
		if err != nil {
			t.Errorf("load fixture %s: %v", e.bodyFixture, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if e.retryAfterValue != "" {
			w.Header().Set("Retry-After", e.retryAfterValue)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(e.statusCode)
		_, _ = w.Write(body)
	}))
	t.Cleanup(e.srv.Close)
	return e
}

func errorFixtureClient(t *testing.T, e *errorFixtureServer) *asc.Client {
	t.Helper()
	return fixtureASCClient(t, e.srv)
}

// TestWriteError_422_SurfacesValidationDetails asserts a 422 surfaces each
// error[] item's title+code+detail and leaks no credential-shaped tokens.
func TestWriteError_422_SurfacesValidationDetails(t *testing.T) {
	e := newErrorFixtureServer(t, "error_422_validation", http.StatusUnprocessableEntity, "")
	c := errorFixtureClient(t, e)

	body := map[string]any{"data": map[string]any{
		"type": "appStoreVersions",
		"id":   "8000000001",
		"attributes": map[string]any{
			"versionString": "99.x.0",
		},
	}}
	_, err := asc.Patch[asc.Single[map[string]any]](
		context.Background(), c, "/v1/appStoreVersions/8000000001", nil, body,
	)
	if err == nil {
		t.Fatal("PATCH returned nil error; want 422 *APIError")
	}

	// 1) typed error
	var apiErr *asc.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *asc.APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusUnprocessableEntity {
		t.Errorf("HTTPStatus = %d, want 422", apiErr.HTTPStatus)
	}
	if len(apiErr.Errors) < 2 {
		t.Errorf("Errors len = %d, want >= 2 (fixture has 2 items)", len(apiErr.Errors))
	}

	// 2) user-facing message includes the first item's title + code + detail.
	msg := err.Error()
	for _, want := range []string{
		"422",
		"ENTITY_ERROR.ATTRIBUTE.INVALID",
		"An attribute value provided is invalid",
		"versionString",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("err.Error() missing %q: %q", want, msg)
		}
	}

	// 3) user-facing message documents the count of remaining items so the
	// user sees that more than one error landed.
	if !strings.Contains(msg, "more error") {
		t.Errorf("err.Error() should summarize extra error[] items: %q", msg)
	}

	// 4) no credential-shaped material in the user-facing message. Even
	// though the fixture doesn't contain any, the redactor must always run.
	assertNoCredentialLeakage(t, msg)

	// 5) exactly one request: no retry on 422.
	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (no retry on 422)", got)
	}
}

// TestWriteError_401_FastFailsNoRetry asserts 401 maps to ErrUnauthorized and
// does not retry.
func TestWriteError_401_FastFailsNoRetry(t *testing.T) {
	e := newErrorFixtureServer(t, "error_401", http.StatusUnauthorized, "")
	c := errorFixtureClient(t, e)

	body := map[string]any{"data": map[string]any{"type": "apps", "id": "1"}}
	_, err := asc.Patch[asc.Single[map[string]any]](
		context.Background(), c, "/v1/apps/1", nil, body,
	)
	if err == nil {
		t.Fatal("PATCH returned nil error; want 401")
	}
	if !errors.Is(err, asc.ErrUnauthorized) {
		t.Errorf("errors.Is(err, ErrUnauthorized) = false; err = %v", err)
	}

	msg := err.Error()
	for _, want := range []string{"401", "NOT_AUTHORIZED"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err.Error() missing %q: %q", want, msg)
		}
	}

	// The sentinel carries the actionable hint; confirm it survives the chain.
	if got := asc.ErrUnauthorized.Error(); !strings.Contains(got, "key ID") || !strings.Contains(got, ".p8") {
		t.Errorf("ErrUnauthorized.Error() lost its actionable hint: %q", got)
	}

	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (401 fast-fail)", got)
	}
	assertNoCredentialLeakage(t, msg)
}

// TestWriteError_403_FastFailsWithPermissionHint asserts 403 maps to
// ErrForbidden with a permission-specific hint distinct from 401.
func TestWriteError_403_FastFailsWithPermissionHint(t *testing.T) {
	e := newErrorFixtureServer(t, "error_403", http.StatusForbidden, "")
	c := errorFixtureClient(t, e)

	body := map[string]any{"data": map[string]any{"type": "apps", "id": "1"}}
	_, err := asc.Patch[asc.Single[map[string]any]](
		context.Background(), c, "/v1/apps/1", nil, body,
	)
	if err == nil {
		t.Fatal("PATCH returned nil error; want 403")
	}
	if !errors.Is(err, asc.ErrForbidden) {
		t.Errorf("errors.Is(err, ErrForbidden) = false; err = %v", err)
	}
	if got := asc.ErrForbidden.Error(); !strings.Contains(got, "lacks permission") {
		t.Errorf("ErrForbidden.Error() lost its actionable hint: %q", got)
	}
	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (403 fast-fail)", got)
	}
}

// TestWriteError_429_SurfacesBodyNoRetryRetryAfterDropped locks current
// behaviour: 429 surfaces the body, does not retry, and drops Retry-After.
func TestWriteError_429_SurfacesBodyNoRetryRetryAfterDropped(t *testing.T) {
	e := newErrorFixtureServer(t, "error_429_rate_limit", http.StatusTooManyRequests, "120")
	c := errorFixtureClient(t, e)

	body := map[string]any{"data": map[string]any{"type": "apps", "id": "1"}}
	_, err := asc.Patch[asc.Single[map[string]any]](
		context.Background(), c, "/v1/apps/1", nil, body,
	)
	if err == nil {
		t.Fatal("PATCH returned nil error; want 429")
	}

	var apiErr *asc.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *asc.APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusTooManyRequests {
		t.Errorf("HTTPStatus = %d, want 429", apiErr.HTTPStatus)
	}

	msg := err.Error()
	for _, want := range []string{"429", "RATE_LIMIT_EXCEEDED", "throttled"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err.Error() missing %q: %q", want, msg)
		}
	}

	// No automatic retry: the caller decides when to retry the window.
	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (429 must not auto-retry)", got)
	}

	// Retry-After is dropped from the error (QA-008); the log flips when fixed.
	if strings.Contains(msg, "120") {
		t.Logf("Retry-After header is now surfaced (msg=%q). QA-008 may be resolvable.", msg)
	}

	assertNoCredentialLeakage(t, msg)
}

// TestWriteError_5xx_SurfacesBodyNoIndefiniteRetry asserts 5xx surfaces the
// body and does not retry (no retry policy in v1).
func TestWriteError_5xx_SurfacesBodyNoIndefiniteRetry(t *testing.T) {
	e := newErrorFixtureServer(t, "error_500", http.StatusInternalServerError, "")
	c := errorFixtureClient(t, e)

	body := map[string]any{"data": map[string]any{"type": "apps", "id": "1"}}
	_, err := asc.Patch[asc.Single[map[string]any]](
		context.Background(), c, "/v1/apps/1", nil, body,
	)
	if err == nil {
		t.Fatal("PATCH returned nil error; want 500")
	}

	msg := err.Error()
	for _, want := range []string{"500", "INTERNAL_ERROR", "unexpected error"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err.Error() missing %q: %q", want, msg)
		}
	}

	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (5xx no indefinite retry)", got)
	}
	assertNoCredentialLeakage(t, msg)
}

// TestWriteError_502_503_504_NoRetry locks the no-retry contract for the other
// server-error codes. Body content is irrelevant; only the status path matters.
func TestWriteError_502_503_504_NoRetry(t *testing.T) {
	cases := []struct {
		name string
		code int
	}{
		{"502 bad gateway", http.StatusBadGateway},
		{"503 service unavailable", http.StatusServiceUnavailable},
		{"504 gateway timeout", http.StatusGatewayTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newErrorFixtureServer(t, "error_500", tc.code, "")
			c := errorFixtureClient(t, e)

			body := map[string]any{"data": map[string]any{"type": "apps", "id": "1"}}
			_, err := asc.Patch[asc.Single[map[string]any]](
				context.Background(), c, "/v1/apps/1", nil, body,
			)
			if err == nil {
				t.Fatalf("%s: PATCH returned nil error", tc.name)
			}
			var apiErr *asc.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("%s: err = %v, want *asc.APIError", tc.name, err)
			}
			if apiErr.HTTPStatus != tc.code {
				t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, tc.code)
			}
			if got := e.count.Load(); got != 1 {
				t.Errorf("server saw %d requests; want 1 (no auto-retry)", got)
			}
			assertNoCredentialLeakage(t, err.Error())
		})
	}
}

// TestWriteError_AllVerbs_SurfaceTypedErrorOn422 asserts every write verb
// fast-fails identically on 4xx, catching drift if a verb skips the typed path.
func TestWriteError_AllVerbs_SurfaceTypedErrorOn422(t *testing.T) {
	e := newErrorFixtureServer(t, "error_422_validation", http.StatusUnprocessableEntity, "")
	c := errorFixtureClient(t, e)
	ctx := context.Background()
	body := map[string]any{"data": map[string]any{"type": "apps", "id": "1"}}

	verbs := []struct {
		name string
		call func() error
	}{
		{
			name: "POST",
			call: func() error {
				_, err := asc.Post[asc.Single[map[string]any]](ctx, c, "/v1/apps", nil, body)
				return err
			},
		},
		{
			name: "PATCH",
			call: func() error {
				_, err := asc.Patch[asc.Single[map[string]any]](ctx, c, "/v1/apps/1", nil, body)
				return err
			},
		},
		{
			name: "DELETE",
			call: func() error {
				return c.Delete(ctx, "/v1/apps/1", nil)
			},
		},
		{
			name: "DeleteWithBody",
			call: func() error {
				return c.DeleteWithBody(ctx, "/v1/betaGroups/1/relationships/betaTesters", nil, body)
			},
		},
	}
	for _, v := range verbs {
		t.Run(v.name, func(t *testing.T) {
			assertTypedErrorOn422(t, v.name, v.call())
		})
	}
}

func assertTypedErrorOn422(t *testing.T, verb string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s on 422: nil error", verb)
	}
	var apiErr *asc.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("%s: err = %v, want *asc.APIError", verb, err)
	}
	if apiErr.HTTPStatus != http.StatusUnprocessableEntity {
		t.Errorf("%s: HTTPStatus = %d, want 422", verb, apiErr.HTTPStatus)
	}
	// POST/PATCH read the body before draining; DELETE/DeleteWithBody drain
	// before decode (QA-007). Lock both so either regression is loud.
	switch verb {
	case "POST", "PATCH":
		if len(apiErr.Errors) < 1 {
			t.Errorf("%s: Errors len = %d, want >= 1", verb, len(apiErr.Errors))
		}
	case "DELETE", "DeleteWithBody":
		if len(apiErr.Errors) != 0 {
			t.Logf("%s: Errors len = %d (QA-007 may be resolved); previously empty due to body-drain-before-decode bug", verb, len(apiErr.Errors))
		}
	}
	assertNoCredentialLeakage(t, err.Error())
}

// TestWriteError_ReviewerDemo_RedactsLeakedPassword asserts the redactor
// scrubs JWT- and bearer-shaped tokens from a 422 error[] detail.
func TestWriteError_ReviewerDemo_RedactsLeakedPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		// Only redactor-catchable patterns; a plain password like "hunter2"
		// matches no pattern, which is documented behaviour.
		_, _ = w.Write([]byte(`{"errors":[{"code":"VALIDATION","title":"bad","detail":"echo of header: Bearer eyJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJ4In0.signed"}]}`))
	}))
	t.Cleanup(srv.Close)
	c := fixtureASCClient(t, srv)

	body := map[string]any{"data": map[string]any{"type": "appStoreReviewDetails"}}
	_, err := asc.Patch[asc.Single[map[string]any]](
		context.Background(), c, "/v1/appStoreReviewDetails/1", nil, body,
	)
	if err == nil {
		t.Fatal("PATCH returned nil; want error")
	}
	msg := err.Error()
	if strings.Contains(msg, "eyJ") {
		t.Errorf("err.Error() leaked JWT-shaped token: %q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "bearer ey") {
		t.Errorf("err.Error() leaked bearer header value: %q", msg)
	}
}

// assertNoCredentialLeakage asserts the cmd-layer error string is post-redaction.
func assertNoCredentialLeakage(t *testing.T, msg string) {
	t.Helper()
	if strings.Contains(msg, "eyJ") {
		t.Errorf("msg leaked JWT-shaped token: %q", msg)
	}
	low := strings.ToLower(msg)
	if strings.Contains(low, "bearer ey") {
		t.Errorf("msg leaked bearer header: %q", msg)
	}
	// "TEST123ABC" is the ephemeral test keyID; its presence means the
	// 10-char keyID redactor failed.
	if strings.Contains(msg, "TEST123ABC") {
		t.Errorf("msg leaked test key ID: %q", msg)
	}
}
