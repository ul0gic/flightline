package config

import (
	"fmt"
	"strings"
)

// ValidateRightsIntent checks only authored legal declarations and EULA scope.
func ValidateRightsIntent(file string, desired, live *State) []Diagnostic {
	if desired == nil {
		return nil
	}
	var out []Diagnostic
	if rights := desired.Spec.ContentRights; rights != nil &&
		*rights != "DOES_NOT_USE_THIRD_PARTY_CONTENT" && *rights != "USES_THIRD_PARTY_CONTENT" {
		out = append(out, rightsDiagnostic(file, "/spec/contentRights", "content rights requires an explicit supported declaration"))
	}
	eula := desired.Spec.AppEULA
	if eula == nil {
		return out
	}
	return append(out, validateAppEULAIntent(file, eula, live)...)
}

func validateAppEULAIntent(file string, eula *AppEULASpec, live *State) []Diagnostic {
	var out []Diagnostic
	if live == nil || live.Spec.AppEULA == nil {
		if eula.AgreementText == nil || eula.Territories == nil {
			out = append(out, rightsDiagnostic(file, "/spec/appEula", "creating a custom EULA requires agreementText and territories"))
		}
	}
	if eula.AgreementText != nil && strings.TrimSpace(*eula.AgreementText) == "" &&
		(live == nil || live.Spec.AppEULA == nil || live.Spec.AppEULA.AgreementText == nil || *live.Spec.AppEULA.AgreementText != *eula.AgreementText) {
		out = append(out, rightsDiagnostic(file, "/spec/appEula/agreementText", "agreementText must be nonempty"))
	}
	out = append(out, validateAppEULATerritories(file, eula.Territories, live)...)
	return out
}

func validateAppEULATerritories(file string, territories *[]string, live *State) []Diagnostic {
	if territories == nil {
		return nil
	}
	var out []Diagnostic
	if len(*territories) == 0 && (live == nil || live.Spec.AppEULA == nil || live.Spec.AppEULA.Territories == nil || len(*live.Spec.AppEULA.Territories) != 0) {
		out = append(out, rightsDiagnostic(file, "/spec/appEula/territories", "custom EULA requires at least one territory"))
	}
	seen := make(map[string]bool, len(*territories))
	for i, territory := range *territories {
		path := fmt.Sprintf("/spec/appEula/territories/%d", i)
		if strings.TrimSpace(territory) == "" {
			out = append(out, rightsDiagnostic(file, path, "territory ID is required"))
		} else if seen[territory] {
			out = append(out, rightsDiagnostic(file, path, "duplicate territory ID"))
		}
		seen[territory] = true
	}
	return out
}

func rightsDiagnostic(file, path, message string) Diagnostic {
	return Diagnostic{File: file, Path: path, Severity: SeverityError, Message: message}
}
