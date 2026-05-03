package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// ExportComplianceView is the read-side view for `export-compliance get`.
// Apple splits export-compliance state across two layers — a per-build
// boolean answer (`usesNonExemptEncryption` on the Build attached to the
// version) AND optional per-app encryption-declaration resources for full
// ECCN classification. Flightline surfaces both so callers (and L3 preflight)
// see whichever answer matters for the version.
type ExportComplianceView struct {
	BundleID      string                      `json:"bundleId"`
	VersionString string                      `json:"versionString"`
	Build         asc.BuildEncryptionView     `json:"build"`
	Declarations  []EncryptionDeclarationView `json:"declarations,omitempty"`
}

// EncryptionDeclarationView is one row in the declarations list.
type EncryptionDeclarationView struct {
	ID         string                                 `json:"id"`
	Type       string                                 `json:"type"`
	Attributes asc.AppEncryptionDeclarationAttributes `json:"attributes"`
}

// TableRows for the export-compliance get view. Per-build answer renders
// first (most-common case is the simple boolean), declarations follow as
// extra rows. Empty/nil values render as "(unanswered)" so the L3
// preflight signal surfaces visually.
func (v *ExportComplianceView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"VERSION", v.VersionString},
		{"BUILD_ID", v.Build.BuildID},
		{"BUILD_VERSION", v.Build.BuildVersion},
		{"USES_NON_EXEMPT_ENCRYPTION", encryptionBoolStr(v.Build.UsesNonExemptEncryption)},
		{"DECLARATION_COUNT", fmt.Sprintf("%d", len(v.Declarations))},
	}
	for i := range v.Declarations {
		d := &v.Declarations[i]
		prefix := fmt.Sprintf("DECL[%d]", i)
		rows = append(rows,
			[]string{prefix + ".ID", d.ID},
			[]string{prefix + ".STATE", d.Attributes.AppEncryptionDeclarationState},
			[]string{prefix + ".CODE_VALUE", d.Attributes.CodeValue},
			[]string{prefix + ".EXEMPT", encryptionBoolStr(d.Attributes.Exempt)},
			[]string{prefix + ".CONTAINS_PROPRIETARY", encryptionBoolStr(d.Attributes.ContainsProprietaryCryptography)},
			[]string{prefix + ".CONTAINS_THIRD_PARTY", encryptionBoolStr(d.Attributes.ContainsThirdPartyCryptography)},
			[]string{prefix + ".AVAILABLE_ON_FRENCH_STORE", encryptionBoolStr(d.Attributes.AvailableOnFrenchStore)},
			[]string{prefix + ".PLATFORM", d.Attributes.Platform},
			[]string{prefix + ".CREATED_DATE", d.Attributes.CreatedDate},
		)
	}
	return headers, rows
}

// encryptionBoolStr renders a *bool with explicit "(unanswered)" for nil.
// Different from boolPtrStr in builds.go: that one renders "" for nil; here
// the L3 preflight signal is "answered yes/no/not-yet", so we surface nil
// loudly.
func encryptionBoolStr(b *bool) string {
	if b == nil {
		return "(unanswered)"
	}
	if *b {
		return "true"
	}
	return "false"
}

var exportComplianceCmd = &cobra.Command{
	Use:   "export-compliance",
	Short: "Inspect export-compliance / encryption answers",
	Long: `export-compliance reads Apple's two-tier export-compliance surface:

  1. The per-build boolean ` + "`usesNonExemptEncryption`" + ` (lives on the Build
     attached to the version, not on the version itself).
  2. The per-app ` + "`appEncryptionDeclaration`" + ` resources for full ECCN
     classification when the boolean is not sufficient.

L3 preflight will gate submissions on a missing build-level answer; this
verb surfaces the same data for manual inspection.`,
}

var exportComplianceGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get export-compliance state for a version",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runExportComplianceGet,
	Example: `  fline export-compliance get com.example.myapp --version 1.0.1
  fline export-compliance get com.example.myapp --version 1.0.1 --output json | jq .build`,
}

var (
	exportComplianceGetVersion  string
	exportComplianceGetPlatform string
)

func init() {
	exportComplianceGetCmd.Flags().StringVar(&exportComplianceGetVersion, "version", "", "version string to look up (e.g. 1.0.1)")
	exportComplianceGetCmd.Flags().StringVar(&exportComplianceGetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	_ = exportComplianceGetCmd.MarkFlagRequired("version")

	exportComplianceCmd.AddCommand(exportComplianceGetCmd)
	rootCmd.AddCommand(exportComplianceCmd)
}

func runExportComplianceGet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(exportComplianceGetVersion)
	platform := strings.TrimSpace(exportComplianceGetPlatform)
	if versionStr == "" {
		return fmt.Errorf("export-compliance: --version is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	// Resolve the version row to find the attached build.
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
		return fmt.Errorf("export-compliance: no version %q found for %q (platform=%s)", versionStr, bundleID, platform)
	}
	versionID := versionPage.Data[0].ID

	buildView, err := fetchVersionBuildEncryption(cmd.Context(), c, versionID)
	if err != nil {
		return err
	}

	decls, err := collectAppEncryptionDeclarations(cmd.Context(), c, appID)
	if err != nil {
		return err
	}

	view := &ExportComplianceView{
		BundleID:      bundleID,
		VersionString: versionStr,
		Build:         buildView,
		Declarations:  decls,
	}
	return Render(view, outputMode())
}

// fetchVersionBuildEncryption resolves the build attached to a version and
// returns its UsesNonExemptEncryption value. If the version has no attached
// build (rare; pre-upload state), returns a zero BuildEncryptionView (all
// nil) and no error — callers see "(unanswered)" in table mode and a nil
// in JSON, both of which encode "no answer yet".
func fetchVersionBuildEncryption(ctx context.Context, c *asc.Client, versionID string) (asc.BuildEncryptionView, error) {
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/build", nil,
	)
	if err != nil {
		// 404 / no build attached is a legitimate state — the version simply
		// has no build yet. errors.As walks the wrap chain so we catch the
		// typed APIError whether or not it was wrapped by an upstream caller.
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return asc.BuildEncryptionView{}, nil
		}
		return asc.BuildEncryptionView{}, err
	}
	return asc.BuildEncryptionView{
		BuildID:                 resp.Data.ID,
		BuildVersion:            resp.Data.Attributes.Version,
		UsesNonExemptEncryption: resp.Data.Attributes.UsesNonExemptEncryption,
	}, nil
}

// collectAppEncryptionDeclarations walks the paging iterator over an app's
// encryption-declaration resources. Most apps have none (boolean answer
// suffices); some have one approved + one expired. Limit is bounded by
// Apple's natural cap.
func collectAppEncryptionDeclarations(ctx context.Context, c *asc.Client, appID string) ([]EncryptionDeclarationView, error) {
	out := make([]EncryptionDeclarationView, 0, 4)
	q := url.Values{"limit": {"50"}}
	for page, err := range asc.Pages[asc.AppEncryptionDeclarationAttributes](
		ctx, c, "/v1/apps/"+appID+"/appEncryptionDeclarations", q,
	) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, EncryptionDeclarationView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
		}
	}
	return out, nil
}
