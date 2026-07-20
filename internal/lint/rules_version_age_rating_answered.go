package lint

import (
	"fmt"
	"reflect"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// versionAgeRatingAnsweredRule fires when the age-rating questionnaire has unanswered prompts.
// Apple blocks "Submit for Review" until every frequency-enum and boolean field has a value. Mode=Both.
type versionAgeRatingAnsweredRule struct{}

func init() { Register(versionAgeRatingAnsweredRule{}) }

func (versionAgeRatingAnsweredRule) ID() string         { return "version.age-rating-answered" }
func (versionAgeRatingAnsweredRule) Severity() Severity { return SeverityError }
func (versionAgeRatingAnsweredRule) Mode() Mode         { return ModeBoth }
func (versionAgeRatingAnsweredRule) Doc() string {
	return "Checks that every prompt in the age-rating questionnaire has a value, across both the frequency-enum fields (violence, sexual content, profanity, and so on) and the boolean prompts (gambling, unrestricted web access). " +
		"A partially answered questionnaire shows up only as a soft block on the Submit for Review button, and Apple will not tell you which field is missing until you open the specific panel. " +
		"Fix it by giving every prompt a value; NONE for frequency fields and false for boolean prompts are valid answers meaning the content is absent. " +
		"Derived and optional fields (seventeenPlus, kidsAgeBand) are exempt: Apple computes seventeenPlus itself and kidsAgeBand only applies to Kids-category apps."
}

func (r versionAgeRatingAnsweredRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.Live {
		return r.checkLive(ctx)
	}
	return r.checkOffline(ctx)
}

func (r versionAgeRatingAnsweredRule) checkOffline(ctx CheckContext) []Diagnostic {
	if ctx.State == nil {
		return nil
	}
	ar := ctx.State.Spec.AgeRating
	if ar == nil {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  "spec.ageRating is missing: Apple requires every prompt to be answered",
			Path:     "/spec/ageRating",
			FixHint: "add the ageRating block; every frequency-enum and boolean prompt must " +
				"have a value. See https://flightline.dev/docs/reference/state-yaml#specagerating.",
			Reference: publicRuleReference(r.ID()),
		}}
	}
	missing := unansweredAgeRatingFields(ar)
	out := make([]Diagnostic, 0, len(missing))
	for _, field := range missing {
		out = append(out, Diagnostic{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("age-rating field %q is not answered", field),
			Path:     "/spec/ageRating/" + field,
			FixHint: "set every age-rating prompt to a value (NONE is valid for frequency " +
				"fields; false is valid for boolean prompts).",
			Reference: publicRuleReference(r.ID()),
		})
	}
	return out
}

func (r versionAgeRatingAnsweredRule) checkLive(ctx CheckContext) []Diagnostic {
	if ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}
	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve app", err)}
	}
	appInfoID, err := liveAppInfoID(ctx, appID)
	if err != nil || appInfoID == "" {
		return []Diagnostic{{
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  "could not locate an editable appInfo for age-rating check",
			FixHint:  "verify the app is in an editable state in App Store Connect.",
		}}
	}
	resp, err := asc.Get[asc.Single[asc.AgeRatingDeclarationAttributes]](
		ctx.Ctx, ctx.Client, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil,
	)
	if err != nil {
		return []Diagnostic{r.fetchErr("fetch ageRatingDeclaration", err)}
	}
	missing := unansweredLiveAgeRatingFields(resp.Data.Attributes)
	out := make([]Diagnostic, 0, len(missing))
	for _, field := range missing {
		out = append(out, Diagnostic{
			RuleID:    r.ID(),
			Severity:  SeverityError,
			Message:   fmt.Sprintf("live age-rating field %q is not answered", field),
			Path:      "/spec/ageRating/" + field,
			FixHint:   fmt.Sprintf("answer the prompt in App Store Connect or run `flightline age-rating set %s --version %s --from age.yaml`", ctx.BundleID, ctx.Version),
			Reference: publicRuleReference(r.ID()),
		})
	}
	return out
}

func (r versionAgeRatingAnsweredRule) fetchErr(what string, err error) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: %v", what, err),
		FixHint:  "rerun preflight; if it persists check ASC API access.",
	}
}

