package lint

import (
	"fmt"

	"github.com/ul0gic/skipper/internal/asc"
)

// versionExportComplianceAnsweredRule fires when the version's
// usesNonExemptEncryption answer is missing. Apple blocks submission until
// it's set; the build itself can carry the answer (ITSAppUsesNonExemptEncryption
// in Info.plist) or it can be set per-version. Skipper checks the spec
// answer (offline) and the live build attribute (live).
//
// Mode: Both. Offline checks State.Spec.ExportCompliance.UsesNonExemptEncryption.
// Live re-fetches the build attached to the version and reads its
// usesNonExemptEncryption.
type versionExportComplianceAnsweredRule struct{}

func init() { Register(versionExportComplianceAnsweredRule{}) }

func (versionExportComplianceAnsweredRule) ID() string         { return "version.export-compliance-answered" }
func (versionExportComplianceAnsweredRule) Severity() Severity { return SeverityError }
func (versionExportComplianceAnsweredRule) Mode() Mode         { return ModeBoth }

func (r versionExportComplianceAnsweredRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.Live {
		return r.checkLive(ctx)
	}
	return r.checkOffline(ctx)
}

func (r versionExportComplianceAnsweredRule) checkOffline(ctx CheckContext) []Diagnostic {
	if ctx.State == nil {
		return nil
	}
	ec := ctx.State.Spec.ExportCompliance
	// "Not managed" (nil ExportCompliance) is intentionally NOT flagged
	// here — the user may rely on the build's Info.plist answer. Only
	// when ExportCompliance IS managed but the answer is unset do we fire.
	if ec == nil {
		return nil
	}
	if ec.UsesNonExemptEncryption == nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  "spec.exportCompliance is set but usesNonExemptEncryption is nil",
			Path:     "/spec/exportCompliance/usesNonExemptEncryption",
			FixHint: "set the answer: `spec.exportCompliance.usesNonExemptEncryption: false` (most apps) " +
				"or `true` plus a declaration block. See docs/state-yaml.md#spec-exportcompliance.",
			Reference: "PRD §L3 — version.export-compliance-answered",
		}}
	}
	return nil
}

func (r versionExportComplianceAnsweredRule) checkLive(ctx CheckContext) []Diagnostic {
	if ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}
	versionID, err := r.resolveVersionID(ctx)
	if err != nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("resolve version %q: %v", ctx.Version, err),
			FixHint:  "verify --version matches an existing App Store version on the app.",
		}}
	}
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](
		ctx.Ctx, ctx.Client, "/v1/appStoreVersions/"+versionID+"/build", nil,
	)
	if err != nil {
		// No attached build is also a problem, but the rules_build_attached_and_valid
		// rule owns that diagnostic. Here we only care whether the answer is set.
		return nil
	}
	if resp.Data.Attributes.UsesNonExemptEncryption == nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  "the build attached to this version has not declared usesNonExemptEncryption",
			Path:     "/spec/exportCompliance/usesNonExemptEncryption",
			FixHint: "answer the export-compliance question in App Store Connect, or " +
				"`skipper export-compliance set <bundleId> --version <v> --uses-non-exempt-encryption=false`.",
			Reference: "PRD §L3 — version.export-compliance-answered",
		}}
	}
	return nil
}

func (versionExportComplianceAnsweredRule) resolveVersionID(ctx CheckContext) (string, error) {
	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return "", err
	}
	return resolveVersionIDOnApp(ctx, appID, ctx.Version)
}
