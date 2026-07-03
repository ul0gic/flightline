package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil is 0", err: nil, want: 0},
		{name: "plain error is 1", err: errors.New("boom"), want: 1},
		{name: "ExitError carries its code", err: &ExitError{Code: 2}, want: 2},
		{name: "wrapped ExitError carries its code", err: fmt.Errorf("outer: %w", &ExitError{Code: 2, Message: "inner"}), want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestDiagnosticsExit(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		summary  LintResultSummary
		wantCode int
		wantMsg  string
	}{
		{name: "clean is nil", mode: "lint", summary: LintResultSummary{}, wantCode: 0},
		{name: "info-only is nil", mode: "lint", summary: LintResultSummary{Info: 3}, wantCode: 0},
		{name: "single error", mode: "lint", summary: LintResultSummary{Error: 1}, wantCode: 1, wantMsg: "lint: 1 error-severity diagnostic: see output above"},
		{name: "multiple errors", mode: "preflight", summary: LintResultSummary{Error: 4}, wantCode: 1, wantMsg: "preflight: 4 error-severity diagnostics: see output above"},
		{name: "errors trump warnings", mode: "lint", summary: LintResultSummary{Error: 1, Warning: 2}, wantCode: 1, wantMsg: "lint: 1 error-severity diagnostic: see output above"},
		{name: "warnings-only exits 2", mode: "lint", summary: LintResultSummary{Warning: 2, Info: 1}, wantCode: 2, wantMsg: "lint: 2 warning(s): exiting 2 (warnings, no errors)"},
		{name: "preflight warnings-only exits 2", mode: "preflight", summary: LintResultSummary{Warning: 1}, wantCode: 2, wantMsg: "preflight: 1 warning(s): exiting 2 (warnings, no errors)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := diagnosticsExit(tt.mode, tt.summary)
			if got := ExitCode(err); got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; err = %v", got, tt.wantCode, err)
			}
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("message = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// Warnings-only YAML must surface exit 2 through the full runLint path.
func TestRunLint_WarningsOnlyExitsTwo(t *testing.T) {
	body := goodStateYAML + `  reviewerDemo:
    contactEmail: "joe at example dot com"
`
	p := writeTempState(t, body)
	viper.Reset()
	viper.Set("output", "json")
	var err error
	out := captureStdout(t, func() {
		err = runLint(lintCmd, []string{p})
	})
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode = %d, want 2; err = %v\nout = %s", got, err, out)
	}
	if !strings.Contains(err.Error(), "warning(s)") {
		t.Errorf("error %q does not mention warnings", err.Error())
	}
}
