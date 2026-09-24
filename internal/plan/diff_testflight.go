package plan

import (
	"fmt"
	"strings"

	"github.com/ul0gic/flightline/internal/config"
)

func diffTestFlight(d, l *config.TestFlightSpec, out *[]Change) {
	if d == nil {
		return
	}
	var liveMetadata *config.BetaMetadataSpec
	if l != nil {
		liveMetadata = l.Metadata
	}
	diffBetaMetadata(d.Metadata, liveMetadata, out)
	live := map[string]config.TestFlightGroup{}
	if l != nil && l.Groups != nil {
		live = l.Groups
	}
	for _, g := range sortedKeys(d.Groups) {
		dg := d.Groups[g]
		lg, exists := live[g]
		base := "/spec/testflight/groups/" + escapeTestFlightPathSegment(g)
		if !exists {
			*out = append(*out, Change{
				Op: OpCreate, Resource: "testflight." + g, Path: base, To: testFlightGroupParentChange(dg),
				Hint: "create TestFlight group " + g,
			})
			diffTestFlightTesters(g, base, dg.Testers, nil, out)
			diffBetaGroupBuilds(g, dg.Builds, nil, out)
			continue
		}
		emitIfDiff(out, "testflight."+g, base+"/isInternal", dg.IsInternal, lg.IsInternal)
		emitIfDiff(out, "testflight."+g, base+"/publicLink", dg.PublicLink, lg.PublicLink)
		emitIfDiff(out, "testflight."+g, base+"/publicLinkLimit", dg.PublicLinkLimit, lg.PublicLinkLimit)
		diffTestFlightTesters(g, base, dg.Testers, lg.Testers, out)
		diffBetaGroupBuilds(g, dg.Builds, lg.Builds, out)
	}
}

func testFlightGroupParentChange(group config.TestFlightGroup) config.TestFlightGroup {
	group.Testers = nil
	group.Builds = nil
	return group
}

func diffTestFlightTesters(group, base string, desired, live []config.TestFlightTester, out *[]Change) {
	if desired == nil {
		return
	}
	dEmails := testerEmails(desired)
	lEmails := testerEmails(live)
	for _, email := range dEmails {
		if contains(lEmails, email) {
			continue
		}
		*out = append(*out, Change{
			Op: OpCreate, Resource: "testflight." + group + ".testers",
			Path: base + "/testers/" + escapeTestFlightPathSegment(email), To: email,
			Hint: fmt.Sprintf("add tester %s to %s", email, group),
		})
	}
	for _, email := range lEmails {
		if contains(dEmails, email) {
			continue
		}
		*out = append(*out, Change{
			Op: OpDelete, Resource: "testflight." + group + ".testers",
			Path: base + "/testers/" + escapeTestFlightPathSegment(email), From: email,
			Hint: fmt.Sprintf("remove tester %s from %s", email, group),
		})
	}
}

func escapeTestFlightPathSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	return strings.ReplaceAll(segment, "/", "~1")
}
