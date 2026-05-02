package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// AgeRatingView is the read-side view for `age-rating get`. The full Apple
// questionnaire payload is embedded; vertical-table mode dumps every field
// row-by-row so authors can spot unanswered questions visually.
type AgeRatingView struct {
	ID         string                             `json:"id"`
	Type       string                             `json:"type"`
	Attributes asc.AgeRatingDeclarationAttributes `json:"attributes"`
	// VersionState is the lifecycle state of the AppStoreVersion that drove
	// this lookup. Surfaced so callers/L3 preflight can correlate (the same
	// app has separate appInfos for editable vs. live cycles).
	VersionState string `json:"versionState,omitempty"`
}

// TableRows for the age-rating get view. Each questionnaire field renders on
// its own row with a stable label so the output is grep-friendly. Empty
// values surface as the literal "(unanswered)" so visual scans land on
// missing answers fast — these are L3 preflight gold.
func (v *AgeRatingView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	a := v.Attributes
	rows = [][]string{
		{"ID", v.ID},
		{"TYPE", v.Type},
		{"VERSION_STATE", v.VersionState},
		// Frequency-enum fields
		{"alcoholTobaccoOrDrugUseOrReferences", ageRatingValue(a.AlcoholTobaccoOrDrugUseOrReferences)},
		{"contests", ageRatingValue(a.Contests)},
		{"gamblingSimulated", ageRatingValue(a.GamblingSimulated)},
		{"gunsOrOtherWeapons", ageRatingValue(a.GunsOrOtherWeapons)},
		{"horrorOrFearThemes", ageRatingValue(a.HorrorOrFearThemes)},
		{"matureOrSuggestiveThemes", ageRatingValue(a.MatureOrSuggestiveThemes)},
		{"medicalOrTreatmentInformation", ageRatingValue(a.MedicalOrTreatmentInformation)},
		{"profanityOrCrudeHumor", ageRatingValue(a.ProfanityOrCrudeHumor)},
		{"sexualContentGraphicAndNudity", ageRatingValue(a.SexualContentGraphicAndNudity)},
		{"sexualContentOrNudity", ageRatingValue(a.SexualContentOrNudity)},
		{"violenceCartoonOrFantasy", ageRatingValue(a.ViolenceCartoonOrFantasy)},
		{"violenceRealistic", ageRatingValue(a.ViolenceRealistic)},
		{"violenceRealisticProlongedGraphicOrSadistic", ageRatingValue(a.ViolenceRealisticProlongedGraphicOrSadistic)},
		// Boolean fields
		{"advertising", ageRatingBool(a.Advertising)},
		{"ageAssurance", ageRatingBool(a.AgeAssurance)},
		{"gambling", ageRatingBool(a.Gambling)},
		{"healthOrWellnessTopics", ageRatingBool(a.HealthOrWellnessTopics)},
		{"lootBox", ageRatingBool(a.LootBox)},
		{"messagingAndChat", ageRatingBool(a.MessagingAndChat)},
		{"parentalControls", ageRatingBool(a.ParentalControls)},
		{"unrestrictedWebAccess", ageRatingBool(a.UnrestrictedWebAccess)},
		{"userGeneratedContent", ageRatingBool(a.UserGeneratedContent)},
		// Overrides + reviewer info
		{"kidsAgeBand", ageRatingValue(a.KidsAgeBand)},
		{"ageRatingOverride", ageRatingValue(a.AgeRatingOverride)},
		{"ageRatingOverrideV2", ageRatingValue(a.AgeRatingOverrideV2)},
		{"koreaAgeRatingOverride", ageRatingValue(a.KoreaAgeRatingOverride)},
		{"developerAgeRatingInfoUrl", a.DeveloperAgeRatingInfoURL},
	}
	return headers, rows
}

// ageRatingValue formats a frequency/override enum value for table mode.
// Empty string surfaces as "(unanswered)" so visual scans catch gaps.
func ageRatingValue(v string) string {
	if v == "" {
		return "(unanswered)"
	}
	return v
}

// ageRatingBool formats a *bool for table mode, distinguishing nil ("not
// answered") from explicit true/false.
func ageRatingBool(b *bool) string {
	if b == nil {
		return "(unanswered)"
	}
	if *b {
		return "true"
	}
	return "false"
}

