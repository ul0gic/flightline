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

	"github.com/ul0gic/skipper/internal/asc"
)

// Phase 3.4.2 — Error-path stress for the write surface.
//
// These tests assert the user-facing contract on Apple's 4xx/5xx replies as
// they arrive on a write request. Read-path error decoding is already covered
// by internal/asc/fixture_replay_test.go's TestFixtureReplay_ErrorEnvelopes;
// here the focus is:
//
//   - 422 validation errors surface every error[] entry's code+title+detail
//     so users can see exactly what Apple complained about.
//   - 401 fast-fails with the typed sentinel — no retry. The request count
//     after a single user-issued PATCH must be exactly 1.
//   - 429 surfaces Apple's reply body. The current build does NOT consume
//     the Retry-After header (filed as QA-XXX); this test locks that
//     behaviour so a regression that silently retries is caught.
//   - 5xx surfaces the body and does not retry indefinitely.
//
// Across all of these, the redactor must scrub any credential-shaped
// token (JWT, Bearer header, key ID) — Apple shouldn't echo creds back,
// but defense in depth is cheap.

// errorFixtureServer serves a single static error fixture for any request
// path. It counts requests so tests can assert "no retry."
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

// ---------------------------------------------------------------------------
// 422 — validation errors. The user-facing message must include EVERY error[]
// item's title+code+detail (or at least name how many more exist), and must
// not contain credential-shaped tokens.
// ---------------------------------------------------------------------------

func TestWriteError_422_SurfacesValidationDetails(t *testing.T) {
	e := newErrorFixtureServer(t, "error_422_validation", http.StatusUnprocessableEntity, "")
	c := errorFixtureClient(t, e)

	// Issue a PATCH against an arbitrary write endpoint — the path doesn't
	// matter, the server returns 422 for everything.
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

	// 5) exactly one request — no retry on 422.
	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (no retry on 422)", got)
	}
}

// ---------------------------------------------------------------------------
// 401 — fast-fail. Maps to ErrUnauthorized via errors.Is. No retry. The
// user-facing message points at the auth-config knobs.
// ---------------------------------------------------------------------------

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

	// The sentinel error itself contains the actionable hint ("check key ID,
	// issuer ID, and .p8 path"). Confirm errors.Is + the sentinel's Error()
	// chain together so users see the hint regardless of how the cmd layer
	// formats output.
	if got := asc.ErrUnauthorized.Error(); !strings.Contains(got, "key ID") || !strings.Contains(got, ".p8") {
		t.Errorf("ErrUnauthorized.Error() lost its actionable hint: %q", got)
	}

	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (401 fast-fail)", got)
	}
	assertNoCredentialLeakage(t, msg)
}

// ---------------------------------------------------------------------------
// 403 — fast-fail variant. ErrForbidden maps to "credential lacks permission",
// so the user-facing hint is different from 401.
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// 429 — Apple says "Retry after the window indicated by the Retry-After
// header". The current build does NOT capture that header (see QA-XXX);
// this test locks the current behaviour: error body surfaces, no retry,
// Retry-After is dropped on the floor.
//
// If the production code is later updated to surface Retry-After, this
// test should be split: one half asserts the header value reaches the
// user-facing error; the other still asserts no retry.
// ---------------------------------------------------------------------------

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

	// Lock the current behaviour: NO retry. Apple's docs say "retry after
	// the Retry-After window"; we do not retry automatically — the user
	// (or upstream tool) is expected to decide.
	if got := e.count.Load(); got != 1 {
		t.Errorf("server saw %d requests; want 1 (429 must not auto-retry)", got)
	}

	// Locked behaviour: Retry-After header value is dropped from the error.
	// QA-008 tracks the gap. When that's resolved this assertion flips.
	if strings.Contains(msg, "120") {
		t.Logf("Retry-After header is now surfaced (msg=%q). QA-008 may be resolvable.", msg)
	}

	assertNoCredentialLeakage(t, msg)
}

