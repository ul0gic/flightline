package lint

import (
	"fmt"
	"reflect"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// versionAgeRatingAnsweredRule fires when the age-rating questionnaire is
// not fully answered. Apple's submission flow blocks until every prompt has
// a value; partially-answered questionnaires surface as a soft block on the
// version's "Submit for Review" button.
//
// Offline: scans State.Spec.AgeRating for the *required* fields that map to
// the schema (every frequency-enum field plus the boolean prompts). A
// pointer that is nil means "not answered". Pointer-to-empty-string is
// treated as not answered too (the schema's enum constraint will catch it
// separately, but the rule's job here is rejection prevention not schema
// validation).
//
// Live: re-fetches the AgeRatingDeclaration via the editable AppInfo and
// checks the same fields against Apple's wire shape. Apple's enum-valued
// fields are plain strings (no nil); empty == not answered.
//
// Mode=Both.
type versionAgeRatingAnsweredRule struct{}

func init() { Register(versionAgeRatingAnsweredRule{}) }

func (versionAgeRatingAnsweredRule) ID() string         { return "version.age-rating-answered" }
func (versionAgeRatingAnsweredRule) Severity() Severity { return SeverityError }
func (versionAgeRatingAnsweredRule) Mode() Mode         { return ModeBoth }

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
			Message:  "spec.ageRating is missing — Apple requires every prompt to be answered",
			Path:     "/spec/ageRating",
			FixHint: "add the ageRating block; every frequency-enum and boolean prompt must " +
				"have a value. See docs/state-yaml.md#spec-agerating.",
			Reference: "PRD §L3 — version.age-rating-answered",
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
			Reference: "PRD §L3 — version.age-rating-answered",
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
			RuleID:   r.ID(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("live age-rating field %q is not answered", field),
			Path:     "/spec/ageRating/" + field,
			FixHint: "answer the prompt in App Store Connect or via " +
				"`fline age-rating set <bundleId> --version <v> --from age.yaml`.",
			Reference: "PRD §L3 — version.age-rating-answered",
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

// unansweredAgeRatingFields returns the schema field names whose pointer is
// nil. Frequency enums (string pointers) and boolean prompts (bool pointers)
// are both treated the same: "answered" means non-nil. The nil-vs-empty-
// string distinction is the whole reason these fields are pointers.
//
// Reflection beats writing a 25-line nil check by hand — and survives
// schema additions without manual edits.
func unansweredAgeRatingFields(ar *config.AgeRatingSpec) []string {
	if ar == nil {
		return nil
	}
	v := reflect.ValueOf(*ar)
	t := v.Type()
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Use the JSON tag (which matches the schema field name) so the
		// diagnostic Path is wire-shape-stable.
		name := jsonTagName(f.Tag.Get("json"))
		if name == "" || name == "-" {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer && fv.IsNil() {
			out = append(out, name)
		}
	}
	return out
}

// unansweredLiveAgeRatingFields walks the live wire shape. Apple's enum-
// valued fields are plain strings (empty = unanswered) and boolean prompts
// are *bool. We track the schema-shape names rather than Apple's wire names
// so the Path matches the offline diagnostic.
func unansweredLiveAgeRatingFields(a asc.AgeRatingDeclarationAttributes) []string {
	// Mapping: schema field name -> Apple wire field check.
	// Flightline's projectAgeRating already does this re-mapping in reverse;
	// we duplicate the small subset here rather than depend on it (lint must
	// stay independent of internal/state).
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

	return missing
}

// jsonTagName extracts the field name from a `json:"name,omitempty"` tag,
// returning "" when the tag is empty.
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

// liveAppInfoID is local to this rule — fully answered age-rating data
// hangs off the appInfo. Mirrors state.fetchEditableAppInfo.
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
