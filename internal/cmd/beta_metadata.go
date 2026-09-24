package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type BetaAppLocalizationsResult struct {
	BundleID      string                                            `json:"bundleId"`
	Localizations []asc.Resource[asc.BetaAppLocalizationAttributes] `json:"localizations"`
}

func (r BetaAppLocalizationsResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"LOCALE", "DESCRIPTION", "FEEDBACK_EMAIL", "ID"}
	for _, loc := range r.Localizations {
		rows = append(rows, []string{loc.Attributes.Locale, loc.Attributes.Description, loc.Attributes.FeedbackEmail, loc.ID})
	}
	return headers, rows
}

type BetaBuildLocalizationsResult struct {
	BundleID      string                                              `json:"bundleId"`
	BuildID       string                                              `json:"buildId"`
	BuildNumber   string                                              `json:"buildNumber"`
	Localizations []asc.Resource[asc.BetaBuildLocalizationAttributes] `json:"localizations"`
}

func (r BetaBuildLocalizationsResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"LOCALE", "WHATS_NEW", "ID"}
	for _, loc := range r.Localizations {
		rows = append(rows, []string{loc.Attributes.Locale, loc.Attributes.WhatsNew, loc.ID})
	}
	return headers, rows
}

type BetaReviewDetailsResult struct {
	BundleID string                                           `json:"bundleId"`
	Detail   *asc.Resource[asc.BetaAppReviewDetailAttributes] `json:"detail"`
}

func (r BetaReviewDetailsResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	if r.Detail == nil {
		return headers, [][]string{{"STATUS", "(none)"}}
	}
	a := r.Detail.Attributes
	return headers, [][]string{
		{"ID", r.Detail.ID},
		{"CONTACT_FIRST_NAME", a.ContactFirstName},
		{"CONTACT_LAST_NAME", a.ContactLastName},
		{"CONTACT_PHONE", a.ContactPhone},
		{"CONTACT_EMAIL", a.ContactEmail},
		{"DEMO_ACCOUNT_NAME", a.DemoAccountName},
		{"DEMO_ACCOUNT_REQUIRED", boolPtrStr(a.DemoAccountRequired)},
		{"NOTES", a.Notes},
	}
}

func newBetaMetadataCommand() *cobra.Command {
	root := &cobra.Command{Use: "metadata", Short: "Inspect TestFlight app copy, build notes, and beta review details"}
	root.AddCommand(newBetaAppLocalizationsListCommand(), newBetaBuildLocalizationsListCommand(), newBetaReviewDetailsGetCommand(),
		newBetaAppLocalizationSetCommand(), newBetaBuildLocalizationSetCommand(), newBetaReviewDetailsSetCommand())
	return root
}

func newBetaAppLocalizationsListCommand() *cobra.Command {
	return &cobra.Command{Use: "app-localizations <bundleId>", Short: "List TestFlight app localizations", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		localizations, err := asc.ListBetaAppLocalizations(cmd.Context(), c, appID)
		if err != nil {
			return err
		}
		if localizations == nil {
			localizations = []asc.Resource[asc.BetaAppLocalizationAttributes]{}
		}
		return Render(BetaAppLocalizationsResult{BundleID: args[0], Localizations: localizations}, outputMode())
	}}
}

func newBetaBuildLocalizationsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "build-localizations <bundleId>", Short: "List TestFlight build notes", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("build", "", "build number (CFBundleVersion)")
	cmd.Flags().String("version", "", "release version to disambiguate a build number")
	cmd.Flags().String("platform", "IOS", "platform to disambiguate a build number")
	_ = cmd.MarkFlagRequired("build")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		build, err := cmd.Flags().GetString("build")
		if err != nil {
			return err
		}
		if build == "" {
			return errors.New("build-localizations: --build is required")
		}
		version, err := cmd.Flags().GetString("version")
		if err != nil {
			return err
		}
		platform, err := cmd.Flags().GetString("platform")
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		buildID, err := resolveBuildIDWithOptions(cmd.Context(), c, appID, args[0], build, buildLookupOptions{ReleaseVersion: version, Platform: platform})
		if err != nil {
			return err
		}
		localizations, err := asc.ListBetaBuildLocalizations(cmd.Context(), c, buildID)
		if err != nil {
			return err
		}
		if localizations == nil {
			localizations = []asc.Resource[asc.BetaBuildLocalizationAttributes]{}
		}
		return Render(BetaBuildLocalizationsResult{BundleID: args[0], BuildID: buildID, BuildNumber: build, Localizations: localizations}, outputMode())
	}
	return cmd
}

