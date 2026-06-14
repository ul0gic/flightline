package asc

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// jwtRegex mirrors the production redactor in errors.go; tests assert it never matches.
var jwtRegex = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

// keyIDLeakRegex matches a standalone 10-char ASC key ID; tests assert it never matches.
var keyIDLeakRegex = regexp.MustCompile(`\b[A-Z0-9]{10}\b`)

func TestFixtureReplay_AppsList(t *testing.T) {
	srv := fixtureServer(t, map[string]FixtureRoute{
		"GET /v1/apps": {File: "apps_list"},
	})
	c := fixtureClient(t, srv)

	got, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/apps", url.Values{"limit": {"200"}})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Data) != 3 {
		t.Fatalf("data len = %d, want 3", len(got.Data))
	}
	wantBundles := []string{"com.example.alpha", "com.example.beta", "com.example.gamma"}
	for i, want := range wantBundles {
		if got.Data[i].Attributes.BundleID != want {
			t.Errorf("data[%d].bundleId = %q, want %q", i, got.Data[i].Attributes.BundleID, want)
		}
	}
	if got.Meta.Paging.Total != 3 {
		t.Errorf("meta.paging.total = %d, want 3", got.Meta.Paging.Total)
	}
	if got.Links.Next != "" {
		t.Errorf("links.next = %q, want empty (single-page fixture)", got.Links.Next)
	}
}

func TestFixtureReplay_AppsGetByBundleId(t *testing.T) {
	srv := fixtureServer(t, map[string]FixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
	})
	c := fixtureClient(t, srv)

	got, err := Get[Collection[appAttrs]](
		context.Background(), c, "/v1/apps",
		url.Values{"filter[bundleId]": {"com.example.alpha"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("data len = %d, want 1", len(got.Data))
	}
	if got.Data[0].Attributes.BundleID != "com.example.alpha" {
		t.Errorf("bundleId = %q, want com.example.alpha", got.Data[0].Attributes.BundleID)
	}
	if got.Data[0].ID != "1234567890" {
		t.Errorf("id = %q, want 1234567890", got.Data[0].ID)
	}
}

// TestFixtureReplay_AppsGetNotFound: Apple returns HTTP 200 with an empty
// array (not 404) on no match; mapping that to a typed error is the caller's job.
func TestFixtureReplay_AppsGetNotFound(t *testing.T) {
	srv := fixtureServer(t, map[string]FixtureRoute{
		"GET /v1/apps": {File: "apps_get_notFound"},
	})
	c := fixtureClient(t, srv)

	got, err := Get[Collection[appAttrs]](
		context.Background(), c, "/v1/apps",
		url.Values{"filter[bundleId]": {"com.unknown.app"}, "limit": {"1"}},
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Data) != 0 {
		t.Errorf("data len = %d, want 0 (Apple returns empty array, not 404)", len(got.Data))
	}
	if got.Meta.Paging.Total != 0 {
		t.Errorf("meta.paging.total = %d, want 0", got.Meta.Paging.Total)
	}
}

func TestFixtureReplay_WhoamiAuthProbe(t *testing.T) {
	srv := fixtureServer(t, map[string]FixtureRoute{
		"GET /v1/apps": {File: "whoami_apps_limit1"},
	})
	c := fixtureClient(t, srv)

	type minAttrs struct{}
	got, err := Get[Collection[minAttrs]](context.Background(), c, "/v1/apps", url.Values{"limit": {"1"}})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Data) != 1 {
		t.Errorf("data len = %d, want 1", len(got.Data))
	}
}

func assertErrorEnvelope(t *testing.T, err error, status int, wantCode, wantTitle string, wantSentinel error) {
	t.Helper()
	if err == nil {
		t.Fatal("Get: want error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError via errors.As", err)
	}
	if apiErr.HTTPStatus != status {
		t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, status)
	}
	if len(apiErr.Errors) != 1 {
		t.Fatalf("Errors len = %d, want 1", len(apiErr.Errors))
	}
	if apiErr.Errors[0].Code != wantCode {
		t.Errorf("Errors[0].Code = %q, want %q", apiErr.Errors[0].Code, wantCode)
	}
	msg := err.Error()
	if !strings.Contains(msg, wantCode) {
		t.Errorf("err.Error() = %q, missing code %q", msg, wantCode)
	}
	if !strings.Contains(msg, wantTitle) {
		t.Errorf("err.Error() = %q, missing title %q", msg, wantTitle)
	}
	if jwtRegex.MatchString(msg) {
		t.Errorf("err.Error() leaked a JWT-shaped token: %q", msg)
	}
	if keyIDLeakRegex.MatchString(msg) {
		t.Errorf("err.Error() leaked a key-ID-shaped token: %q", msg)
	}
	if wantSentinel != nil && !errors.Is(err, wantSentinel) {
		t.Errorf("errors.Is(err, %v) = false, want true", wantSentinel)
	}
}

func TestFixtureReplay_ErrorEnvelopes(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		status      int
		wantCode    string
		wantTitle   string
		wantSentinl error
	}{
		{
			name:        "401 unauthorized",
			file:        "error_401",
			status:      http.StatusUnauthorized,
			wantCode:    "NOT_AUTHORIZED",
			wantTitle:   "Authentication credentials are missing or invalid.",
			wantSentinl: ErrUnauthorized,
		},
		{
			name:        "403 forbidden",
			file:        "error_403",
			status:      http.StatusForbidden,
			wantCode:    "FORBIDDEN_ERROR",
			wantTitle:   "This request is forbidden for security reasons",
			wantSentinl: ErrForbidden,
		},
		{
			name:      "429 rate limit",
			file:      "error_429_rate_limit",
			status:    http.StatusTooManyRequests,
			wantCode:  "RATE_LIMIT_EXCEEDED",
			wantTitle: "The request was throttled.",
		},
		{
			name:      "500 internal server error",
			file:      "error_500",
			status:    http.StatusInternalServerError,
			wantCode:  "INTERNAL_ERROR",
			wantTitle: "An unexpected error occurred.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := fixtureServer(t, map[string]FixtureRoute{
				"GET /v1/apps": {File: tc.file, Status: tc.status},
			})
			c := fixtureClient(t, srv)
			_, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/apps", nil)
			assertErrorEnvelope(t, err, tc.status, tc.wantCode, tc.wantTitle, tc.wantSentinl)
		})
	}
}

func TestFixtureServer_UnknownRouteIs404(t *testing.T) {
	srv := fixtureServer(t, map[string]FixtureRoute{
		"GET /v1/apps": {File: "apps_list"},
	})
	c := fixtureClient(t, srv)

	_, err := Get[Collection[appAttrs]](context.Background(), c, "/v1/users", nil)
	if err == nil {
		t.Fatal("want 404 error for unrouted path")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", apiErr.HTTPStatus)
	}
	if len(apiErr.Errors) == 0 || apiErr.Errors[0].Code != "FIXTURE_NO_ROUTE" {
		t.Errorf("Errors = %+v, want a FIXTURE_NO_ROUTE diagnostic", apiErr.Errors)
	}
}
