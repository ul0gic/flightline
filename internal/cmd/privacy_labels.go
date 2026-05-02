package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// PrivacyLabelsView is the read-side view for `privacy-labels get`.
//
// Apple's App Store Connect API v4.3 does NOT expose an appPrivacyDetails
// resource — privacy nutrition labels are authored exclusively in App
// Store Connect's web UI. Until Apple ships an API surface, this command
// returns a typed "not supported" diagnostic so callers and L3 preflight
// see a stable, honest signal rather than silently succeeding against a
// non-existent endpoint.
//
// JSON shape is stable so consumers can detect the diagnostic case
// programmatically: `.supported == false` and `.reason` carries the
// explanation. When Apple ships the endpoint, switch to a typed view
// embedding the new attributes; consumers checking .supported continue
// to work.
//
// Tracked in ISSUE-002.
type PrivacyLabelsView struct {
	BundleID  string `json:"bundleId"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// TableRows for the privacy-labels get view. Surfaces the diagnostic on
// stdout (data, not stderr — the user piped this for a reason).
func (v *PrivacyLabelsView) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"BUNDLE_ID", v.BundleID},
		{"SUPPORTED", fmt.Sprintf("%t", v.Supported)},
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
not expose this surface — labels are authored exclusively in App Store
Connect's web UI.

This command returns a typed diagnostic so callers can detect the
unsupported state programmatically. When Apple ships an API endpoint,
the command will be wired without changing the JSON contract.

See ISSUE-002 in .project/issues/.`,
}

var privacyLabelsGetCmd = &cobra.Command{
	Use:          "get <bundleId>",
	Short:        "Get privacy nutrition labels for an app (currently unsupported by ASC API)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPrivacyLabelsGet,
	Example: `  skipper privacy-labels get com.example.myapp
  skipper privacy-labels get com.example.myapp --output json | jq .supported`,
}

// privacyLabelsSetCmd is the write-side companion of privacy-labels get.
// Apple's ASC API v4.3 has no appPrivacyDetails surface, so set returns
// the same typed diagnostic as get rather than fabricating an endpoint.
// See ISSUE-002.
var privacyLabelsSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set privacy nutrition labels (currently unsupported by ASC API)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runPrivacyLabelsSet,
	Long: `set would PATCH an app's privacy nutrition labels. Apple's App Store Connect
API v4.3 does not expose this surface — labels are authored exclusively in
App Store Connect's web UI.

This command returns a typed diagnostic so callers can detect the
unsupported state programmatically (` + "`.supported == false`" + `). When Apple
ships an API endpoint, set will be wired without changing the JSON contract.

See ISSUE-002 in .project/issues/.`,
	Example: `  skipper privacy-labels set com.example.myapp --from labels.yaml
  skipper privacy-labels set com.example.myapp --output json | jq .supported`,
}

// privacyLabelsSetFrom is accepted but unused — kept on the surface so the
// flag exists when Apple eventually ships the endpoint and the JSON
// contract stays forward-compatible.
var privacyLabelsSetFrom string

func init() {
	privacyLabelsSetCmd.Flags().StringVar(&privacyLabelsSetFrom, "from", "", "(reserved) path to YAML/JSON describing the labels — see ISSUE-002")

	privacyLabelsCmd.AddCommand(privacyLabelsGetCmd)
	privacyLabelsCmd.AddCommand(privacyLabelsSetCmd)
	rootCmd.AddCommand(privacyLabelsCmd)
}

// privacyLabelsDiagnostic is the canonical "unsupported" view returned by
// both get and set. Centralizing it keeps the JSON contract identical.
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
