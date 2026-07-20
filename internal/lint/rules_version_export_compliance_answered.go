package lint

import (
	"fmt"

	"github.com/ul0gic/flightline/internal/asc"
)

// versionExportComplianceAnsweredRule fires when usesNonExemptEncryption is unanswered.
// Apple blocks submission until it is set (via ITSAppUsesNonExemptEncryption in Info.plist or per-version). Mode=Both.
type versionExportComplianceAnsweredRule struct{}

func init() { Register(versionExportComplianceAnsweredRule{}) }

func (versionExportComplianceAnsweredRule) ID() string         { return "version.export-compliance-answered" }
func (versionExportComplianceAnsweredRule) Severity() Severity { return SeverityError }
func (versionExportComplianceAnsweredRule) Mode() Mode         { return ModeBoth }
func (versionExportComplianceAnsweredRule) Doc() string {
	return "Checks that the export-compliance encryption question (usesNonExemptEncryption) has an answer on record. " +
		"Apple blocks submission until this declaration is set, either per-version in App Store Connect or via ITSAppUsesNonExemptEncryption in the build's Info.plist, and forgetting it is one of the most common blocks for a new build. " +
		"Fix it by setting usesNonExemptEncryption to false (correct for most apps, since standard HTTPS is exempt) or to true with the supplemental declaration."
}

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
	// Nil ExportCompliance means the user relies on Info.plist; only fire when managed but unset.
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
				"or `true` plus a declaration block. See https://flightline.dev/docs/reference/state-yaml#specexportcompliance.",
			Reference: publicRuleReference(r.ID()),
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
		// build.attached-and-valid owns the no-build diagnostic; skip here.
		return nil
	}
	if resp.Data.Attributes.UsesNonExemptEncryption == nil {
		return []Diagnostic{{
			RuleID:    r.ID(),
			Severity:  SeverityError,
			Message:   "the build attached to this version has not declared usesNonExemptEncryption",
			Path:      "/spec/exportCompliance/usesNonExemptEncryption",
			FixHint:   fmt.Sprintf("answer the export-compliance question in App Store Connect, or run `flightline export-compliance set %s --version %s --uses-encryption false`", ctx.BundleID, ctx.Version),
			Reference: publicRuleReference(r.ID()),
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