var ageRatingCmd = &cobra.Command{
	Use:   "age-rating",
	Short: "Inspect Apple age-rating declarations",
	Long: `age-rating reads the questionnaire Apple uses to compute a version's
age rating. The declaration lives on the per-version appInfo resource;
Skipper resolves bundleId + versionString to the right appInfo and
fetches its ageRatingDeclaration.

L3 preflight will flag declarations with unanswered questions — surface
the same data here for manual inspection.`,
}

var ageRatingGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get the age-rating declaration for a version",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAgeRatingGet,
	Example: `  skipper age-rating get com.example.myapp --version 1.0.1
  skipper age-rating get com.example.myapp --version 1.0.1 --output json | jq .attributes`,
}

var (
	ageRatingGetVersion  string
	ageRatingGetPlatform string
)

func init() {
	ageRatingGetCmd.Flags().StringVar(&ageRatingGetVersion, "version", "", "version string to look up (e.g. 1.0.1)")
	ageRatingGetCmd.Flags().StringVar(&ageRatingGetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	_ = ageRatingGetCmd.MarkFlagRequired("version")

	ageRatingCmd.AddCommand(ageRatingGetCmd)
	rootCmd.AddCommand(ageRatingCmd)
}

func runAgeRatingGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(ageRatingGetVersion)
	platform := strings.TrimSpace(ageRatingGetPlatform)
	if versionStr == "" {
		return fmt.Errorf("age-rating: --version is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Pull the version row first — we need its lifecycle state to pick the
	// matching appInfo (each app keeps a "live" appInfo and an "editable"
	// appInfo; they share state with the version that owns them).
	vQuery := url.Values{
		"filter[versionString]": {versionStr},
		"limit":                 {"1"},
	}
	if platform != "" {
		vQuery.Set("filter[platform]", platform)
	}
	versionPage, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		cmd.Context(), c, "/v1/apps/"+appID+"/appStoreVersions", vQuery,
	)
	if err != nil {
		return err
	}
	if len(versionPage.Data) == 0 {
		return fmt.Errorf("age-rating: no version %q found for %q (platform=%s)", versionStr, bundleID, platform)
	}
	versionState := versionDisplayState(versionPage.Data[0].Attributes)

	appInfoID, err := pickAppInfoForVersion(cmd.Context(), c, appID, versionState)
	if err != nil {
		return err
	}

	decl, err := asc.Get[asc.Single[asc.AgeRatingDeclarationAttributes]](
		cmd.Context(), c, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil,
	)
	if err != nil {
		return err
	}

	view := &AgeRatingView{
		ID:           decl.Data.ID,
		Type:         decl.Data.Type,
		Attributes:   decl.Data.Attributes,
		VersionState: versionState,
	}
	return Render(view, outputMode())
}

// pickAppInfoForVersion lists an app's appInfos and picks the one whose
// state matches the version's lifecycle bucket. The mapping:
//
//   - Editable bucket (PREPARE_FOR_SUBMISSION, WAITING_FOR_REVIEW,
//     IN_REVIEW, REJECTED, DEVELOPER_REJECTED, READY_FOR_REVIEW,
//     REPLACED_WITH_NEW_INFO, ACCEPTED, PENDING_RELEASE) — there is at
//     most one editable appInfo at a time.
//   - Live bucket (READY_FOR_DISTRIBUTION) — there is at most one live
//     appInfo at a time.
//
// When the bucket-matching appInfo doesn't exist (rare; happens during
// transitional moments), we fall back to the first appInfo in the list.
//
// This logic is the most pragmatic available: Apple's API does not expose
// a direct version → appInfo relationship. Apple's web UI uses the same
// bucketing internally.
func pickAppInfoForVersion(ctx context.Context, c *asc.Client, appID, versionState string) (string, error) {
	q := url.Values{"limit": {"50"}}
	page, err := asc.Get[asc.Collection[asc.AppInfoAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appInfos", q,
	)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("age-rating: app %q has no appInfo records", appID)
	}

	wantBucket := isLiveVersionState(versionState)
	for i := range page.Data {
		info := &page.Data[i]
		if isLiveVersionState(info.Attributes.State) == wantBucket {
			return info.ID, nil
		}
	}
	// Fallback: first appInfo. Surface this as a soft signal via state on the
	// returned attributes — callers see VersionState ≠ matching info.state.
	return page.Data[0].ID, nil
}

// isLiveVersionState classifies a version/appInfo state into the live (true)
// vs editable (false) bucket. READY_FOR_DISTRIBUTION is Apple's "currently
// shipping" state.
func isLiveVersionState(state string) bool {
	return state == "READY_FOR_DISTRIBUTION" || state == "READY_FOR_SALE"
}
