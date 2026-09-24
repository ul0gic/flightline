package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestG2_NewBuildReconcilesItsOwnExportState(t *testing.T) {
	no := false
	description := "App encryption description"
	export := &config.ExportComplianceSpec{
		UsesNonExemptEncryption: &no,
		Declaration: &config.ExportComplianceDeclaration{
			AppDescription:                  &description,
			ContainsProprietaryCryptography: &no,
			ContainsThirdPartyCryptography:  &no,
			AvailableOnFrenchStore:          &no,
		},
	}
	live := &config.State{Spec: config.StateSpec{Build: &config.BuildSpec{Number: "1"}, ExportCompliance: export}}
	desired := &config.State{Spec: config.StateSpec{Build: &config.BuildSpec{Number: "2"}, ExportCompliance: export}}
	changes := Diff(desired, live)
	paths := make(map[string]bool)
	for _, change := range changes {
		paths[change.Path] = true
	}
	for _, path := range []string{"/spec/build/number", "/spec/exportCompliance/usesNonExemptEncryption", "/spec/exportCompliance/declaration"} {
		if !paths[path] {
			t.Errorf("missing new-build reconciliation at %s: %+v", path, changes)
		}
	}
}