func newBetaReviewDetailsGetCommand() *cobra.Command {
	return &cobra.Command{Use: "review-details <bundleId>", Short: "Read TestFlight beta review contact and demo details", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		detail, err := asc.GetBetaAppReviewDetail(cmd.Context(), c, appID)
		if err != nil {
			return err
		}
		return Render(BetaReviewDetailsResult{BundleID: args[0], Detail: detail}, outputMode())
	}}
}

type BetaMetadataWriteResult struct {
	Action      string `json:"action"`
	BundleID    string `json:"bundleId"`
	BuildID     string `json:"buildId,omitempty"`
	Locale      string `json:"locale,omitempty"`
	ID          string `json:"id,omitempty"`
	Changed     bool   `json:"changed"`
	PasswordSet bool   `json:"passwordSet,omitempty"`
}

func (r BetaMetadataWriteResult) TableRows() (headers []string, rows [][]string) {
	return []string{"FIELD", "VALUE"}, [][]string{
		{"ACTION", r.Action}, {"BUNDLE_ID", r.BundleID}, {"BUILD_ID", r.BuildID},
		{"LOCALE", r.Locale}, {"ID", r.ID}, {"CHANGED", strconv.FormatBool(r.Changed)},
		{"PASSWORD_SET", strconv.FormatBool(r.PasswordSet)},
	}
}

func newBetaAppLocalizationSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set-app-localization <bundleId>", Short: "Create or update TestFlight app copy for one locale", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("locale", "", "locale to create or update")
	for name, help := range map[string]string{
		"description": "beta app description", "feedback-email": "tester feedback email",
		"marketing-url": "TestFlight marketing URL", "privacy-policy-url": "TestFlight privacy URL",
		"tvos-privacy-policy": "tvOS privacy policy",
	} {
		cmd.Flags().String(name, "", help)
	}
	_ = cmd.MarkFlagRequired("locale")
	cmd.RunE = runBetaAppLocalizationSet
	return cmd
}

func runBetaAppLocalizationSet(cmd *cobra.Command, args []string) error {
	locale, err := cmd.Flags().GetString("locale")
	if err != nil {
		return err
	}
	if strings.TrimSpace(locale) == "" {
		return errors.New("set-app-localization: --locale is required")
	}
	fields := map[string]string{"description": "description", "feedback-email": "feedbackEmail", "marketing-url": "marketingUrl", "privacy-policy-url": "privacyPolicyUrl", "tvos-privacy-policy": "tvOsPrivacyPolicy"}
	desired, err := betaChangedStringFlags(cmd, fields)
	if err != nil {
		return err
	}
	if len(desired) == 0 {
		return errors.New("set-app-localization: specify at least one metadata field")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, args[0])
	if err != nil {
		return err
	}
	rows, err := asc.ListBetaAppLocalizations(cmd.Context(), c, appID)
	if err != nil {
		return err
	}
	var current *asc.Resource[asc.BetaAppLocalizationAttributes]
	for i := range rows {
		if rows[i].Attributes.Locale == locale {
			if current != nil {
				return fmt.Errorf("app %s has multiple beta app localizations for %s", args[0], locale)
			}
			current = &rows[i]
		}
	}
	result := BetaMetadataWriteResult{Action: "set-app-localization", BundleID: args[0], Locale: locale}
	if current == nil {
		row, err := asc.CreateBetaAppLocalization(cmd.Context(), c, appID, locale, desired)
		if err != nil {
			return err
		}
		result.ID, result.Changed = row.ID, true
	} else {
		result.ID = current.ID
		betaPruneAppLocalizationDelta(desired, current.Attributes)
		if len(desired) != 0 {
			if _, err := asc.UpdateBetaAppLocalization(cmd.Context(), c, current.ID, desired); err != nil {
				return err
			}
			result.Changed = true
		}
	}
	return Render(result, outputMode())
}

func betaPruneAppLocalizationDelta(delta map[string]any, a asc.BetaAppLocalizationAttributes) {
	current := map[string]string{"description": a.Description, "feedbackEmail": a.FeedbackEmail,
		"marketingUrl": a.MarketingURL, "privacyPolicyUrl": a.PrivacyPolicyURL, "tvOsPrivacyPolicy": a.TVOSPrivacyPolicy}
	for name, value := range delta {
		if value == current[name] {
			delete(delta, name)
		}
	}
}

