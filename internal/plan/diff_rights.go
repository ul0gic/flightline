package plan

import (
	"slices"

	"github.com/ul0gic/flightline/internal/config"
)

func diffRights(desired, live *config.State, out *[]Change) {
	if desired == nil {
		return
	}
	if live == nil {
		live = &config.State{}
	}
	emitIfDiff(out, "contentRights", "/spec/contentRights", desired.Spec.ContentRights, live.Spec.ContentRights)
	want := desired.Spec.AppEULA
	if want == nil {
		return
	}
	have := live.Spec.AppEULA
	merged := config.AppEULASpec{}
	if have != nil {
		merged = *have
	}
	if want.AgreementText != nil {
		merged.AgreementText = want.AgreementText
	}
	if want.Territories != nil {
		copyIDs := slices.Clone(*want.Territories)
		slices.Sort(copyIDs)
		merged.Territories = &copyIDs
	}
	if have != nil && sameRightsText(merged.AgreementText, have.AgreementText) &&
		sameRightsTerritoryPointers(merged.Territories, have.Territories) {
		return
	}
	op := OpUpdate
	if have == nil {
		op = OpCreate
	}
	*out = append(*out, Change{Op: op, Resource: "appEula", Path: "/spec/appEula", From: have, To: merged, Hint: "reconcile custom app EULA"})
}

func sameRightsText(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func sameRightsTerritoryPointers(a, b *[]string) bool {
	return a == nil && b == nil || a != nil && b != nil && sameRightsTerritories(*a, *b)
}

func sameRightsTerritories(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
