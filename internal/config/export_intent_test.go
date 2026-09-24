package config

import "testing"

func TestC2C_ValidateExportIntent(t *testing.T) {
	str := func(v string) *string { return &v }
	boolPtr := func(v bool) *bool { return &v }
	complete := func() *ExportComplianceDeclaration {
		return &ExportComplianceDeclaration{
			AppDescription:                  str("Uses TLS"),
			ContainsProprietaryCryptography: boolPtr(true),
			ContainsThirdPartyCryptography:  boolPtr(false),
			AvailableOnFrenchStore:          boolPtr(true),
		}
	}
	for _, tc := range []struct {
		name     string
		state    *State
		wantPath string
	}{
		{"complete", &State{Spec: StateSpec{Build: &BuildSpec{Number: "42"}, ExportCompliance: &ExportComplianceSpec{Declaration: complete()}}}, ""},
		{"missing build", &State{Spec: StateSpec{ExportCompliance: &ExportComplianceSpec{Declaration: complete()}}}, "/spec/build/number"},
		{"missing required field", &State{Spec: StateSpec{Build: &BuildSpec{Number: "42"}, ExportCompliance: &ExportComplianceSpec{Declaration: &ExportComplianceDeclaration{}}}}, "/spec/exportCompliance/declaration/appDescription"},
		{"legacy uses encryption", &State{Spec: StateSpec{Build: &BuildSpec{Number: "42"}, ExportCompliance: &ExportComplianceSpec{Declaration: func() *ExportComplianceDeclaration { d := complete(); d.UsesEncryption = boolPtr(true); return d }()}}}, "/spec/exportCompliance/declaration/usesEncryption"},
		{"legacy exempt", &State{Spec: StateSpec{Build: &BuildSpec{Number: "42"}, ExportCompliance: &ExportComplianceSpec{Declaration: func() *ExportComplianceDeclaration { d := complete(); d.Exempt = boolPtr(true); return d }()}}}, "/spec/exportCompliance/declaration/exempt"},
		{"apple assigned eccn", &State{Spec: StateSpec{Build: &BuildSpec{Number: "42"}, ExportCompliance: &ExportComplianceSpec{Declaration: func() *ExportComplianceDeclaration { d := complete(); d.ECCN = str("5D002"); return d }()}}}, "/spec/exportCompliance/declaration/eccn"},
		{"document lifecycle", &State{Spec: StateSpec{Build: &BuildSpec{Number: "42"}, ExportCompliance: &ExportComplianceSpec{Declaration: func() *ExportComplianceDeclaration {
			d := complete()
			d.DocumentName = str("e.pdf")
			d.DocumentURL = str("https://example.test/e.pdf")
			return d
		}()}}}, "/spec/exportCompliance/declaration/documentName"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := ValidateExportIntent("state.yaml", tc.state, nil)
			if tc.wantPath == "" {
				if len(diags) != 0 {
					t.Fatalf("diagnostics=%+v", diags)
				}
				return
			}
			for _, diagnostic := range diags {
				if diagnostic.Path == tc.wantPath {
					return
				}
			}
			t.Fatalf("missing diagnostic path %q in %+v", tc.wantPath, diags)
		})
	}
}
