package state

import (
	"strings"

	"github.com/ul0gic/flightline/internal/plan"
)

// changeDependsOn names the supported reconciliation dependencies. Independent
// changes remain eligible after a failure; descendants of failed creates do not.
func changeDependsOn(child, parent plan.Change) bool {
	if parent.Op == plan.OpCreate && strings.HasPrefix(child.Path, parent.Path+"/") {
		return true
	}
	if isScreenshotOrderPath(child.Path) && strings.TrimSuffix(child.Path, "/order") == parent.Path {
		return true
	}
	return parent.Path == "/spec/build/number" && strings.HasPrefix(child.Path, "/spec/exportCompliance/")
}

// orderApplyChanges places dependencies first while preserving input order for
// independent changes. Dependencies only point to ancestors or the build field,
// so this graph cannot contain a cycle.
func orderApplyChanges(changes []plan.Change) []plan.Change {
	ordered := make([]plan.Change, 0, len(changes))
	visited := make([]bool, len(changes))
	var visit func(int)
	visit = func(i int) {
		if visited[i] {
			return
		}
		visited[i] = true
		for j := range changes {
			if i != j && changeDependsOn(changes[i], changes[j]) {
				visit(j)
			}
		}
		ordered = append(ordered, changes[i])
	}
	for i := range changes {
		visit(i)
	}
	return ordered
}

func hasFailedDependency(change plan.Change, failed []plan.Change) bool {
	for _, parent := range failed {
		if changeDependsOn(change, parent) {
			return true
		}
	}
	return false
}
