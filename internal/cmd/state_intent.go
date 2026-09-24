package cmd

import (
	"fmt"
	"os"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
	"github.com/ul0gic/flightline/internal/state"
)

func validateCommandWriteIntent(file string, desired, live *config.State) error {
	diagnostics := writeIntentDiagnostics(file, desired, live)
	if len(diagnostics) == 0 {
		return nil
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintln(os.Stderr, diagnostic.String())
	}
	return fmt.Errorf("%s failed write intent validation (%d diagnostics)", file, len(diagnostics))
}

func writeIntentDiagnostics(file string, desired, live *config.State) []config.Diagnostic {
	diagnostics := config.ValidateWriteIntent(file, desired, live)
	seen := make(map[string]bool)
	for _, diagnostic := range diagnostics {
		seen[diagnostic.Path] = true
	}
	changes := plan.Diff(desired, live)
	failures := append(state.ValidateChanges(changes), state.ValidateVersionChanges(live, changes)...)
	for i := range failures {
		failure := &failures[i]
		if seen[failure.Change.Path] {
			continue
		}
		diagnostics = append(diagnostics, config.Diagnostic{File: file, Path: failure.Change.Path, Severity: config.SeverityError, Message: failure.MessageText()})
		seen[failure.Change.Path] = true
	}
	return diagnostics
}

func hasPhasedReleaseIntent(desired *config.State) bool {
	return desired != nil && desired.Spec.Version != nil && desired.Spec.Version.PhasedRelease != nil
}
