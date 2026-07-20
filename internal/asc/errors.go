package asc

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Sentinel errors for fast-fail no-retry cred problems. Use errors.Is to test.
var (
	// ErrUnauthorized is returned for HTTP 401. The credential is wrong; do not retry.
	ErrUnauthorized = errors.New("asc: unauthorized (HTTP 401): check key ID, issuer ID, and .p8 path")
	// ErrForbidden is returned for HTTP 403. The credential is valid but lacks permission.
	ErrForbidden = errors.New("asc: forbidden (HTTP 403): credential lacks permission for this resource")
)

// APIError wraps Apple's errors[] payload. RetryAfter is set on 429 if Apple sent a Retry-After header.
type APIError struct {
	HTTPStatus int
	Errors     []ErrorItem
	RetryAfter time.Duration
}

// ErrorItem mirrors one entry in Apple's errors[] array.
type ErrorItem struct {
	ID     string         `json:"id,omitempty"`
	Status string         `json:"status,omitempty"`
	Code   string         `json:"code,omitempty"`
	Title  string         `json:"title,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Source map[string]any `json:"source,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// Error formats a human-readable summary. All output passes through redact() to prevent credential leakage.
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
		fmt.Fprintf(&sb, ": %s", first.Detail)
	}
	if extra := len(e.Errors) - 1; extra > 0 {
		fmt.Fprintf(&sb, " (+%d more error(s))", extra)
	}
	if e.RetryAfter > 0 {
		fmt.Fprintf(&sb, " (retry after %s)", e.RetryAfter)
	}
	return redact(sb.String())
}

// Is matches *APIError (any API error), ErrUnauthorized (401), and ErrForbidden (403).
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

var (
	jwtPattern         = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`) // 3-segment JWT
	bearerPattern      = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]+`)
	keyIDPattern       = regexp.MustCompile(`\b[A-Z0-9]{10}\b`) // Apple 10-char ASC key IDs
	authKeyPathPattern = regexp.MustCompile(`AuthKey_[A-Z0-9]{10}\.p8`)
	uuidPattern        = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
)

// Redact strips credential material from a string before it reaches stderr or error output.
// AuthKey_.p8 uses a dedicated pattern because the underscore breaks the \b key-ID boundary.
func Redact(s string) string {
	s = jwtPattern.ReplaceAllString(s, "[REDACTED-JWT]")
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	s = authKeyPathPattern.ReplaceAllString(s, "AuthKey_[REDACTED-KEYID].p8")
	// All-digit 10-char tokens are Apple app/resource IDs, not key IDs — redacting them corrupts user-facing errors.
	s = keyIDPattern.ReplaceAllStringFunc(s, func(m string) string {
		if strings.ContainsAny(m, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return "[REDACTED-KEYID]"
		}
		return m
	})
	s = uuidPattern.ReplaceAllString(s, "[REDACTED-UUID]")
	return s
}

// redact is the unexported alias retained for in-package callers.
// New callers should use Redact directly.
func redact(s string) string { return Redact(s) }
