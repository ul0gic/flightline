package lint

import (
	"fmt"
	"regexp"

	"github.com/ul0gic/flightline/internal/config"
)

// strictFormatEmailRule fires when a contact-email field fails a permissive local@domain.tld check.
// Reimplemented at lint layer because jsonschema/v6 skips format assertions by default. Warning-only; Apple validates on submit. Offline-only.
type strictFormatEmailRule struct{}

func init() { Register(strictFormatEmailRule{}) }

func (strictFormatEmailRule) ID() string         { return "strict.format-email" }
func (strictFormatEmailRule) Severity() Severity { return SeverityWarning }
func (strictFormatEmailRule) Mode() Mode         { return ModeOffline }

// permissiveEmailRE requires local@domain.tld: rejects "joe at example dot com", accepts plus-aliases and subdomains.
// Full RFC 5322 is impractical; Apple's own validator is the final authority.
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
			// strict.required-nonzero owns the empty-email diagnostic.
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
