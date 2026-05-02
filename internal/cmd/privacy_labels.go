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

func init() {
	privacyLabelsCmd.AddCommand(privacyLabelsGetCmd)
	rootCmd.AddCommand(privacyLabelsCmd)
}

func runPrivacyLabelsGet(_ *cobra.Command, args []string) error {
	bundleID := args[0]
	view := &PrivacyLabelsView{
		BundleID:  bundleID,
		Supported: false,
		Reason:    "App Store Connect API v4.3 does not expose appPrivacyDetails. Manage privacy nutrition labels via App Store Connect web UI.",
		Reference: "https://developer.apple.com/app-store/app-privacy-details/",
	}
	return Render(view, outputMode())
}
