package asc

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sentinel errors for fast-fail no-retry cred problems. Use errors.Is to test.
var (
	// ErrUnauthorized is returned for HTTP 401. The credential is wrong; do not retry.
	ErrUnauthorized = errors.New("asc: unauthorized (HTTP 401) — check key ID, issuer ID, and .p8 path")
	// ErrForbidden is returned for HTTP 403. The credential is valid but lacks permission.
	ErrForbidden = errors.New("asc: forbidden (HTTP 403) — credential lacks permission for this resource")
)

// APIError is the typed error wrapping Apple's errors[] payload on a 4xx/5xx
// response. HTTPStatus is the response status code; Errors is the parsed
// errors[] array (may be empty if Apple returned a non-JSON error body).
type APIError struct {
	HTTPStatus int
	Errors     []ErrorItem
}

// ErrorItem mirrors one entry in Apple's errors[] array.
// id, status, code, title, detail are documented strings; source and meta are
// loose object payloads that vary per error code.
type ErrorItem struct {
	ID     string         `json:"id,omitempty"`
	Status string         `json:"status,omitempty"`
	Code   string         `json:"code,omitempty"`
	Title  string         `json:"title,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Source map[string]any `json:"source,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// Error formats a human-readable summary of an APIError. The first item's
// title and detail are surfaced; remaining items are summarized as a count.
// All output is run through redact() before return so that JWTs, bearer
// tokens, and 10-character ASC key IDs cannot leak through error messages.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "asc: HTTP %d", e.HTTPStatus)
	if len(e.Errors) == 0 {
		return redact(sb.String())
	}
	first := e.Errors[0]
	if first.Title != "" {
		fmt.Fprintf(&sb, ": %s", first.Title)
	}
	if first.Code != "" {
		fmt.Fprintf(&sb, " [%s]", first.Code)
	}
	if first.Detail != "" {
		fmt.Fprintf(&sb, " — %s", first.Detail)
	}
	if extra := len(e.Errors) - 1; extra > 0 {
		fmt.Fprintf(&sb, " (+%d more error(s))", extra)
	}
	return redact(sb.String())
}

// Is supports errors.Is across the sentinels and APIError targets:
//
//	errors.Is(err, &asc.APIError{})    // any API error
//	errors.Is(err, asc.ErrUnauthorized) // 401 specifically
//	errors.Is(err, asc.ErrForbidden)    // 403 specifically
func (e *APIError) Is(target error) bool {
	if e == nil {
		return false
	}
	if _, ok := target.(*APIError); ok {
		return true
	}
	switch {
	case errors.Is(target, ErrUnauthorized):
		return e.HTTPStatus == 401
	case errors.Is(target, ErrForbidden):
		return e.HTTPStatus == 403
	}
	return false
}

// Redaction patterns. Compiled once at package load.
//
// jwtPattern matches a 3-segment JWT (eyJ... base64url . base64url . base64url).
// bearerPattern matches "Bearer <token>" / "bearer <token>" headers.
// keyIDPattern matches Apple's 10-character ASC key IDs as standalone tokens.
var (
	jwtPattern         = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	bearerPattern      = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`)
	keyIDPattern       = regexp.MustCompile(`\b[A-Z0-9]{10}\b`)
	authKeyPathPattern = regexp.MustCompile(`AuthKey_[A-Z0-9]{10}\.p8`)
	uuidPattern        = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
)

// Redact strips potential credential material from a string before it reaches
// stderr or a returned error. Defense-in-depth: callers should never put
// credentials into APIError fields, but if Apple ever echoes a token back in
// a 4xx body or a developer logs an Authorization header, we don't leak it.
//
// Patterns scrubbed:
//   - JWTs (eyJ-prefixed three-segment dotted base64url)
//   - "Bearer <token>" headers
//   - 10-char ASC API key IDs (whole-word boundary)
//   - AuthKey_<KEYID>.p8 filenames (the underscore breaks \b, so this is a
//     separate explicit pattern — see closed SEC-002 for context)
//   - Issuer UUIDs (the App Store Connect issuer ID format)
//
// Exported as Redact() so the cmd-layer printer in cmd/skipper/main.go can
// apply it to ALL error output, not just APIError.Error().
func Redact(s string) string {
	s = jwtPattern.ReplaceAllString(s, "[REDACTED-JWT]")
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	s = authKeyPathPattern.ReplaceAllString(s, "AuthKey_[REDACTED-KEYID].p8")
	s = keyIDPattern.ReplaceAllString(s, "[REDACTED-KEYID]")
	s = uuidPattern.ReplaceAllString(s, "[REDACTED-UUID]")
	return s
}

// redact is the unexported alias retained for in-package callers.
// New callers should use Redact directly.
func redact(s string) string { return Redact(s) }