func betaChangedStringFlags(cmd *cobra.Command, names map[string]string) (map[string]any, error) {
	attributes := make(map[string]any)
	for flag, field := range names {
		if !cmd.Flags().Changed(flag) {
			continue
		}
		value, err := cmd.Flags().GetString(flag)
		if err != nil {
			return nil, err
		}
		attributes[field] = value
	}
	return attributes, nil
}

func newBetaBuildLocalizationSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set-build-localization <bundleId>", Short: "Create or update TestFlight what's-new text", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("build", "", "build number (CFBundleVersion)")
	cmd.Flags().String("version", "", "release version to disambiguate a build number")
	cmd.Flags().String("platform", "IOS", "platform to disambiguate a build number")
	cmd.Flags().String("locale", "", "locale to create or update")
	cmd.Flags().String("whats-new", "", "what testers should test")
	_ = cmd.MarkFlagRequired("build")
	_ = cmd.MarkFlagRequired("locale")
	_ = cmd.MarkFlagRequired("whats-new")
	cmd.RunE = runBetaBuildLocalizationSet
	return cmd
}

func runBetaBuildLocalizationSet(cmd *cobra.Command, args []string) error {
	build, _ := cmd.Flags().GetString("build")
	version, _ := cmd.Flags().GetString("version")
	platform, _ := cmd.Flags().GetString("platform")
	locale, _ := cmd.Flags().GetString("locale")
	whatsNew, _ := cmd.Flags().GetString("whats-new")
	if strings.TrimSpace(build) == "" || strings.TrimSpace(locale) == "" || strings.TrimSpace(whatsNew) == "" {
		return errors.New("set-build-localization requires --build, --locale, and nonempty --whats-new")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), c, args[0])
	if err != nil {
		return err
	}
	buildID, err := resolveBuildIDWithOptions(cmd.Context(), c, appID, args[0], build, buildLookupOptions{ReleaseVersion: version, Platform: platform})
	if err != nil {
		return err
	}
	rows, err := asc.ListBetaBuildLocalizations(cmd.Context(), c, buildID)
	if err != nil {
		return err
	}
	var current *asc.Resource[asc.BetaBuildLocalizationAttributes]
	for i := range rows {
		if rows[i].Attributes.Locale == locale {
			if current != nil {
				return fmt.Errorf("build %s has multiple beta build localizations for %s", build, locale)
			}
			current = &rows[i]
		}
	}
	result := BetaMetadataWriteResult{Action: "set-build-localization", BundleID: args[0], BuildID: buildID, Locale: locale}
	if current == nil {
		row, err := asc.CreateBetaBuildLocalization(cmd.Context(), c, buildID, locale, map[string]any{"whatsNew": whatsNew})
		if err != nil {
			return err
		}
		result.ID, result.Changed = row.ID, true
	} else {
		result.ID = current.ID
		if current.Attributes.WhatsNew != whatsNew {
			if _, err := asc.UpdateBetaBuildLocalization(cmd.Context(), c, current.ID, map[string]any{"whatsNew": whatsNew}); err != nil {
				return err
			}
			result.Changed = true
		}
	}
	return Render(result, outputMode())
}

func newBetaReviewDetailsSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set-review-details <bundleId>", Short: "Update TestFlight beta review contact and demo details", Args: cobra.ExactArgs(1)}
	for name, help := range map[string]string{
		"contact-first-name": "beta reviewer contact first name", "contact-last-name": "beta reviewer contact last name",
		"contact-phone": "beta reviewer contact phone", "contact-email": "beta reviewer contact email",
		"demo-account-name": "TestFlight demo account name", "notes": "TestFlight beta review notes",
	} {
		cmd.Flags().String(name, "", help)
	}
	cmd.Flags().Bool("demo-account-required", false, "whether beta review requires a demo account")
	cmd.Flags().String("password-ref", "", "env:NAME reference for beta demo password; never emitted")
	cmd.Flags().String("password-file", "", "file containing beta demo password; never emitted")
	cmd.RunE = runBetaReviewDetailsSet
	return cmd
}

