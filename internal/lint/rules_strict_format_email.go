package lint

import (
	"fmt"
	"regexp"

	"github.com/ul0gic/skipper/internal/config"
)

// strictFormatEmailRule fires when a contact-email field carries a value
// that doesn't even match a permissive RFC 5322 simple-form regex
// (`local@domain.tld`). The schema declares `format: email` on
// `spec.reviewerDemo.contactEmail` and `spec.testflight.groups.*.testers[].email`,
// but santhosh-tekuri/jsonschema/v6 doesn't enforce format keywords by
// default — so a value like `joe at example dot com` slips through.
//
// We re-implement the check at the lint layer rather than turning on
// jsonschema's format assertions globally because format coverage is a
// gradual upgrade: enabling it package-wide would suddenly reject things
// the loader has been tolerating, breaking existing state.yaml files. The
// lint rule fires at preflight time, with a clear message and a diagnostic
// the user can act on.
//
// Severity Warning rather than Error: the schema spec says format is
// "advisory" in draft 2020-12. We surface the failure but don't gate
// preflight on it (Apple's contact-email validator catches it on submit).
//
// Offline-only.
type strictFormatEmailRule struct{}

func init() { Register(strictFormatEmailRule{}) }

func (strictFormatEmailRule) ID() string         { return "strict.format-email" }
func (strictFormatEmailRule) Severity() Severity { return SeverityWarning }
func (strictFormatEmailRule) Mode() Mode         { return ModeOffline }

// permissiveEmailRE is intentionally relaxed — RFC 5322 in full is huge and
// most libraries miss edge cases anyway. We require:
//   - at least one local-part char (no spaces),
//   - exactly one @,
//   - at least one domain-label char,
//   - at least one ".",
//   - at least one TLD char.
//
// This rejects "joe at example dot com" and "no-at-sign" while accepting
// realistic addresses including plus-aliases and subdomains.
var permissiveEmailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (r strictFormatEmailRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.State == nil {
		return nil
	}
	out := make([]Diagnostic, 0)

	if rd := ctx.State.Spec.ReviewerDemo; rd != nil && rd.ContactEmail != nil {
		if !permissiveEmailRE.MatchString(*rd.ContactEmail) {
			out = append(out, Diagnostic{
				RuleID:    r.ID(),
				Severity:  SeverityWarning,
				Message:   fmt.Sprintf("spec.reviewerDemo.contactEmail %q does not look like an email", *rd.ContactEmail),
				Path:      "/spec/reviewerDemo/contactEmail",
				FixHint:   "use a real email address: local@domain.tld. Apple uses this to contact you about review issues.",
				Reference: "QA-011 (resolved via this rule); schema format: email",
			})
		}
	}

	if tf := ctx.State.Spec.TestFlight; tf != nil {
		for groupName, group := range tf.Groups {
			r.scanTesters(groupName, group, &out)
		}
	}
	return out
}

func (r strictFormatEmailRule) scanTesters(groupName string, group config.TestFlightGroup, out *[]Diagnostic) {
	for idx, t := range group.Testers {
		if t.Email == "" {
			// strict.required-nonzero handles the empty case; don't
			// double-report.
			continue
		}
		if permissiveEmailRE.MatchString(t.Email) {
			continue
		}
		*out = append(*out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"testflight group %q tester[%d] email %q does not look like an email",
				groupName, idx, t.Email,
			),
			Path:      fmt.Sprintf("/spec/testflight/groups/%s/testers/%d/email", groupName, idx),
			FixHint:   "use a real email address; Apple's invite system rejects non-RFC-5322 addresses on submit.",
			Reference: "QA-011 (resolved via this rule); schema format: email",
		})
	}
}
