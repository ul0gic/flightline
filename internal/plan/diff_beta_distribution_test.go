package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF5C_BetaGroupBuildsDiffOmittedNoOpAndExplicitClear(t *testing.T) {
	build := config.BetaBuildSelector{Number: "42", Version: "1.2", Platform: "IOS"}
	live := []config.BetaBuildSelector{build}
	var changes []Change
	diffBetaGroupBuilds("Beta/QA", nil, &live, &changes)
	if len(changes) != 0 {
		t.Fatalf("omitted builds changed membership: %+v", changes)
	}
	empty := []config.BetaBuildSelector{}
	diffBetaGroupBuilds("Beta/QA", &empty, &live, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/testflight/groups/Beta~1QA/builds" || changes[0].Op != OpUpdate {
		t.Fatalf("explicit empty membership changes=%+v", changes)
	}
	if to, ok := changes[0].To.([]config.BetaBuildSelector); !ok || to == nil || len(to) != 0 {
		t.Fatalf("explicit clear To=%#v", changes[0].To)
	}
}

func TestF5C_BetaGroupBuildsDiffIgnoresOrdering(t *testing.T) {
	a := config.BetaBuildSelector{Number: "42", Version: "1.2", Platform: "IOS"}
	b := config.BetaBuildSelector{Number: "43", Version: "1.2", Platform: "IOS"}
	want, have := []config.BetaBuildSelector{a, b}, []config.BetaBuildSelector{b, a}
	var changes []Change
	diffBetaGroupBuilds("Beta", &want, &have, &changes)
	if len(changes) != 0 {
		t.Fatalf("ordering-only change=%+v", changes)
	}
}
