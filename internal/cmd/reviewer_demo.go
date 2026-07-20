// The --password flag is a credential surface: it is never logged, never
// echoed by --verbose, and filtered out of every error string before return.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// AppStoreReviewDetailAttributes lives in asc so the lint package can share it.
type AppStoreReviewDetailAttributes = asc.AppStoreReviewDetailAttributes

// reviewDetailCreateRequest is the wire body for POST /v1/appStoreReviewDetails.
type reviewDetailCreateRequest struct {
	Data reviewDetailCreateData `json:"data"`
}

type reviewDetailCreateData struct {
	Type          string                         `json:"type"`
	Attributes    AppStoreReviewDetailAttributes `json:"attributes"`
	Relationships map[string]relRefBlock         `json:"relationships"`
}

// reviewDetailPatchRequest is the wire body for PATCH /v1/appStoreReviewDetails/{id}.
type reviewDetailPatchRequest struct {
	Data reviewDetailPatchData `json:"data"`
}

type reviewDetailPatchData struct {
	Type       string                         `json:"type"`
	ID         string                         `json:"id"`
	Attributes AppStoreReviewDetailAttributes `json:"attributes"`
}

// ReviewerDemoWriteResult is the JSON-stable view returned by `reviewer-demo set`.
// DemoAccountPasswordSet reports whether a password is on file without echoing the secret.
type ReviewerDemoWriteResult struct {
	Action                 string   `json:"action"`
	ID                     string   `json:"id"`
	Type                   string   `json:"type"`
	BundleID               string   `json:"bundleId"`
	VersionString          string   `json:"versionString"`
	NoOp                   bool     `json:"noop"`
	ChangedKeys            []string `json:"changedKeys,omitempty"`
	ContactFirstName       string   `json:"contactFirstName,omitempty"`
	ContactLastName        string   `json:"contactLastName,omitempty"`
	ContactPhone           string   `json:"contactPhone,omitempty"`
	ContactEmail           string   `json:"contactEmail,omitempty"`
	DemoAccountName        string   `json:"demoAccountName,omitempty"`
	DemoAccountPasswordSet bool     `json:"demoAccountPasswordSet"`
	DemoAccountRequired    *bool    `json:"demoAccountRequired,omitempty"`
	Notes                  string   `json:"notes,omitempty"`
}

// TableRows for ReviewerDemoWriteResult. Password value is intentionally
// absent: only the boolean "set" flag.
func (r *ReviewerDemoWriteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"ID", r.ID},
		{"TYPE", r.Type},
		{"BUNDLE_ID", r.BundleID},
		{"VERSION", r.VersionString},
		{"NOOP", strconv.FormatBool(r.NoOp)},
		{"CHANGED_KEYS", strings.Join(r.ChangedKeys, ",")},
		{"CONTACT_FIRST_NAME", r.ContactFirstName},
		{"CONTACT_LAST_NAME", r.ContactLastName},
		{"CONTACT_PHONE", r.ContactPhone},
		{"CONTACT_EMAIL", r.ContactEmail},
		{"DEMO_ACCOUNT_NAME", r.DemoAccountName},
		{"DEMO_ACCOUNT_PASSWORD_SET", strconv.FormatBool(r.DemoAccountPasswordSet)},
		{"DEMO_ACCOUNT_REQUIRED", boolPtrStr(r.DemoAccountRequired)},
		{"NOTES", r.Notes},
	}
	return headers, rows
}

var reviewerDemoCmd = &cobra.Command{
	Use:   "reviewer-demo",
	Short: "Manage the App Store Review demo account + reviewer contact info",
	Long: `reviewer-demo configures the per-version appStoreReviewDetail Apple
shows reviewers during App Store Review.

Security: --password is never written to logs, never echoed in --verbose
output, and never appears in error messages. Prefer --password-file
<path> to keep the secret out of shell history.`,
}

var reviewerDemoSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set the App Store reviewer demo account + contact info (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runReviewerDemoSet,
	Long: `set creates or PATCHes the appStoreReviewDetail for a version. Diffs
against current state: only fields that differ go in the body. When all
supplied flags already match current state, returns noop=true.

The --password flag is treated specially: never logged, never echoed,
never included in any error output. Use --password-file <path> to read
the secret from a file rather than the shell command line.`,
	Example: `  flightline reviewer-demo set com.example.myapp --version 1.0.1 --contact-name "Jane Doe" --contact-email reviewer@example.com
  flightline reviewer-demo set com.example.myapp --version 1.0.1 --username demo@example.com --password-file ./.password
  flightline reviewer-demo set com.example.myapp --version 1.0.1 --notes "Tap the gear icon to access the demo flow"`,
}

var (
	reviewerDemoSetVersion      string
	reviewerDemoSetPlatform     string
	reviewerDemoSetContactName  string
	reviewerDemoSetContactEmail string
	reviewerDemoSetContactPhone string
	reviewerDemoSetUsername     string
	reviewerDemoSetPassword     string
	reviewerDemoSetPasswordFile string
	reviewerDemoSetNotes        string
)

func init() {
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetVersion, "version", "", "version string to look up (e.g. 1.0.1)")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetContactName, "contact-name", "", "reviewer contact full name; split on first space into first/last")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetContactEmail, "contact-email", "", "reviewer contact email")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetContactPhone, "contact-phone", "", "reviewer contact phone")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetUsername, "username", "", "demo account username")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetPassword, "password", "", "demo account password (NEVER logged or echoed; prefer --password-file)")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetPasswordFile, "password-file", "", "path to a file containing the demo account password (preferred over --password)")
	reviewerDemoSetCmd.Flags().StringVar(&reviewerDemoSetNotes, "notes", "", "freeform reviewer notes")
	_ = reviewerDemoSetCmd.MarkFlagRequired("version")

	reviewerDemoCmd.AddCommand(reviewerDemoSetCmd)
	rootCmd.AddCommand(reviewerDemoCmd)
}

func runReviewerDemoSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(reviewerDemoSetVersion)
	platform := strings.TrimSpace(reviewerDemoSetPlatform)

	password, err := resolveReviewerPassword()
	if err != nil {
		return err
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
		return redactReviewerError(err, password)
	}

	existingID, current, err := fetchAppStoreReviewDetail(cmd.Context(), c, versionID)
	if err != nil {
		return redactReviewerError(err, password)
	}

	desired := buildDesiredReviewerDetail(cmd, password)

	if existingID == "" {
		// Create.
		body := reviewDetailCreateRequest{
			Data: reviewDetailCreateData{
				Type:       "appStoreReviewDetails",
				Attributes: desired,
				Relationships: map[string]relRefBlock{
					"appStoreVersion": {Data: relRef{Type: "appStoreVersions", ID: versionID}},
				},
			},
		}
		resp, err := asc.Post[asc.Single[AppStoreReviewDetailAttributes]](
			cmd.Context(), c, "/v1/appStoreReviewDetails", nil, body,
		)
		if err != nil {
			return redactReviewerError(err, password)
		}
		return Render(buildReviewerResult("create", resp.Data.ID, resp.Data.Type, bundleID, versionStr, false, allChangedKeys(desired), resp.Data.Attributes), outputMode())
	}

	// Update: diff against current.
	delta, changed := diffReviewerDetail(current, desired)
	if !changed {
		return Render(buildReviewerResult("set", existingID, "appStoreReviewDetails", bundleID, versionStr, true, nil, current), outputMode())
	}

	body := reviewDetailPatchRequest{
		Data: reviewDetailPatchData{
			Type:       "appStoreReviewDetails",
			ID:         existingID,
			Attributes: delta,
		},
	}
	resp, err := asc.Patch[asc.Single[AppStoreReviewDetailAttributes]](
		cmd.Context(), c, "/v1/appStoreReviewDetails/"+existingID, nil, body,
	)
	if err != nil {
		return redactReviewerError(err, password)
	}
	return Render(buildReviewerResult("set", resp.Data.ID, resp.Data.Type, bundleID, versionStr, false, changedKeys(delta), resp.Data.Attributes), outputMode())
}

