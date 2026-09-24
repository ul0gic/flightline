package config

import "strings"

// ValidateExportIntent rejects declaration intent that cannot use ASC's supported create and build-association lifecycle.
func ValidateExportIntent(file string, desired, _ *State) []Diagnostic {
	if desired == nil || desired.Spec.ExportCompliance == nil || desired.Spec.ExportCompliance.Declaration == nil {
		return nil
	}

	declaration := desired.Spec.ExportCompliance.Declaration
	diags := make([]Diagnostic, 0, 10)
	appendError := func(path, message string) {
		diags = append(diags, Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message})
	}
	if desired.Spec.Build == nil || strings.TrimSpace(desired.Spec.Build.Number) == "" {
		appendError("/spec/build/number", "export declaration requires an explicit build number so Flightline can verify and associate the declaration")
	}
	if declaration.AppDescription == nil {
		appendError("/spec/exportCompliance/declaration/appDescription", "appDescription is required to create an App Encryption Declaration")
	}
	if declaration.ContainsProprietaryCryptography == nil {
		appendError("/spec/exportCompliance/declaration/containsProprietaryCryptography", "containsProprietaryCryptography is required to create an App Encryption Declaration")
	}
	if declaration.ContainsThirdPartyCryptography == nil {
		appendError("/spec/exportCompliance/declaration/containsThirdPartyCryptography", "containsThirdPartyCryptography is required to create an App Encryption Declaration")
	}
	if declaration.AvailableOnFrenchStore == nil {
		appendError("/spec/exportCompliance/declaration/availableOnFrenchStore", "availableOnFrenchStore is required to create an App Encryption Declaration")
	}
	if declaration.UsesEncryption != nil {
		appendError("/spec/exportCompliance/declaration/usesEncryption", "usesEncryption is unsupported for declaration creation; manage the build-level usesNonExemptEncryption answer instead")
	}
	if declaration.Exempt != nil {
		appendError("/spec/exportCompliance/declaration/exempt", "exempt is a legacy declaration attribute and cannot be managed through the supported create lifecycle")
	}
	if declaration.ECCN != nil {
		appendError("/spec/exportCompliance/declaration/eccn", "eccn is Apple-assigned classification and cannot be supplied when creating a declaration")
	}
	if declaration.DocumentName != nil {
		appendError("/spec/exportCompliance/declaration/documentName", "documentName belongs to the declaration-document upload lifecycle, which is not managed by this declaration intent")
	}
	if declaration.DocumentURL != nil {
		appendError("/spec/exportCompliance/declaration/documentUrl", "documentUrl belongs to the declaration-document upload lifecycle, which is not managed by this declaration intent")
	}
	return diags
}
