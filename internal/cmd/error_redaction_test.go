package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

var jwtPatternCmd = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

// Fixture JWTs carry the literal "TEST123ABC" key ID; any occurrence in a cmd-layer error means the redactor regressed.
var keyIDPatternCmd = regexp.MustCompile(`\bTEST123ABC\b`)

// Confirms cmd-layer wrapping (resolveAppID etc.) preserves the typed error and never re-introduces creds.
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

			_, err := resolveAppID(context.Background(), c, "com.example.alpha")
			assertCmdLayerErrorRedacted(t, err, tc.status, tc.wantCode, tc.wantSentnl)
		})
	}
}

func assertCmdLayerErrorRedacted(t *testing.T, err error, wantStatus int, wantCode string, wantSentnl error) {
	t.Helper()
	if err == nil {
		t.Fatal("resolveAppID: want error, got nil")
	}

	msg := err.Error()

	var apiErr *asc.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *asc.APIError via errors.As (cmd-layer wrap dropped the type)", err)
	}
	if apiErr.HTTPStatus != wantStatus {
		t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, wantStatus)
	}

	if !strings.Contains(msg, wantCode) {
		t.Errorf("err.Error() = %q, missing Apple code %q", msg, wantCode)
	}

	if wantSentnl != nil && !errors.Is(err, wantSentnl) {
		t.Errorf("errors.Is(err, %v) = false: sentinel mapping broken at cmd layer", wantSentnl)
	}

	if jwtPatternCmd.MatchString(msg) {
		t.Errorf("err.Error() leaked a JWT: %q", msg)
	}

	if keyIDPatternCmd.MatchString(msg) {
		t.Errorf("err.Error() leaked the test key ID TEST123ABC: %q", msg)
	}
}

// Apple returns 200 with an empty data array (not 404); the error must still name the missing bundleId.
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
	if jwtPatternCmd.MatchString(msg) || keyIDPatternCmd.MatchString(msg) {
		t.Errorf("err.Error() leaked credentials: %q", msg)
	}
}

// Guards against a skip-on-error refactor of asc.Pages that would make every list command silently lossy.
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
