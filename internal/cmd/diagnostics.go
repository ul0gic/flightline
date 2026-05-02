package cmd

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// DiagnosticSignatureView is one row of the diagnostics list output.
type DiagnosticSignatureView struct {
	ID         string                            `json:"id"`
	Type       string                            `json:"type"`
	Attributes asc.DiagnosticSignatureAttributes `json:"attributes"`
}

// DiagnosticSignatureList is the table-aware view for `diagnostics list`.
type DiagnosticSignatureList struct {
	BundleID   string                    `json:"bundleId"`
	BuildID    string                    `json:"buildId"`
	Signatures []DiagnosticSignatureView `json:"signatures"`
}

// TableRows implements TableRenderable for the diagnostics list view.
//
// Signatures are surfaced sorted by weight desc (most-impactful first); the
// signature column truncates because Apple's signatures can be hundreds of
// characters wide and would blow out the table.
func (l DiagnosticSignatureList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"WEIGHT", "TYPE", "SIGNATURE", "INSIGHT", "ID"}
	rows = make([][]string, 0, len(l.Signatures))
	for i := range l.Signatures {
		s := &l.Signatures[i]
		rows = append(rows, []string{
			formatWeight(s.Attributes.Weight),
			s.Attributes.DiagnosticType,
			truncTitle(s.Attributes.Signature, 60),
			insightSummary(s.Attributes.Insight),
			s.ID,
		})
	}
	return headers, rows
}

// DiagnosticGetView is the read-side view for `diagnostics get <signatureId>`.
//
// Apple's v4.3 spec exposes only /v1/diagnosticSignatures/{id}/logs (no
// /v1/diagnosticSignatures/{id} get); the get verb resolves to the logs
// endpoint and surfaces ProductData (insights + call-stack trees) plus a
// version string.
type DiagnosticGetView struct {
	SignatureID string                      `json:"signatureId"`
	Version     string                      `json:"version,omitempty"`
	ProductData []asc.DiagnosticProductData `json:"productData,omitempty"`
	Note        string                      `json:"note,omitempty"`
}

// TableRows for the diagnostics get view. Vertical layout summarizes the
// log payload; consumers wanting the full stack trace must use --output json.
func (v *DiagnosticGetView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"SIGNATURE_ID", v.SignatureID},
		{"VERSION", v.Version},
	}
	if v.Note != "" {
		rows = append(rows, []string{"NOTE", v.Note})
	}
	for i := range v.ProductData {
		pd := &v.ProductData[i]
		rows = append(rows,
			[]string{"PRODUCT_SIGNATURE_ID", pd.SignatureID},
			[]string{"INSIGHTS", strconv.Itoa(len(pd.DiagnosticInsights))},
			[]string{"LOGS", strconv.Itoa(len(pd.DiagnosticLogs))},
		)
		// Surface the first log's metadata so the table is glanceable.
		if len(pd.DiagnosticLogs) > 0 {
			md := pd.DiagnosticLogs[0].DiagnosticMetaData
			rows = append(rows,
				[]string{"EVENT", md.Event},
				[]string{"BUILD_VERSION", md.BuildVersion},
				[]string{"OS_VERSION", md.OsVersion},
				[]string{"DEVICE_TYPE", md.DeviceType},
			)
		}
	}
	return headers, rows
}

// formatWeight renders Apple's float weight as a fixed-precision number.
// JSON output retains the float; only the table view rounds.
func formatWeight(w float64) string {
	if w == 0 {
		return "0"
	}
	return strconv.FormatFloat(w, 'f', 2, 64)
}

// insightSummary collapses the (optional) insight payload into a short
// glanceable cell — direction + count of reference versions.
func insightSummary(in *asc.DiagnosticInsight) string {
	if in == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if in.Direction != "" {
		parts = append(parts, in.Direction)
	}
	if n := len(in.ReferenceVersions); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" refs")
	}
	return strings.Join(parts, " ")
}

var diagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Read crash and hang diagnostic signatures (build-scoped)",
	Long: `diagnostics groups read commands over Apple's diagnostic signatures
resource. Apple deduplicates crash and hang reports into signatures —
same call stack, same crash, regardless of how many users hit it.

Apple v4.3 only exposes diagnostic signatures scoped to a build:

  - list <bundleId> --build <number>     — list signatures for a build
  - get <signatureId>                    — fetch the full log payload

There is no app-wide aggregation API in v4.3; --build is required on list.`,
}

var diagnosticsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List diagnostic signatures for a specific build",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runDiagnosticsList,
	Example: `  skipper diagnostics list com.example.myapp --build 42
  skipper diagnostics list com.example.myapp --build 42 --type HANGS
  skipper diagnostics list com.example.myapp --build 42 --output json | jq '.signatures[].attributes.weight'`,
}

var diagnosticsGetCmd = &cobra.Command{
	Use:          "get <signatureId>",
	Short:        "Fetch the full log payload for a diagnostic signature",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runDiagnosticsGet,
	Example: `  skipper diagnostics get DIAG-SIG-1234
  skipper diagnostics get DIAG-SIG-1234 --output json`,
}

var (
	diagnosticsListBuild string
	diagnosticsListType  string
	diagnosticsListLimit int
)

func init() {
	diagnosticsListCmd.Flags().StringVar(&diagnosticsListBuild, "build", "", "build number to inspect (CFBundleVersion, e.g. 42)")
	diagnosticsListCmd.Flags().StringVar(&diagnosticsListType, "type", "", "filter by diagnostic type: DISK_WRITES | HANGS | LAUNCHES")
	diagnosticsListCmd.Flags().IntVar(&diagnosticsListLimit, "limit", 0, "max signatures to emit (0 = no cap)")
	_ = diagnosticsListCmd.MarkFlagRequired("build")

	diagnosticsCmd.AddCommand(diagnosticsListCmd)
	diagnosticsCmd.AddCommand(diagnosticsGetCmd)
	rootCmd.AddCommand(diagnosticsCmd)
}

func runDiagnosticsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	build := strings.TrimSpace(diagnosticsListBuild)
	if build == "" {
		return fmt.Errorf("diagnostics: --build is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}

	bq := url.Values{
		"filter[version]": {build},
		"limit":           {"1"},
	}
	bpage, err := asc.Get[asc.Collection[asc.BuildAttributes]](
		cmd.Context(), c, "/v1/apps/"+appID+"/builds", bq,
	)
	if err != nil {
		return err
	}
	if len(bpage.Data) == 0 {
		return fmt.Errorf("diagnostics: no build %q found for %q", build, bundleID)
	}
	buildID := bpage.Data[0].ID

	q := url.Values{"limit": {"200"}}
	if dt := strings.TrimSpace(diagnosticsListType); dt != "" {
		q.Set("filter[diagnosticType]", dt)
	}

	views, err := collectDiagnosticSignatures(
		cmd.Context(), c,
		"/v1/builds/"+buildID+"/diagnosticSignatures",
		q, diagnosticsListLimit,
	)
	if err != nil {
		return err
	}

	// Sort newest-first by weight (most user-impacting first). Apple's
	// API does not expose a sort param for this collection in v4.3.
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].Attributes.Weight > views[j].Attributes.Weight
	})

	return Render(DiagnosticSignatureList{
		BundleID:   bundleID,
		BuildID:    buildID,
		Signatures: views,
	}, outputMode())
}

func runDiagnosticsGet(cmd *cobra.Command, args []string) error {
	signatureID := strings.TrimSpace(args[0])
	if signatureID == "" {
		return fmt.Errorf("diagnostics: signature id is required")
	}
	c, err := newClient()
	if err != nil {
		return err
	}

	// The /logs endpoint returns a non-JSON:API custom envelope: read it
	// directly via Get into the typed DiagnosticLogsResponse.
	logs, err := asc.Get[asc.DiagnosticLogsResponse](
		cmd.Context(), c, "/v1/diagnosticSignatures/"+signatureID+"/logs", nil,
	)
	if err != nil {
		return err
	}

	view := &DiagnosticGetView{
		SignatureID: signatureID,
		Version:     logs.Version,
		ProductData: logs.ProductData,
	}
	if len(logs.ProductData) == 0 {
		view.Note = "no diagnostic logs available for this signature (Apple may not have processed the logs yet, or the signature has no captured stack traces)"
	}
	return Render(view, outputMode())
}

// collectDiagnosticSignatures walks the paging iterator. Diagnostic
// signatures are bounded per build (typically <100), so paging is rare —
// but the helper still uses Pages for consistency with every other list
// surface.
func collectDiagnosticSignatures(ctx context.Context, c *asc.Client, path string, query url.Values, limit int) ([]DiagnosticSignatureView, error) {
	out := make([]DiagnosticSignatureView, 0, defaultListCap(limit))
	for page, err := range asc.Pages[asc.DiagnosticSignatureAttributes](ctx, c, path, query) {
		if err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			out = append(out, DiagnosticSignatureView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}
