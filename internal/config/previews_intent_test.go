package config

import "testing"

func TestF6A_PreviousIntentRejectsAmbiguousAndInvalidSets(t *testing.T) {
	state := &State{Spec: StateSpec{Previews: &PreviewsSpec{Locales: map[string]map[string][]PreviewFile{
		"en-US": {"IPHONE_67": {{Path: "clips/a.mov", SourceFileChecksum: "ABC"}, {Path: "clips/a.mov", SourceFileChecksum: "DEF"}}},
	}}}}
	if diagnostics := ValidatePreviewsIntent("state.yaml", state, nil); len(diagnostics) == 0 {
		t.Fatal("duplicate filename accepted")
	}
	state.Spec.Previews.Locales["en-US"]["IPHONE_67"] = []PreviewFile{{Path: "a.mov", SourceFileChecksum: "ABC"}, {Path: "b.mov", SourceFileChecksum: "ABC"}}
	if diagnostics := ValidatePreviewsIntent("state.yaml", state, nil); len(diagnostics) == 0 {
		t.Fatal("duplicate checksum accepted")
	}
	state.Spec.Previews.Locales["en-US"] = map[string][]PreviewFile{"APP_IPHONE_67": {{Path: "a.mov"}}}
	if diagnostics := ValidatePreviewsIntent("state.yaml", state, nil); len(diagnostics) == 0 {
		t.Fatal("invalid preview type accepted")
	}
}