func runBetaReviewDetailsSet(cmd *cobra.Command, args []string) error {
	fields := map[string]string{"contact-first-name": "contactFirstName", "contact-last-name": "contactLastName",
		"contact-phone": "contactPhone", "contact-email": "contactEmail", "demo-account-name": "demoAccountName", "notes": "notes"}
	desired, err := betaChangedStringFlags(cmd, fields)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("demo-account-required") {
		value, err := cmd.Flags().GetBool("demo-account-required")
		if err != nil {
			return err
		}
		desired["demoAccountRequired"] = value
	}
	password, supplied, err := betaReviewPassword(cmd)
	if err != nil {
		return err
	}
	if supplied {
		desired["demoAccountPassword"] = password
	}
	if len(desired) == 0 {
		return errors.New("set-review-details: specify at least one field")
	}
	c, err := newClient()
	if err != nil {
		return redactReviewerError(err, password)
	}
	appID, err := resolveAppID(cmd.Context(), c, args[0])
	if err != nil {
		return redactReviewerError(err, password)
	}
	current, err := asc.GetBetaAppReviewDetail(cmd.Context(), c, appID)
	if err != nil {
		return redactReviewerError(err, password)
	}
	if current == nil {
		return errors.New("set-review-details: Apple has no beta app review detail for this app; create one in App Store Connect first")
	}
	betaPruneReviewDetailDelta(desired, current.Attributes)
	result := BetaMetadataWriteResult{Action: "set-review-details", BundleID: args[0], ID: current.ID, PasswordSet: supplied}
	if len(desired) != 0 {
		if _, err := asc.UpdateBetaAppReviewDetail(cmd.Context(), c, current.ID, desired); err != nil {
			return redactReviewerError(err, password)
		}
		result.Changed = true
	}
	return Render(result, outputMode())
}

func betaPruneReviewDetailDelta(delta map[string]any, a asc.BetaAppReviewDetailAttributes) {
	current := map[string]string{"contactFirstName": a.ContactFirstName, "contactLastName": a.ContactLastName,
		"contactPhone": a.ContactPhone, "contactEmail": a.ContactEmail, "demoAccountName": a.DemoAccountName, "notes": a.Notes}
	for name, value := range delta {
		if name == "demoAccountPassword" {
			continue
		}
		if name == "demoAccountRequired" {
			if a.DemoAccountRequired != nil && value == *a.DemoAccountRequired {
				delete(delta, name)
			}
			continue
		}
		if value == current[name] {
			delete(delta, name)
		}
	}
}

func betaReviewPassword(cmd *cobra.Command) (password string, supplied bool, err error) {
	ref, err := cmd.Flags().GetString("password-ref")
	if err != nil {
		return "", false, err
	}
	file, err := cmd.Flags().GetString("password-file")
	if err != nil {
		return "", false, err
	}
	if cmd.Flags().Changed("password-ref") && cmd.Flags().Changed("password-file") {
		return "", false, errors.New("set-review-details: --password-ref and --password-file are mutually exclusive")
	}
	if cmd.Flags().Changed("password-ref") {
		return betaPasswordFromEnv(ref)
	}
	if cmd.Flags().Changed("password-file") {
		return betaPasswordFromFile(file)
	}
	return "", false, nil
}

func betaPasswordFromEnv(ref string) (password string, supplied bool, err error) {
	if !strings.HasPrefix(ref, "env:") || len(ref) <= 4 {
		return "", false, errors.New("set-review-details: --password-ref must be env:NAME")
	}
	name := strings.TrimPrefix(ref, "env:")
	if name[0] < 'A' || name[0] > 'Z' && name[0] != '_' {
		return "", false, errors.New("set-review-details: --password-ref must start with an uppercase letter or underscore")
	}
	for _, r := range name {
		if r < 'A' || r > 'Z' && (r < '0' || r > '9') && r != '_' {
			return "", false, errors.New("set-review-details: --password-ref must be env:NAME with uppercase letters, digits, and underscores")
		}
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return "", false, errors.New("set-review-details: referenced password environment variable is unset or empty")
	}
	return value, true, nil
}

func betaPasswordFromFile(file string) (password string, supplied bool, err error) {
	if file == "" {
		return "", false, errors.New("set-review-details: --password-file needs a path")
	}
	buf, err := os.ReadFile(file) //nolint:gosec // caller supplies the secret file
	if err != nil {
		return "", false, fmt.Errorf("set-review-details: read --password-file: %w", err)
	}
	value := strings.TrimSuffix(string(buf), "\n")
	if value == "" {
		return "", false, errors.New("set-review-details: password file is empty")
	}
	return value, true, nil
}
