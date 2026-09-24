package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestC2C_DiffExportDeclarationIsOneSemanticChange(t *testing.T) {
	str := func(v string) *string { return &v }
	boolPtr := func(v bool) *bool { return &v }
	declaration := func(description string) *config.ExportComplianceDeclaration {
		return &config.ExportComplianceDeclaration{
			AppDescription:                  str(description),
			ContainsProprietaryCryptography: boolPtr(true),
			ContainsThirdPartyCryptography:  boolPtr(false),
			AvailableOnFrenchStore:          boolPtr(true),
		}
	}
	desired := &config.State{Spec: config.StateSpec{ExportCompliance: &config.ExportComplianceSpec{Declaration: declaration("Uses TLS")}}}
	if got := Diff(desired, &config.State{Spec: config.StateSpec{ExportCompliance: &config.ExportComplianceSpec{Declaration: declaration("Uses TLS")}}}); len(got) != 0 {
		t.Fatalf("identical declaration diff=%+v", got)
	}
	got := Diff(desired, &config.State{Spec: config.StateSpec{ExportCompliance: &config.ExportComplianceSpec{Declaration: declaration("Uses HTTPS")}}})
	if len(got) != 1 || got[0].Op != OpCreate || got[0].Path != "/spec/exportCompliance/declaration" {
		t.Fatalf("changes=%+v", got)
	}
	if _, ok := got[0].To.(config.ExportComplianceDeclaration); !ok {
		t.Fatalf("semantic payload type=%T", got[0].To)
	}
}