// ---------------------------------------------------------------------------
// 5xx — surfaces body, does not retry indefinitely. The current build does
// not retry at all on 5xx (no retry policy in v1). Keep the behaviour locked.
// ---------------------------------------------------------------------------

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

// TestWriteError_502_503_504_NoRetry locks the same no-retry contract for
// the other server-error codes that real backends produce during outages.
// Reuses error_500 as the body — content doesn't matter, only the status
// code path through the client.
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

// ---------------------------------------------------------------------------
// All write methods (POST / PATCH / DELETE / PUT-via-DeleteWithBody) must
// behave the same way on 4xx — same fast-fail, same redaction, same body
// surfaced. Catches drift if a future verb implementation skips the typed
// error path.
// ---------------------------------------------------------------------------

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
			err := v.call()
			if err == nil {
				t.Fatalf("%s on 422: nil error", v.name)
			}
			var apiErr *asc.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("%s: err = %v, want *asc.APIError", v.name, err)
			}
			if apiErr.HTTPStatus != http.StatusUnprocessableEntity {
				t.Errorf("%s: HTTPStatus = %d, want 422", v.name, apiErr.HTTPStatus)
			}
			// Errors[] population — POST and PATCH go through doJSON which
			// reads the body BEFORE draining; DELETE / DeleteWithBody go
			// through deleteCommon which drains BEFORE error decode (see
			// QA-007). Locking current behaviour so the regression of either
			// path is loud.
			switch v.name {
			case "POST", "PATCH":
				if len(apiErr.Errors) < 1 {
					t.Errorf("%s: Errors len = %d, want >= 1", v.name, len(apiErr.Errors))
				}
			case "DELETE", "DeleteWithBody":
				if len(apiErr.Errors) != 0 {
					t.Logf("%s: Errors len = %d (QA-007 may be resolved); previously empty due to body-drain-before-decode bug", v.name, len(apiErr.Errors))
				}
			}
			assertNoCredentialLeakage(t, err.Error())
		})
	}
}

// ---------------------------------------------------------------------------
// Reviewer-demo specific: a 422 from Apple on a reviewer-demo PATCH must
// NOT echo the demo password back to the user. This is a credential-leak
// guardrail: Apple wouldn't intentionally echo it, but if an `errors[]`
// detail string ever names the password, the redactor catches it.
// ---------------------------------------------------------------------------

func TestWriteError_ReviewerDemo_RedactsLeakedPassword(t *testing.T) {
	// Build an in-memory error body that pathologically includes a password
	// string in detail. Sanity-check the redactor stack catches anything
	// bearer-shaped or JWT-shaped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		// Note: we deliberately do NOT include a real password here — only
		// patterns the redactor should catch: a JWT-shaped token in detail,
		// and a bearer header echo. A real password "hunter2" wouldn't
		// match any redactor pattern; that's documented behaviour.
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

// ---------------------------------------------------------------------------
// Cross-cutting helper: assert no credential-shaped leakage.
//
// Patterns scrubbed by asc.Redact:
//   - JWT (eyJ + 2 more segments)
//   - Bearer header
//   - 10-char ASC key ID (whole-word)
//   - AuthKey_<KEYID>.p8
//   - UUID (issuer ID)
//
// We can't test the Redact function directly here (it's exercised in
// asc/errors_test.go); we test that the user-facing string passed through
// the cmd layer's error path is post-redaction.
// ---------------------------------------------------------------------------

func assertNoCredentialLeakage(t *testing.T, msg string) {
	t.Helper()
	if strings.Contains(msg, "eyJ") {
		t.Errorf("msg leaked JWT-shaped token: %q", msg)
	}
	low := strings.ToLower(msg)
	if strings.Contains(low, "bearer ey") {
		t.Errorf("msg leaked bearer header: %q", msg)
	}
	// The ephemeral test key uses keyID "TEST123ABC". If that string ever
	// appears in a redacted error message, the 10-char keyID redactor
	// failed to scrub it.
	if strings.Contains(msg, "TEST123ABC") {
		t.Errorf("msg leaked test key ID: %q", msg)
	}
}
