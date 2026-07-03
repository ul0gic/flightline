package cmd

import (
	"errors"
	"fmt"
)

// ExitError carries a specific process exit code out of a command; an empty
// Message means the diagnostics were already rendered and nothing more prints.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// ExitCode maps err to the process exit code: 0 for nil, the carried code for
// an ExitError, 1 for anything else.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}

// diagnosticsExit maps a rendered lint/preflight summary onto the documented
// contract: exit 1 on any error, 2 on warnings-only, 0 when clean.
func diagnosticsExit(mode string, s LintResultSummary) error {
	switch {
	case s.Error == 1:
		return &ExitError{Code: 1, Message: mode + ": 1 error-severity diagnostic: see output above"}
	case s.Error > 1:
		return &ExitError{Code: 1, Message: fmt.Sprintf("%s: %d error-severity diagnostics: see output above", mode, s.Error)}
	case s.Warning > 0:
		return &ExitError{Code: 2, Message: fmt.Sprintf("%s: %d warning(s): exiting 2 (warnings, no errors)", mode, s.Warning)}
	}
	return nil
}