// notAnswerable: nil is a valid answer here — Apple derives, defaults, or conditionally scopes these,
// and demanding them recreates the lint/apply catch-22 (apply rejects seventeenPlus as read-only).
var notAnswerable = map[string]struct{}{
	"seventeenPlus":            {},
	"kidsAgeBand":              {},
	"socialMediaAgeRestricted": {},
	"gamblingSimulated":        {},
	"gunsOrOtherWeapons":       {},
	"advertising":              {},
	"ageAssurance":             {},
	"healthOrWellnessTopics":   {},
	"lootBox":                  {},
	"messagingAndChat":         {},
	"parentalControls":         {},
	"userGeneratedContent":     {},
}

// unansweredAgeRatingFields returns names of nil pointer fields; uses reflection so new schema fields are automatic.
// Fields are pointers precisely to distinguish nil (unanswered) from empty string (wrong answer).
func unansweredAgeRatingFields(ar *config.AgeRatingSpec) []string {
	if ar == nil {
		return nil
	}
	v := reflect.ValueOf(*ar)
	t := v.Type()
	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		name := jsonTagName(f.Tag.Get("json"))
		if name == "" || name == "-" {
			continue
		}
		if _, skip := notAnswerable[name]; skip {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer && fv.IsNil() {
			out = append(out, name)
		}
	}
	if ar.SocialMedia != nil && *ar.SocialMedia && ar.SocialMediaAgeRestricted == nil {
		out = append(out, "socialMediaAgeRestricted")
	}
	return out
}

// unansweredLiveAgeRatingFields checks the wire shape: enum fields are empty strings, bool fields are nil *bool.
// Uses schema-side names so Path matches the offline diagnostic. Duplicates projectAgeRating's mapping: lint is independent of internal/state.
func unansweredLiveAgeRatingFields(a asc.AgeRatingDeclarationAttributes) []string {
	missing := make([]string, 0, 16)

	checkStr := func(name, val string) {
		if val == "" {
			missing = append(missing, name)
		}
	}
	checkBool := func(name string, b *bool) {
		if b == nil {
			missing = append(missing, name)
		}
	}

	checkStr("cartoonOrFantasyViolence", a.ViolenceCartoonOrFantasy)
	checkStr("realisticViolence", a.ViolenceRealistic)
	checkStr("profanityOrCrudeHumor", a.ProfanityOrCrudeHumor)
	checkStr("matureSuggestiveThemes", a.MatureOrSuggestiveThemes)
	checkStr("horrorOrFearThemes", a.HorrorOrFearThemes)
	checkStr("medicalOrTreatmentInformation", a.MedicalOrTreatmentInformation)
	checkStr("alcoholTobaccoOrDrugUseOrReferences", a.AlcoholTobaccoOrDrugUseOrReferences)
	checkStr("contestsAndGambling", a.Contests)
	checkStr("sexualContentOrNudity", a.SexualContentOrNudity)
	checkStr("sexualContentGraphicAndNudity", a.SexualContentGraphicAndNudity)
	checkBool("gambling", a.Gambling)
	checkBool("unrestrictedWebAccess", a.UnrestrictedWebAccess)
	// Required for all submissions from 2026-09-07 (Apple's social-media questionnaire addition, July 2026).
	checkBool("socialMedia", a.SocialMedia)
	if a.SocialMedia != nil && *a.SocialMedia && a.SocialMediaAgeRestricted == nil {
		missing = append(missing, "socialMediaAgeRestricted")
	}

	return missing
}

// jsonTagName extracts the field name from a `json:"name,omitempty"` tag.
func jsonTagName(tag string) string {
	if tag == "" {
		return ""
	}
	for i, c := range tag {
		if c == ',' {
			return tag[:i]
		}
	}
	return tag
}

// liveAppInfoID returns an editable appInfo ID for the app. Mirrors state.fetchEditableAppInfo.
func liveAppInfoID(ctx CheckContext, appID string) (string, error) {
	page, err := asc.Get[asc.Collection[asc.AppInfoAttributes]](
		ctx.Ctx, ctx.Client, "/v1/apps/"+appID+"/appInfos", nil,
	)
	if err != nil {
		return "", err
	}
	for _, r := range page.Data {
		switch r.Attributes.State {
		case "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED",
			"METADATA_REJECTED", "WAITING_FOR_REVIEW", "IN_REVIEW":
			return r.ID, nil
		}
	}
	if len(page.Data) > 0 {
		return page.Data[0].ID, nil
	}
	return "", nil
}