// resolveReviewerPassword picks --password-file over --password; empty is allowed.
// File contents are never logged; only the path appears in errors.
func resolveReviewerPassword() (string, error) {
	if reviewerDemoSetPasswordFile != "" && reviewerDemoSetPassword != "" {
		return "", errors.New("reviewer-demo set: --password and --password-file are mutually exclusive (pick one)")
	}
	if reviewerDemoSetPasswordFile != "" {
		buf, err := os.ReadFile(reviewerDemoSetPasswordFile) //nolint:gosec // path supplied by trusted caller
		if err != nil {
			return "", fmt.Errorf("reviewer-demo set: read --password-file %s: %w", reviewerDemoSetPasswordFile, err)
		}
		// Trim only trailing newline; inner whitespace is part of the password.
		return strings.TrimRight(string(buf), "\r\n"), nil
	}
	return reviewerDemoSetPassword, nil
}

// buildDesiredReviewerDetail builds the desired attributes from flag state.
// Only flags the user changed are set, so the diff never clobbers unmentioned fields.
func buildDesiredReviewerDetail(cmd *cobra.Command, password string) AppStoreReviewDetailAttributes {
	out := AppStoreReviewDetailAttributes{}
	if cmd.Flags().Changed("contact-name") {
		first, last := splitContactName(reviewerDemoSetContactName)
		out.ContactFirstName = strPtr(first)
		out.ContactLastName = strPtr(last)
	}
	if cmd.Flags().Changed("contact-email") {
		out.ContactEmail = strPtr(reviewerDemoSetContactEmail)
	}
	if cmd.Flags().Changed("contact-phone") {
		out.ContactPhone = strPtr(reviewerDemoSetContactPhone)
	}
	if cmd.Flags().Changed("username") {
		out.DemoAccountName = strPtr(reviewerDemoSetUsername)
	}
	if cmd.Flags().Changed("password") || cmd.Flags().Changed("password-file") {
		out.DemoAccountPassword = strPtr(password)
	}
	if cmd.Flags().Changed("notes") {
		out.Notes = strPtr(reviewerDemoSetNotes)
	}
	// demoAccountRequired is computed server-side from username+password
	// presence; never written directly.
	return out
}

// splitContactName splits "Jane Doe" → ("Jane", "Doe"). A single-token name
// goes in firstName with empty lastName.
func splitContactName(full string) (firstName, lastName string) {
	full = strings.TrimSpace(full)
	if i := strings.IndexByte(full, ' '); i >= 0 {
		return full[:i], strings.TrimSpace(full[i+1:])
	}
	return full, ""
}

// fetchAppStoreReviewDetail returns the existing detail, or zero values when Apple 404s (not yet created).
func fetchAppStoreReviewDetail(ctx context.Context, c *asc.Client, versionID string) (string, AppStoreReviewDetailAttributes, error) {
	resp, err := asc.Get[asc.Single[AppStoreReviewDetailAttributes]](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreReviewDetail", nil,
	)
	if err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return "", AppStoreReviewDetailAttributes{}, nil
		}
		return "", AppStoreReviewDetailAttributes{}, err
	}
	return resp.Data.ID, resp.Data.Attributes, nil
}

