package asc

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIError_Error_FormatsTitleAndCode(t *testing.T) {
	e := &APIError{
		HTTPStatus: 404,
		Errors: []ErrorItem{
			{Code: "NOT_FOUND", Title: "The resource was not found.", Detail: "App with bundle id ... does not exist."},
		},
	}
	got := e.Error()
	for _, want := range []string{"HTTP 404", "NOT_FOUND", "not found"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want substring %q", got, want)
		}
	}
}

func TestAPIError_Error_NoErrorsBody(t *testing.T) {
	e := &APIError{HTTPStatus: 500}
	got := e.Error()
	if !strings.Contains(got, "HTTP 500") {
		t.Errorf("Error() = %q, want HTTP 500", got)
	}
}

func TestAPIError_Error_SummarizesExtras(t *testing.T) {
	e := &APIError{
		HTTPStatus: 422,
		Errors: []ErrorItem{
			{Code: "A", Title: "first"},
			{Code: "B", Title: "second"},
			{Code: "C", Title: "third"},
		},
	}
	got := e.Error()
	if !strings.Contains(got, "+2 more") {
		t.Errorf("Error() = %q, want '+2 more'", got)
	}
}

func TestAPIError_Is_Sentinels(t *testing.T) {
	tests := []struct {
		name   string
		status int
		target error
		want   bool
	}{
		{"401 is ErrUnauthorized", 401, ErrUnauthorized, true},
		{"403 is ErrForbidden", 403, ErrForbidden, true},
		{"500 is not ErrUnauthorized", 500, ErrUnauthorized, false},
		{"401 is APIError", 401, &APIError{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &APIError{HTTPStatus: tt.status}
			var asErr error = e
			if got := errors.Is(asErr, tt.target); got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", asErr, tt.target, got, tt.want)
			}
		})
	}
}

func TestRedact_StripsJWT(t *testing.T) {
	in := "Bearer eyJhbGciOiJFUzI1NiIsImtpZCI6IkFCQzEyM0RFRjQifQ.eyJpc3MiOiJ0In0.signature_here_xxx"
	got := redact(in)
	if strings.Contains(got, "eyJ") {
		t.Errorf("redact() leaked JWT: %q", got)
	}
	if !strings.Contains(got, "[REDACTED-JWT]") {
		t.Errorf("redact() did not stamp REDACTED: %q", got)
	}
}

func TestRedact_StripsBearerHeader(t *testing.T) {
	in := "Authorization: Bearer abc123def456"
	got := redact(in)
	if strings.Contains(got, "abc123def456") {
		t.Errorf("redact() leaked bearer token: %q", got)
	}
}

func TestRedact_StripsKeyID(t *testing.T) {
	// 10 char uppercase token like Apple's key IDs.
	in := "key ID ABC1234DEF used"
	got := redact(in)
	if strings.Contains(got, "ABC1234DEF") {
		t.Errorf("redact() leaked key ID: %q", got)
	}
}

func TestRedact_PreservesNumericAppID(t *testing.T) {
	in := `no app found with bundleId "6762067669"`
	got := redact(in)
	if !strings.Contains(got, "6762067669") {
		t.Errorf("redact() ate a numeric app ID: %q", got)
	}
}

func TestAPIError_Error_RedactsLeakedToken(t *testing.T) {
	// Imagine a server pathologically echoes a token back; ensure Error() redacts.
	e := &APIError{
		HTTPStatus: 400,
		Errors: []ErrorItem{
			{Code: "BAD", Title: "Invalid", Detail: "Bearer eyJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJ0In0.sig was rejected"},
		},
	}
	if strings.Contains(e.Error(), "eyJ") {
		t.Errorf("Error() leaked JWT: %q", e.Error())
	}
}
