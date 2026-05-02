package cmd

import (
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/ul0gic/skipper/internal/asc"
)

// WhoamiInfo is the command's stable JSON output shape. Field names match
// Apple's env-var convention for symmetry with the user's shell config.
//
// The struct is exported because the JSON contract is the public surface for
// LLM consumers; renaming a field is a breaking change.
type WhoamiInfo struct {
	KeyID        string `json:"keyId"`
	IssuerID     string `json:"issuerId"`
	VendorNumber string `json:"vendorNumber,omitempty"`
	Authorized   bool   `json:"authorized"`
	APIBaseURL   string `json:"apiBaseUrl"`
}

// TableRows implements TableRenderable for human output.
func (w WhoamiInfo) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"KEY_ID", w.KeyID},
		{"ISSUER_ID", w.IssuerID},
		{"VENDOR_NUMBER", w.VendorNumber},
		{"AUTHORIZED", boolStr(w.Authorized)},
		{"API_BASE_URL", w.APIBaseURL},
	}
	return headers, rows
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Verify ASC credentials and print the configured identity",
	Long: `whoami exercises the simplest auth-required ASC endpoint to verify the
configured key works, then prints the credential metadata Skipper is using.

Credentials are resolved via the standard precedence:

  --key-id flag  >  APP_STORE_CONNECT_KEY_ID  >  ~/.config/skipper/config.yaml

The .p8 private key is read from $APP_STORE_CONNECT_KEY_PATH if set, otherwise
~/.appstoreconnect/AuthKey_<KEY_ID>.p8 (mode 0600 required).

Examples:
  skipper whoami
  skipper whoami --output json | jq -r .keyId
  skipper whoami --output json | jq -e .authorized   # exit nonzero on failure`,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE:         runWhoami,
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}

func runWhoami(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	// Hit the cheapest auth-required endpoint to confirm the JWT works.
	// /v1/apps?limit=1 returns at most one App resource (or an empty array
	// for accounts that have no apps yet — still a 200 on success).
	type minAppAttrs struct{}
	if _, err := asc.Get[asc.Collection[minAppAttrs]](
		cmd.Context(),
		c,
		"/v1/apps",
		url.Values{"limit": {"1"}},
	); err != nil {
		return err
	}

	info := WhoamiInfo{
		KeyID:        viper.GetString("key_id"),
		IssuerID:     viper.GetString("issuer_id"),
		VendorNumber: viper.GetString("vendor_number"),
		Authorized:   true,
		APIBaseURL:   "https://api.appstoreconnect.apple.com",
	}
	return Render(info, outputMode())
}
