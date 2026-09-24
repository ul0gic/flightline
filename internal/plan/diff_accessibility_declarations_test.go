package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF4A_AccessibilityDiffPreservesOmissionAndConverges(t *testing.T) {
	trueValue, falseValue, draft, published := true, false, "DRAFT", "PUBLISHED"
	live := &config.State{Spec: config.StateSpec{AccessibilityDeclarations: &config.AccessibilityDeclarationsSpec{Families: map[string]config.AccessibilityDeclarationSpec{
		"IPHONE": {State: &draft, SupportsVoiceover: &trueValue},
		"IPAD":   {State: &published, SupportsCaptions: &falseValue},
	}}}}
	if changes := f4aAccessibilityDiff(&config.State{}, live); len(changes) != 0 {
		t.Fatalf("omission changed live state: %+v", changes)
	}
	if changes := f4aAccessibilityDiff(live, live); len(changes) != 0 {
		t.Fatalf("fetched state changed itself: %+v", changes)
	}
	desired := &config.State{Spec: config.StateSpec{AccessibilityDeclarations: &config.AccessibilityDeclarationsSpec{Families: map[string]config.AccessibilityDeclarationSpec{
		"IPHONE": {SupportsVoiceover: &falseValue},
		"MAC":    {SupportsCaptions: &falseValue},
	}}}}
	changes := f4aAccessibilityDiff(desired, live)
	if len(changes) != 2 || changes[0].Path != "/spec/accessibilityDeclarations/families/IPHONE" || changes[0].Op != OpUpdate || changes[1].Op != OpCreate {
		t.Fatalf("changes=%+v", changes)
	}
	changedLive := &config.State{Spec: config.StateSpec{AccessibilityDeclarations: &config.AccessibilityDeclarationsSpec{Families: map[string]config.AccessibilityDeclarationSpec{
		"IPHONE": {State: &draft, SupportsVoiceover: &falseValue},
		"IPAD":   {State: &published, SupportsCaptions: &falseValue},
		"MAC":    {State: &draft, SupportsCaptions: &falseValue},
	}}}}
	if remaining := f4aAccessibilityDiff(desired, changedLive); len(remaining) != 0 {
		t.Fatalf("remaining=%+v", remaining)
	}
}

func f4aAccessibilityDiff(desired, live *config.State) []Change {
	var changes []Change
	diffAccessibilityDeclarations(desired, live, &changes)
	return changes
}
