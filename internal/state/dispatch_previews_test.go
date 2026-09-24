package state

import (
	"errors"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestF6A_ValidatePreviewChangePathAndCompleteSet(t *testing.T) {
	ch := plan.Change{Op: plan.OpCreate, Path: "/spec/previews/locales/en-US/IPHONE_67", To: []config.PreviewFile{{Path: "one.mov"}}}
	if err := ValidatePreviewChange(ch); err != nil {
		t.Fatal(err)
	}
	ch.Path = "/spec/previews/locales/en-US/UNKNOWN"
	if err := ValidatePreviewChange(ch); !errors.Is(err, ErrUnmappedChange) {
		t.Fatalf("err=%v", err)
	}
	ch.Path = "/spec/customProductPages/Page/localizations/en-US/previews/IPHONE_67"
	if err := ValidatePreviewChange(ch); err != nil {
		t.Fatal(err)
	}
	ch.Path = "/spec/customProductPages/A~1B~0C/localizations/en-US/previews/IPHONE_67"
	if err := ValidatePreviewChange(ch); err != nil {
		t.Fatal(err)
	}
	target, err := parsePreviewChangePath(ch.Path)
	if err != nil || target.page != "A/B~C" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	ch.To = "one.mov"
	if err := ValidatePreviewChange(ch); err == nil {
		t.Fatal("scalar target accepted")
	}
}

func TestF6A_PreviewMutationPreflightAndExpectedState(t *testing.T) {
	current := []asc.AppPreview{{ID: "P1", FileName: "one.mov", SourceFileChecksum: "AAA", PreviewFrameTimeCode: "00:00:01.000"}}
	frame := "00:00:01.000"
	before := []config.PreviewFile{{Path: "one.mov", SourceFileChecksum: "AAA", PreviewFrameTimeCode: &frame}}
	if err := verifyPreviewObserved(before, current); err != nil {
		t.Fatal(err)
	}
	if err := verifyPreviewObserved([]config.PreviewFile{{Path: "one.mov", SourceFileChecksum: "BBB"}}, current); err == nil {
		t.Fatal("stale prior checksum accepted")
	}
	if _, _, err := preparePreviewMutation(t.TempDir(), []config.PreviewFile{{Path: "missing.mov", SourceFileChecksum: "BBB"}}, current); err == nil {
		t.Fatal("missing source accepted")
	}
	prepared, stale, err := preparePreviewMutation(t.TempDir(), []config.PreviewFile{{Path: "one.mov", SourceFileChecksum: "AAA"}}, current)
	if err != nil || len(prepared) != 1 || prepared[0].existingID != "P1" || len(stale) != 0 {
		t.Fatalf("prepared=%+v stale=%+v err=%v", prepared, stale, err)
	}
	if _, _, err := preparePreviewMutation(t.TempDir(), []config.PreviewFile{{Path: "one.mov"}}, current); err == nil {
		t.Fatal("filename hid missing desired checksum")
	}
}
