package config

import (
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func TestF4A_AccessibilityIntentPreservesOmissionAndRejectsPublishedEdit(t *testing.T) {
	trueValue, falseValue, draft, published := true, false, "DRAFT", "PUBLISHED"
	live := &State{Spec: StateSpec{AccessibilityDeclarations: &AccessibilityDeclarationsSpec{Families: map[string]AccessibilityDeclarationSpec{
		"IPHONE": {State: &draft, SupportsVoiceover: &trueValue},
		"IPAD":   {State: &published, SupportsVoiceover: &trueValue},
	}}}}
	for _, tc := range []struct {
		name     string
		families map[string]AccessibilityDeclarationSpec
		want     bool
	}{
		{"omitted", nil, false},
		{"unchanged published snapshot", map[string]AccessibilityDeclarationSpec{"IPAD": {State: &published, SupportsVoiceover: &trueValue}}, false},
		{"draft false answer", map[string]AccessibilityDeclarationSpec{"IPHONE": {SupportsVoiceover: &falseValue}}, false},
		{"published changed", map[string]AccessibilityDeclarationSpec{"IPAD": {SupportsVoiceover: &falseValue}}, true},
		{"state changed", map[string]AccessibilityDeclarationSpec{"IPHONE": {State: &published}}, true},
		{"new empty family", map[string]AccessibilityDeclarationSpec{"MAC": {}}, true},
		{"new explicit false", map[string]AccessibilityDeclarationSpec{"MAC": {SupportsVoiceover: &falseValue}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := &State{Spec: StateSpec{AccessibilityDeclarations: &AccessibilityDeclarationsSpec{Families: tc.families}}}
			if got := ValidateAccessibilityIntent("state.yaml", desired, live); (len(got) > 0) != tc.want {
				t.Fatalf("diagnostics=%+v want=%v", got, tc.want)
			}
		})
	}
}

func TestF4A_AccessibilityYAMLRoundTripKeepsFalseDistinctFromOmitted(t *testing.T) {
	falseValue := false
	want := &State{APIVersion: "flightline.dev/v1alpha1", Kind: "AppState", Metadata: StateMetadata{BundleID: "com.example.app", Version: "1.0"}, Spec: StateSpec{AccessibilityDeclarations: &AccessibilityDeclarationsSpec{Families: map[string]AccessibilityDeclarationSpec{
		"IPHONE": {SupportsVoiceover: &falseValue},
	}}}}
	buf, err := yaml.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got State
	if err := yaml.Unmarshal(buf, &got); err != nil {
		t.Fatal(err)
	}
	answers := got.Spec.AccessibilityDeclarations.Families["IPHONE"]
	if answers.SupportsVoiceover == nil || *answers.SupportsVoiceover || answers.SupportsCaptions != nil {
		t.Fatalf("round trip lost false/omission: %+v", answers)
	}
	if diags := Validate("state.yaml", &got); len(diags) > 0 {
		t.Fatalf("schema rejected round trip: %+v", diags)
	}
}
