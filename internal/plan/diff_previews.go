package plan

import (
	"fmt"
	"strings"

	"github.com/ul0gic/flightline/internal/config"
)

// diffPreviews emits one complete-set change per explicitly managed preview type.
func diffPreviews(desired, live *config.State, out *[]Change) {
	if desired == nil {
		return
	}
	if desired.Spec.Previews != nil {
		var observed map[string]map[string][]config.PreviewFile
		if live != nil && live.Spec.Previews != nil {
			observed = live.Spec.Previews.Locales
		}
		for _, locale := range sortedKeys(desired.Spec.Previews.Locales) {
			for _, previewType := range sortedKeys(desired.Spec.Previews.Locales[locale]) {
				appendPreviewChange(out, "/spec/previews/locales/"+escapeTestFlightPathSegment(locale)+"/"+previewType, "previews."+locale+"."+previewType,
					desired.Spec.Previews.Locales[locale][previewType], observed[locale][previewType])
			}
		}
	}
	if desired.Spec.CustomProductPages == nil {
		return
	}
	for _, name := range sortedKeys(*desired.Spec.CustomProductPages) {
		page := (*desired.Spec.CustomProductPages)[name]
		var current config.CustomProductPage
		if live != nil && live.Spec.CustomProductPages != nil {
			current = (*live.Spec.CustomProductPages)[name]
		}
		for _, locale := range sortedKeys(page.Localizations) {
			wanted := page.Localizations[locale]
			observed := current.Localizations[locale]
			for _, previewType := range sortedKeys(wanted.Previews) {
				appendPreviewChange(out, "/spec/customProductPages/"+escapeTestFlightPathSegment(name)+"/localizations/"+escapeTestFlightPathSegment(locale)+"/previews/"+previewType,
					"customProductPages."+name+".previews."+locale+"."+previewType, wanted.Previews[previewType], observed.Previews[previewType])
			}
		}
	}
}

func appendPreviewChange(out *[]Change, path, resource string, desired, live []config.PreviewFile) {
	merged := mergePreviewFrames(desired, live)
	if equalPreviewFiles(merged, live) {
		return
	}
	op := OpUpdate
	if len(live) == 0 {
		op = OpCreate
	}
	*out = append(*out, Change{Op: op, Resource: resource, Path: path, From: live, To: merged,
		Hint: fmt.Sprintf("reconcile %d preview(s)", len(merged))})
}

func mergePreviewFrames(desired, live []config.PreviewFile) []config.PreviewFile {
	out := make([]config.PreviewFile, len(desired))
	copy(out, desired)
	for index := range out {
		if out[index].PreviewFrameTimeCode != nil {
			continue
		}
		for old := range live {
			if previewFileIdentity(out[index], live[old]) {
				out[index].PreviewFrameTimeCode = live[old].PreviewFrameTimeCode
				break
			}
		}
	}
	return out
}

func equalPreviewFiles(desired, live []config.PreviewFile) bool {
	if len(desired) != len(live) {
		return false
	}
	matched := make([]bool, len(live))
	for index := range desired {
		found := false
		for old := range live {
			if matched[old] || !previewFileIdentity(desired[index], live[old]) || !equalOptionalFrame(desired[index].PreviewFrameTimeCode, live[old].PreviewFrameTimeCode) {
				continue
			}
			matched[old], found = true, true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func previewFileIdentity(desired, live config.PreviewFile) bool {
	if desired.SourceFileChecksum != "" || live.SourceFileChecksum != "" {
		return strings.EqualFold(desired.SourceFileChecksum, live.SourceFileChecksum)
	}
	return desired.Path != "" && desired.Path == live.Path
}

func equalOptionalFrame(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
