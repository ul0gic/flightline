package state

import (
	"fmt"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// ValidateVersionChanges preserves metadata editability when a snapshot was
// allowed solely to plan a phased-release operation on a distributed version.
func ValidateVersionChanges(live *config.State, changes []plan.Change) []ChangeError {
	if live == nil || live.ObservedVersionState == "" {
		return nil
	}
	if _, editable := editableVersionStates[live.ObservedVersionState]; editable {
		return nil
	}
	var failures []ChangeError
	for _, change := range changes {
		if change.Path == "/spec/version/phasedRelease" {
			continue
		}
		failures = append(failures, newChangeError(change, fmt.Errorf("version is %s; only phased-release changes may be planned for a noneditable version", live.ObservedVersionState)))
	}
	return failures
}
