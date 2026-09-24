package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF6A_PreviewDiffOmissionRoundTripAndChangedBytes(t *testing.T) {
	frame := "00:00:01.000"
	observed := config.PreviewFile{Path: "one.mov", SourceFileChecksum: "AAA", PreviewFrameTimeCode: &frame}
	live := &config.State{Spec: config.StateSpec{Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {observed}}}}}}
	var changes []Change
	diffPreviews(&config.State{}, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("omission changed previews: %+v", changes)
	}
	diffPreviews(live, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("roundtrip changed previews: %+v", changes)
	}
	want := &config.State{Spec: config.StateSpec{Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {{Path: "one.mov", SourceFileChecksum: "BBB"}}}}}}}
	diffPreviews(want, live, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/previews/locales/en-US/IPHONE_67" {
		t.Fatalf("changed bytes not planned: %+v", changes)
	}
	to, ok := changes[0].To.([]config.PreviewFile)
	if !ok {
		t.Fatalf("unexpected target type %T", changes[0].To)
	}
	if len(to) != 1 || to[0].PreviewFrameTimeCode != nil {
		t.Fatalf("frame from different bytes inherited: %+v", to)
	}
}

func TestF6A_PreviewDiffChecksumDominatesFilenameAndFrameOmission(t *testing.T) {
	frame := "00:00:02.000"
	live := &config.State{Spec: config.StateSpec{Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {{Path: "one.mov", SourceFileChecksum: "AAA", PreviewFrameTimeCode: &frame}}}}}}}
	desired := &config.State{Spec: config.StateSpec{Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {{Path: "one.mov"}}}}}}}
	var changes []Change
	diffPreviews(desired, live, &changes)
	if len(changes) != 1 {
		t.Fatalf("missing desired checksum must not claim identity: %+v", changes)
	}
	changes = nil
	desired.Spec.Previews.Locales["en-US"]["IPHONE_67"][0].SourceFileChecksum = "AAA"
	diffPreviews(desired, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("omitted frame should preserve existing frame: %+v", changes)
	}
	newFrame := "00:00:03.000"
	desired.Spec.Previews.Locales["en-US"]["IPHONE_67"][0].PreviewFrameTimeCode = &newFrame
	diffPreviews(desired, live, &changes)
	if len(changes) != 1 {
		t.Fatalf("explicit frame update missing: %+v", changes)
	}
}

func TestF6A_PreviewDiffFilenameFallbackOnlyWithoutChecksums(t *testing.T) {
	live := &config.State{Spec: config.StateSpec{Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {{Path: "one.mov"}}}}}}}
	desired := &config.State{Spec: config.StateSpec{Previews: &config.PreviewsSpec{Locales: map[string]map[string][]config.PreviewFile{"en-US": {"IPHONE_67": {{Path: "one.mov"}}}}}}}
	var changes []Change
	diffPreviews(desired, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("unknown checksums with exact filename changed: %+v", changes)
	}
}

func TestF6A_CPPPreviewDiffEscapesPagePointer(t *testing.T) {
	desired := &config.State{Spec: config.StateSpec{CustomProductPages: &config.CustomProductPagesSpec{"A/B~C": {Localizations: map[string]config.CustomProductPageLocale{"en-US": {Previews: map[string][]config.PreviewFile{"IPHONE_67": {{Path: "one.mov"}}}}}}}}}
	var changes []Change
	diffPreviews(desired, nil, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/customProductPages/A~1B~0C/localizations/en-US/previews/IPHONE_67" {
		t.Fatalf("changes=%+v", changes)
	}
}
