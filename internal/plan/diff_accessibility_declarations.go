package plan

import (
	"sort"

	"github.com/ul0gic/flightline/internal/config"
)

func diffAccessibilityDeclarations(desired, live *config.State, out *[]Change) {
	if desired == nil || desired.Spec.AccessibilityDeclarations == nil {
		return
	}
	var observed map[string]config.AccessibilityDeclarationSpec
	if live != nil && live.Spec.AccessibilityDeclarations != nil {
		observed = live.Spec.AccessibilityDeclarations.Families
	}
	families := make([]string, 0, len(desired.Spec.AccessibilityDeclarations.Families))
	for family := range desired.Spec.AccessibilityDeclarations.Families {
		families = append(families, family)
	}
	sort.Strings(families)
	for _, family := range families {
		want := desired.Spec.AccessibilityDeclarations.Families[family]
		have, exists := observed[family]
		if !exists && !config.AccessibilityHasAnswers(want) {
			continue
		}
		if exists && len(config.AccessibilityAnswerChanges(want, have)) == 0 {
			continue
		}
		if exists && have.State != nil && *have.State != "DRAFT" {
			continue
		}
		op := OpUpdate
		var from any = have
		if !exists {
			op, from = OpCreate, nil
		}
		want.State = nil
		*out = append(*out, Change{
			Op: op, Resource: "accessibilityDeclarations." + family,
			Path: "/spec/accessibilityDeclarations/families/" + family,
			From: from, To: want,
			Hint: "set accessibility answers for " + family,
		})
	}
}