// diffReviewerDetail returns the changed-field delta; only non-nil desired fields are compared.
func diffReviewerDetail(current, desired AppStoreReviewDetailAttributes) (AppStoreReviewDetailAttributes, bool) {
	out := AppStoreReviewDetailAttributes{}
	changed := false

	if desired.ContactFirstName != nil && !strPtrEq(desired.ContactFirstName, current.ContactFirstName) {
		out.ContactFirstName = desired.ContactFirstName
		changed = true
	}
	if desired.ContactLastName != nil && !strPtrEq(desired.ContactLastName, current.ContactLastName) {
		out.ContactLastName = desired.ContactLastName
		changed = true
	}
	if desired.ContactEmail != nil && !strPtrEq(desired.ContactEmail, current.ContactEmail) {
		out.ContactEmail = desired.ContactEmail
		changed = true
	}
	if desired.ContactPhone != nil && !strPtrEq(desired.ContactPhone, current.ContactPhone) {
		out.ContactPhone = desired.ContactPhone
		changed = true
	}
	if desired.DemoAccountName != nil && !strPtrEq(desired.DemoAccountName, current.DemoAccountName) {
		out.DemoAccountName = desired.DemoAccountName
		changed = true
	}
	if desired.DemoAccountPassword != nil {
		// Apple never returns the password on read, so it can't be diffed
		// against current; password is always written through when supplied.
		out.DemoAccountPassword = desired.DemoAccountPassword
		changed = true
	}
	if desired.Notes != nil && !strPtrEq(desired.Notes, current.Notes) {
		out.Notes = desired.Notes
		changed = true
	}

	return out, changed
}

// strPtrEq compares two *string by value (nil == nil; nil ≠ &"").
func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// changedKeys returns the wire-name keys present in delta, sorted for stable JSON output.
func changedKeys(delta AppStoreReviewDetailAttributes) []string {
	var keys []string
	if delta.ContactFirstName != nil {
		keys = append(keys, "contactFirstName")
	}
	if delta.ContactLastName != nil {
		keys = append(keys, "contactLastName")
	}
	if delta.ContactEmail != nil {
		keys = append(keys, "contactEmail")
	}
	if delta.ContactPhone != nil {
		keys = append(keys, "contactPhone")
	}
	if delta.DemoAccountName != nil {
		keys = append(keys, "demoAccountName")
	}
	if delta.DemoAccountPassword != nil {
		keys = append(keys, "demoAccountPassword")
	}
	if delta.Notes != nil {
		keys = append(keys, "notes")
	}
	sortStrings(keys)
	return keys
}

// allChangedKeys is changedKeys for the create branch: same shape but
// implies every supplied key is "changed" (no current to diff against).
func allChangedKeys(desired AppStoreReviewDetailAttributes) []string {
	return changedKeys(desired)
}

// buildReviewerResult composes the result view; never copies the password, only DemoAccountPasswordSet.
func buildReviewerResult(action, id, typ, bundleID, version string, noop bool, changed []string, attrs AppStoreReviewDetailAttributes) *ReviewerDemoWriteResult {
	return &ReviewerDemoWriteResult{
		Action:                 action,
		ID:                     id,
		Type:                   typ,
		BundleID:               bundleID,
		VersionString:          version,
		NoOp:                   noop,
		ChangedKeys:            changed,
		ContactFirstName:       derefStr(attrs.ContactFirstName),
		ContactLastName:        derefStr(attrs.ContactLastName),
		ContactPhone:           derefStr(attrs.ContactPhone),
		ContactEmail:           derefStr(attrs.ContactEmail),
		DemoAccountName:        derefStr(attrs.DemoAccountName),
		DemoAccountPasswordSet: attrs.DemoAccountPassword != nil && *attrs.DemoAccountPassword != "",
		DemoAccountRequired:    attrs.DemoAccountRequired,
		Notes:                  derefStr(attrs.Notes),
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// redactReviewerError scrubs the password substring from any error before it reaches stderr.
// Defense-in-depth against Apple echoing it in a 4xx body or a wrap site concatenating it.
func redactReviewerError(err error, password string) error {
	if err == nil {
		return nil
	}
	if password == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, password) {
		return err
	}
	scrubbed := strings.ReplaceAll(msg, password, "[REDACTED-PASSWORD]")
	return errors.New(scrubbed)
}
