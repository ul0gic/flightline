package cmd

import (
	"strconv"

	"github.com/spf13/cobra"
)

// ASC API v4.3 has no appPrivacyDetails resource (web-UI only). See ISSUE-002.
type PrivacyLabelsView struct {
	BundleID  string `json:"bundleId"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Reference string `json:"reference,omitempty"`
}

func (v *PrivacyLabelsView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"SUPPORTED", strconv.FormatBool(v.Supported)},
		{"REASON", v.Reason},
		{"REFERENCE", v.Reference},
	}
	return headers, rows
}

var privacyLabelsCmd = &cobra.Command{
	Use:   "privacy-labels",
	Short: "Inspect privacy nutrition labels",
	Long: `privacy-labels would read Apple's App Privacy Details
(nutrition labels) for an app. Apple's App Store Connect API v4.3 does
not expose this surface: labels are authored exclusively in App Store
Connect's web UI.

This command returns a typed diagnostic so callers can detect the
unsupported state programmatically. When Apple ships an API endpoint,
the command can be wired without changing the JSON contract.`,
}

var privacyLabelsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get privacy nutrition labels for an app (currently unsupported by ASC API)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPrivacyLabelsGet,
	Example: `  flightline privacy-labels get com.example.myapp
  flightline privacy-labels get com.example.myapp --output json | jq .supported`,
}

// No appPrivacyDetails surface in v4.3; set returns the same typed
// diagnostic as get rather than fabricating an endpoint. See ISSUE-002.
var privacyLabelsSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set privacy nutrition labels (currently unsupported by ASC API)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPrivacyLabelsSet,
	Long: `set would PATCH an app's privacy nutrition labels. Apple's App Store Connect
API v4.3 does not expose this surface: labels are authored exclusively in
App Store Connect's web UI.

This command returns a typed diagnostic so callers can detect the
unsupported state programmatically (` + "`.supported == false`" + `). When Apple
ships an API endpoint, set can be wired without changing the JSON contract.`,
	Example: `  flightline privacy-labels set com.example.myapp --from labels.yaml
  flightline privacy-labels set com.example.myapp --output json | jq .supported`,
}

// Accepted but unused: reserved so the flag exists when Apple ships the
// endpoint, keeping the contract forward-compatible.
var privacyLabelsSetFrom string

func init() {
	privacyLabelsSetCmd.Flags().StringVar(&privacyLabelsSetFrom, "from", "", "(reserved) path to YAML/JSON describing the labels")

	privacyLabelsCmd.AddCommand(privacyLabelsGetCmd)
	privacyLabelsCmd.AddCommand(privacyLabelsSetCmd)
	rootCmd.AddCommand(privacyLabelsCmd)
}

// Shared by get and set so the JSON contract is identical.
func privacyLabelsDiagnostic(bundleID string) *PrivacyLabelsView {
	return &PrivacyLabelsView{
		BundleID:  bundleID,
		Supported: false,
		Reason:    "App Store Connect API v4.3 does not expose appPrivacyDetails. Manage privacy nutrition labels via App Store Connect web UI.",
		Reference: "https://developer.apple.com/app-store/app-privacy-details/",
	}
}

func runPrivacyLabelsGet(_ *cobra.Command, args []string) error {
	return Render(privacyLabelsDiagnostic(args[0]), outputMode())
}

func runPrivacyLabelsSet(_ *cobra.Command, args []string) error {
	return Render(privacyLabelsDiagnostic(args[0]), outputMode())
}
