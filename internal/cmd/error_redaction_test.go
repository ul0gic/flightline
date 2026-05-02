package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ul0gic/skipper/internal/asc"
)

// jwtPatternCmd matches a 3-segment JWT (eyJ.eyJ.sig). Mirrors the
// asc package's jwtRegex; duplicated rather than exported to keep the
// asc test surface tight.
var jwtPatternCmd = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

// keyIDPatternCmd matches plausible 10-char ASC key IDs as standalone
// tokens. Used to assert error strings have been redacted before bubbling
// to the cmd layer. The fixture client uses the literal "TEST123ABC" key
// ID in JWTs — if that string appears in any cmd-layer error, the
// redactor regressed.
var keyIDPatternCmd = regexp.MustCompile(`\bTEST123ABC\b`)

// TestErrorPaths_CmdLayerRedacted is the cross-cutting cmd-level error
// path test. The asc package already verifies redaction at its layer
// (TestFixtureReplay_ErrorEnvelopes); this test confirms the wrapping
// done at the cmd layer (resolveAppID, collectBuilds, etc.) does NOT
// re-introduce credentials by, say, embedding the URL or request headers.
//
// We replay each canonical Apple error envelope (401, 403, 429, 500)
// through resolveAppID — the universal cmd-layer entry point that every
// list/get command uses to translate a bundleId into an app ID. If
// redaction holds here, it holds in every command that calls it.
//
// Failure modes this test catches:
//   - Cmd-layer error wrapping drops the asc.APIError type (errors.As fails)
//   - Cmd wrapping concatenates the JWT or key ID into the message
//   - Status code is lost in the wrap (callers can't branch on 401 vs 500)
//   - 401/403 sentinel routing breaks for callers using errors.Is
func TestErrorPaths_CmdLayerRedacted(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		status     int
		wantCode   string
		wantSentnl error
	}{
		{
			name:       "401 unauthorized propagates with sentinel and no creds",
			fixture:    "error_401",
			status:     http.StatusUnauthorized,
			wantCode:   "NOT_AUTHORIZED",
			wantSentnl: asc.ErrUnauthorized,
		},
		{
			name:       "403 forbidden propagates with sentinel and no creds",
			fixture:    "error_403",
			status:     http.StatusForbidden,
			wantCode:   "FORBIDDEN_ERROR",
			wantSentnl: asc.ErrForbidden,
		},
		{
			name:     "429 rate-limit propagates with code and no creds",
			fixture:  "error_429_rate_limit",
			status:   http.StatusTooManyRequests,
			wantCode: "RATE_LIMIT_EXCEEDED",
		},
		{
			name:     "500 internal propagates with code and no creds",
			fixture:  "error_500",
			status:   http.StatusInternalServerError,
			wantCode: "INTERNAL_ERROR",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := startFixtureServer(t, map[string]fixtureRoute{
				"GET /v1/apps": {File: tc.fixture, Status: tc.status},
			})
			c := fixtureASCClient(t, srv)

			// resolveAppID is the universal cmd-layer translator from
			// bundleId → appId. Every list/get command that takes a
			// bundle ID flows through it, so testing redaction here
			// covers the cmd-layer surface in one shot.
			_, err := resolveAppID(context.Background(), c, "com.example.alpha")
			if err == nil {
				t.Fatal("resolveAppID: want error, got nil")
			}

			msg := err.Error()

			// 1. Status code preserved through the wrap.
			var apiErr *asc.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *asc.APIError via errors.As (cmd-layer wrap dropped the type)", err)
			}
			if apiErr.HTTPStatus != tc.status {
				t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, tc.status)
			}

			// 2. Apple's error code surfaces in the message (actionable signal).
			if !strings.Contains(msg, tc.wantCode) {
				t.Errorf("err.Error() = %q, missing Apple code %q", msg, tc.wantCode)
			}

			// 3. Sentinel mapping survives wrapping (callers use errors.Is
			//    to branch on auth vs throttle vs server error).
			if tc.wantSentnl != nil && !errors.Is(err, tc.wantSentnl) {
				t.Errorf("errors.Is(err, %v) = false — sentinel mapping broken at cmd layer", tc.wantSentnl)
			}

			// 4. No JWT leaks into the cmd-layer error message.
			if jwtPatternCmd.MatchString(msg) {
				t.Errorf("err.Error() leaked a JWT: %q", msg)
			}

			// 5. No key ID leaks into the cmd-layer error message.
			if keyIDPatternCmd.MatchString(msg) {
				t.Errorf("err.Error() leaked the test key ID TEST123ABC: %q", msg)
			}
		})
	}
}

// TestErrorPaths_NotFoundFromAppleEnvelope verifies the not-found case
// (Apple returns HTTP 200 with empty data array, not 404) surfaces a
// useful message naming the missing bundleId. resolveAppID translates
// the empty-result shape into a typed error; this test pins the message
// format so users see "which bundle ID was missing" rather than a
// confusing "no error, but also no data" silence.
func TestErrorPaths_NotFoundFromAppleEnvelope(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_notFound"},
	})
	c := fixtureASCClient(t, srv)

	_, err := resolveAppID(context.Background(), c, "com.unknown.app")
	if err == nil {
		t.Fatal("resolveAppID: want error for not-found bundle, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "com.unknown.app") {
		t.Errorf("err.Error() = %q, must name the missing bundleId for actionable signal", msg)
	}
	// No creds in the not-found message either.
	if jwtPatternCmd.MatchString(msg) || keyIDPatternCmd.MatchString(msg) {
		t.Errorf("err.Error() leaked credentials: %q", msg)
	}
}

// TestErrorPaths_PaginationBubblesError proves the paging iterator
// surfaces an HTTP error mid-walk rather than silently truncating.
// collectBuilds (and every collect* helper that uses asc.Pages) must
// return early with the error wrapped — never swallow it and return a
// partial list.
//
// This is a regression guard for a class of bug: if a future refactor
// changes asc.Pages to skip-on-error, every list command becomes
// silently lossy.
func TestErrorPaths_PaginationBubblesError(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		// First hop succeeds (apps lookup), second hop fails (builds list).
		"GET /v1/apps":                   {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/builds": {File: "error_500", Status: http.StatusInternalServerError},
	})
	c := fixtureASCClient(t, srv)

	appID, err := resolveAppID(context.Background(), c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID happy-path setup: %v", err)
	}

	views, err := collectBuilds(context.Background(), c, "/v1/apps/"+appID+"/builds",
		url.Values{"limit": {"200"}}, 0)
	if err == nil {
		t.Fatal("collectBuilds: want error from 500, got nil (pagination silently swallowed the error)")
	}
	if len(views) != 0 {
		t.Errorf("collectBuilds: got %d views back with error, want 0 (no partial returns on error)", len(views))
	}
	// 500 must not leak creds either.
	if jwtPatternCmd.MatchString(err.Error()) || keyIDPatternCmd.MatchString(err.Error()) {
		t.Errorf("err.Error() leaked credentials: %q", err.Error())
	}
}
