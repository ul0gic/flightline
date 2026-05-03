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

// ExportComplianceWriteResult is the JSON-stable view returned by
// `export-compliance set`. noop=true means current state already matched.
type ExportComplianceWriteResult struct {
	Action                  string `json:"action"`
	BundleID                string `json:"bundleId"`
	VersionString           string `json:"versionString"`
	BuildID                 string `json:"buildId"`
	BuildVersion            string `json:"buildVersion"`
	UsesNonExemptEncryption *bool  `json:"usesNonExemptEncryption"`
	NoOp                    bool   `json:"noop"`
}

// TableRows for ExportComplianceWriteResult.
func (r *ExportComplianceWriteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"BUNDLE_ID", r.BundleID},
		{"VERSION", r.VersionString},
		{"BUILD_ID", r.BuildID},
		{"BUILD_VERSION", r.BuildVersion},
		{"USES_NON_EXEMPT_ENCRYPTION", encryptionBoolStr(r.UsesNonExemptEncryption)},
		{"NOOP", fmt.Sprintf("%t", r.NoOp)},
	}
	return headers, rows
}

// buildPatchRequest is the wire body for PATCH /v1/builds/{id}.
// Mirrors components.schemas.BuildUpdateRequest.
type buildPatchRequest struct {
	Data buildPatchData `json:"data"`
}

type buildPatchData struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	Attributes buildPatchAttrs `json:"attributes"`
}

type buildPatchAttrs struct {
	UsesNonExemptEncryption *bool `json:"usesNonExemptEncryption,omitempty"`
}

// ErrExportComplianceFutureFlag is returned when the caller passes flags that
// require an AppEncryptionDeclaration — a separate POST surface not yet wired
// in L1. Keeps v1 narrow without silently dropping the user's intent.
var ErrExportComplianceFutureFlag = errors.New(
	"export-compliance set: --exempt and --documentation-url require AppEncryptionDeclaration support, " +
		"which lands in a follow-up; for the boolean answer use --uses-encryption {true,false} alone",
)

var exportComplianceSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set the build's usesNonExemptEncryption answer (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runExportComplianceSet,
	Long: `set PATCHes the build attached to a version with the export-compliance
boolean Apple requires before review.

Apple's two-tier model:

  1. Per-build boolean — ` + "`usesNonExemptEncryption`" + ` on the Build attached to
     the version. This verb writes that field.
  2. Per-app AppEncryptionDeclaration — for full ECCN classification. The
     ` + "`--exempt`" + ` and ` + "`--documentation-url`" + ` flags target this surface and are
     reserved for a follow-up command; they currently return a typed error.

Idempotent: reads the build's current answer; PATCH only when the requested
value differs.`,
	Example: `  fline export-compliance set com.example.myapp --version 1.0.1 --uses-encryption false
  fline export-compliance set com.example.myapp --version 1.0.1 --uses-encryption true --output json`,
}

var (
	exportComplianceSetVersion          string
	exportComplianceSetPlatform         string
	exportComplianceSetUsesEncryption   string
	exportComplianceSetExempt           bool
	exportComplianceSetDocumentationURL string
)

func init() {
	exportComplianceSetCmd.Flags().StringVar(&exportComplianceSetVersion, "version", "", "version string to look up (e.g. 1.0.1)")
	exportComplianceSetCmd.Flags().StringVar(&exportComplianceSetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	exportComplianceSetCmd.Flags().StringVar(&exportComplianceSetUsesEncryption, "uses-encryption", "", "true | false — whether the build uses non-exempt encryption")
	exportComplianceSetCmd.Flags().BoolVar(&exportComplianceSetExempt, "exempt", false, "(reserved) AppEncryptionDeclaration exemption — see follow-up")
	exportComplianceSetCmd.Flags().StringVar(&exportComplianceSetDocumentationURL, "documentation-url", "", "(reserved) AppEncryptionDeclaration documentation URL — see follow-up")
	_ = exportComplianceSetCmd.MarkFlagRequired("version")
	_ = exportComplianceSetCmd.MarkFlagRequired("uses-encryption")

	exportComplianceCmd.AddCommand(exportComplianceSetCmd)
}

func runExportComplianceSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(exportComplianceSetVersion)
	platform := strings.TrimSpace(exportComplianceSetPlatform)
	usesEncRaw := strings.TrimSpace(exportComplianceSetUsesEncryption)

	// Reserved flags surface as typed errors so users see why their command
	// didn't fire.
	if exportComplianceSetExempt || strings.TrimSpace(exportComplianceSetDocumentationURL) != "" {
		return ErrExportComplianceFutureFlag
	}

	desired, err := resolveTriBool("uses-encryption", usesEncRaw)
	if err != nil {
		return fmt.Errorf("export-compliance set: %w", err)
	}
	if desired == nil {
		return errors.New("export-compliance set: --uses-encryption is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	versionID, err := lookupVersionIDForCompliance(cmd.Context(), c, appID, versionStr, platform)
	if err != nil {
		return err
	}

	buildID, buildVersion, current, err := fetchVersionBuildEncryptionForSet(cmd.Context(), c, versionID)
	if err != nil {
		return err
	}
	if buildID == "" {
		return fmt.Errorf("export-compliance set: version %q has no build attached yet (use `fline builds attach` first)", versionStr)
	}

	if boolPtrEq(current, desired) {
		return Render(&ExportComplianceWriteResult{
			Action:                  "set",
			BundleID:                bundleID,
			VersionString:           versionStr,
			BuildID:                 buildID,
			BuildVersion:            buildVersion,
			UsesNonExemptEncryption: current,
			NoOp:                    true,
		}, outputMode())
	}

	body := buildPatchRequest{
		Data: buildPatchData{
			Type:       "builds",
			ID:         buildID,
			Attributes: buildPatchAttrs{UsesNonExemptEncryption: desired},
		},
	}
	if _, err := asc.Patch[asc.Single[asc.BuildAttributes]](cmd.Context(), c, "/v1/builds/"+buildID, nil, body); err != nil {
		return err
	}

	return Render(&ExportComplianceWriteResult{
		Action:                  "set",
		BundleID:                bundleID,
		VersionString:           versionStr,
		BuildID:                 buildID,
		BuildVersion:            buildVersion,
		UsesNonExemptEncryption: desired,
		NoOp:                    false,
	}, outputMode())
}

// lookupVersionIDForCompliance resolves bundle+version+platform to the
// AppStoreVersion ID. Same shape as lookupVersionState but returns the ID
// rather than the lifecycle state — set needs the version-relationship hop
// to find the attached build.
func lookupVersionIDForCompliance(ctx context.Context, c *asc.Client, appID, versionStr, platform string) (string, error) {
	q := url.Values{
		"filter[versionString]": {versionStr},
		"limit":                 {"1"},
	}
	if platform != "" {
		q.Set("filter[platform]", platform)
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("export-compliance set: no version %q found (platform=%s)", versionStr, platform)
	}
	return page.Data[0].ID, nil
}

// fetchVersionBuildEncryptionForSet returns the attached build's ID, version
// string, and current usesNonExemptEncryption answer. (zero, "", nil, nil)
// when no build is attached — caller treats as "must attach first".
func fetchVersionBuildEncryptionForSet(ctx context.Context, c *asc.Client, versionID string) (buildID, buildVersion string, current *bool, err error) {
	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/build", nil,
	)
	if err != nil {
		// 404 / no build attached is a legitimate state.
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return "", "", nil, nil
		}
		return "", "", nil, err
	}
	return resp.Data.ID, resp.Data.Attributes.Version, resp.Data.Attributes.UsesNonExemptEncryption, nil
}
