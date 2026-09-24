package cmd

import "github.com/ul0gic/flightline/internal/plan"

// plannedApplyRows is the preview-only table segment. G3 appends this to
// ApplyResult.TableRows before applied/skipped/error rows.
func plannedApplyRows(changes []plan.Change) [][]string {
	rows := make([][]string, 0, len(changes))
	for _, change := range changes {
		rows = append(rows, []string{"planned", string(change.Op), change.Path, ""})
	}
	return rows
}
