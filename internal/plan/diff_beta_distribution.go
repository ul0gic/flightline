package plan

import (
	"reflect"
	"sort"

	"github.com/ul0gic/flightline/internal/config"
)

func diffBetaGroupBuilds(groupName string, desired, live *[]config.BetaBuildSelector, out *[]Change) {
	if desired == nil {
		return
	}
	want := append([]config.BetaBuildSelector{}, (*desired)...)
	have := []config.BetaBuildSelector{}
	if live != nil {
		have = append(have, (*live)...)
	}
	less := func(values []config.BetaBuildSelector) {
		sort.Slice(values, func(i, j int) bool { return betaBuildKey(values[i]) < betaBuildKey(values[j]) })
	}
	less(want)
	less(have)
	if reflect.DeepEqual(want, have) {
		return
	}
	*out = append(*out, Change{
		Op: OpUpdate, Resource: "testflight." + groupName + ".builds",
		Path: "/spec/testflight/groups/" + escapeTestFlightPathSegment(groupName) + "/builds",
		From: have, To: want, Hint: "reconcile beta group " + groupName + " builds",
	})
}
