package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
)

// ValidatePreviewsIntent checks only explicitly managed preview sets.
func ValidatePreviewsIntent(file string, desired, _ *State) []Diagnostic {
	if desired == nil {
		return nil
	}
	var diagnostics []Diagnostic
	if desired.Spec.Previews != nil {
		for locale, types := range desired.Spec.Previews.Locales {
			for previewType, files := range types {
				path := "/spec/previews/locales/" + locale + "/" + previewType
				diagnostics = append(diagnostics, validatePreviewFiles(file, path, locale, previewType, files)...)
			}
		}
	}
	if desired.Spec.CustomProductPages != nil {
		for name, page := range *desired.Spec.CustomProductPages {
			for locale, localization := range page.Localizations {
				for previewType, files := range localization.Previews {
					path := "/spec/customProductPages/" + name + "/localizations/" + locale + "/previews/" + previewType
					diagnostics = append(diagnostics, validatePreviewFiles(file, path, locale, previewType, files)...)
				}
			}
		}
	}
	return diagnostics
}

func validatePreviewFiles(file, path, locale, previewType string, files []PreviewFile) []Diagnostic {
	var diagnostics []Diagnostic
	if strings.TrimSpace(locale) == "" || strings.Contains(locale, "/") || !asc.ValidPreviewType(previewType) {
		diagnostics = append(diagnostics, previewDiagnostic(file, path, "preview locale or type is invalid"))
	}
	seenNames := make(map[string]bool)
	seenChecksums := make(map[string]bool)
	for index := range files {
		asset := &files[index]
		assetPath := fmt.Sprintf("%s/%d", path, index)
		if strings.TrimSpace(asset.Path) == "" {
			diagnostics = append(diagnostics, previewDiagnostic(file, assetPath+"/path", "preview path is required"))
		}
		name := filepath.Base(asset.Path)
		if seenNames[name] {
			diagnostics = append(diagnostics, previewDiagnostic(file, assetPath+"/path", "duplicate preview filename is ambiguous"))
		}
		seenNames[name] = true
		if asset.SourceFileChecksum != "" {
			checksum := strings.ToLower(asset.SourceFileChecksum)
			if seenChecksums[checksum] {
				diagnostics = append(diagnostics, previewDiagnostic(file, assetPath+"/sourceFileChecksum", "duplicate preview checksum is ambiguous"))
			}
			seenChecksums[checksum] = true
		}
	}
	return diagnostics
}

func previewDiagnostic(file, path, message string) Diagnostic {
	return Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message}
}
