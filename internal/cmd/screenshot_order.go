package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// ScreenshotOrderResult is the outcome of an explicit complete-linkage
// screenshot reorder. It never uploads, deletes, or changes image bytes.
type ScreenshotOrderResult struct {
	BundleID      string   `json:"bundleId"`
	SetID         string   `json:"setId"`
	ScreenshotIDs []string `json:"screenshotIds"`
	Changed       bool     `json:"changed"`
}

func (r ScreenshotOrderResult) TableRows() (headers []string, rows [][]string) {
	return []string{"BUNDLE_ID", "SET_ID", "CHANGED", "SCREENSHOT_IDS"}, [][]string{{
		r.BundleID, r.SetID, strconv.FormatBool(r.Changed), strings.Join(r.ScreenshotIDs, ","),
	}}
}

// newScreenshotOrderCommand builds the unregistered screenshots reorder command.
// The lead-owned screenshots tree attaches it after the screenshots collection
// commands are registered.
func newScreenshotOrderCommand() *cobra.Command {
	var in screenshotOrderInput
	cmd := &cobra.Command{
		Use:          "reorder <bundleId>",
		Short:        "Replace one screenshot set's complete display order",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		Long: `Replace the complete ordered screenshot membership for one existing set.

The command re-reads the selected set immediately before PATCHing and rejects
missing, duplicate, or foreign screenshot IDs. It only changes relationship
order; it never uploads or re-encodes image bytes. Select either a normal App
Store version or an exact Custom Product Page version.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			return runScreenshotOrderWithClient(cmd, args, client, in, outputMode())
		},
	}
	cmd.Flags().StringVar(&in.version, "version", "", "App Store version string for a normal version target")
	cmd.Flags().StringVar(&in.platform, "platform", "IOS", "platform for --version (IOS|MAC_OS|TV_OS|VISION_OS)")
	cmd.Flags().StringVar(&in.locale, "locale", "", "BCP-47 locale code")
	cmd.Flags().StringVar(&in.deviceSet, "device-set", "", "ScreenshotDisplayType")
	cmd.Flags().StringVar(&in.customProductPage, "custom-product-page", "", "App Custom Product Page ID")
	cmd.Flags().StringVar(&in.customProductPageVersion, "custom-product-page-version", "", "exact App Custom Product Page version ID")
	cmd.Flags().StringArrayVar(&in.screenshotIDs, "screenshot", nil, "appScreenshot ID in final order (repeatable; complete set required)")
	return cmd
}

type screenshotOrderInput struct {
	version, platform, locale, deviceSet        string
	customProductPage, customProductPageVersion string
	screenshotIDs                               []string
}

func runScreenshotOrderWithClient(cmd *cobra.Command, args []string, client *asc.Client, input screenshotOrderInput, output string) error {
	if len(args) != 1 {
		return errors.New("screenshots reorder: exactly one bundleId is required")
	}
	in, err := validateScreenshotOrderInput(input)
	if err != nil {
		return err
	}
	appID, err := resolveAppID(cmd.Context(), client, args[0])
	if err != nil {
		return err
	}
	setID, err := resolveScreenshotOrderTarget(cmd.Context(), client, appID, in)
	if err != nil {
		return err
	}
	members, err := asc.ListAppScreenshots(cmd.Context(), client, setID)
	if err != nil {
		return fmt.Errorf("screenshots reorder: read fresh set membership: %w", err)
	}
	if err := validateScreenshotOrderMembership(in.screenshotIDs, members); err != nil {
		return err
	}

	changed := !sameScreenshotIDOrder(in.screenshotIDs, members)
	if changed {
		if err := asc.ReplaceAppScreenshotOrder(cmd.Context(), client, setID, in.screenshotIDs); err != nil {
			return err
		}
	}
	return renderTo(cmd.OutOrStdout(), ScreenshotOrderResult{
		BundleID: args[0], SetID: setID, ScreenshotIDs: in.screenshotIDs, Changed: changed,
	}, output, true)
}

func validateScreenshotOrderInput(input screenshotOrderInput) (screenshotOrderInput, error) {
	input.version = strings.TrimSpace(input.version)
	input.platform = strings.TrimSpace(input.platform)
	input.locale = strings.TrimSpace(input.locale)
	input.deviceSet = strings.TrimSpace(input.deviceSet)
	input.customProductPage = strings.TrimSpace(input.customProductPage)
	input.customProductPageVersion = strings.TrimSpace(input.customProductPageVersion)
	if input.locale == "" {
		return input, errors.New("screenshots reorder: --locale is required")
	}
	if !isValidDeviceSet(input.deviceSet) {
		return input, fmt.Errorf("screenshots reorder: --device-set %q is not a recognised ScreenshotDisplayType", input.deviceSet)
	}
	cppRequested := input.customProductPage != "" || input.customProductPageVersion != ""
	if cppRequested {
		if input.version != "" {
			return input, errors.New("screenshots reorder: --version cannot be combined with Custom Product Page flags")
		}
		if input.customProductPage == "" || input.customProductPageVersion == "" {
			return input, errors.New("screenshots reorder: both --custom-product-page and --custom-product-page-version are required together")
		}
	} else if input.version == "" {
		return input, errors.New("screenshots reorder: --version is required for a normal App Store version target")
	}
	if !cppRequested && input.platform == "" {
		return input, errors.New("screenshots reorder: --platform is required with --version")
	}
	if len(input.screenshotIDs) == 0 {
		return input, errors.New("screenshots reorder: at least one --screenshot is required")
	}
	for i := range input.screenshotIDs {
		input.screenshotIDs[i] = strings.TrimSpace(input.screenshotIDs[i])
		if input.screenshotIDs[i] == "" {
			return input, errors.New("screenshots reorder: --screenshot cannot be empty")
		}
	}
	return input, nil
}

func resolveScreenshotOrderTarget(ctx context.Context, c *asc.Client, appID string, in screenshotOrderInput) (string, error) {
	if in.customProductPage != "" {
		return resolveCPPScreenshotOrderTarget(ctx, c, appID, in)
	}
	versionID, err := exactAppStoreVersion(ctx, c, appID, in.version, in.platform)
	if err != nil {
		return "", err
	}
	localizationID, err := exactAppStoreVersionLocalization(ctx, c, versionID, in.locale)
	if err != nil {
		return "", err
	}
	return findExistingScreenshotSet(ctx, c, "appStoreVersionLocalizations", localizationID, in.deviceSet)
}

func exactAppStoreVersion(ctx context.Context, c *asc.Client, appID, versionString, platform string) (string, error) {
	path := "/v1/apps/" + url.PathEscape(appID) + "/appStoreVersions"
	query := url.Values{"filter[versionString]": {versionString}, "filter[platform]": {platform}, "limit": {"200"}}
	var id string
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, c, path, query) {
		if err != nil {
			return "", fmt.Errorf("screenshots reorder: list App Store versions: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "appStoreVersions" || row.ID == "" {
				return "", errors.New("screenshots reorder: app returned an invalid App Store version")
			}
			if row.Attributes.VersionString == versionString && row.Attributes.Platform == platform {
				if id != "" {
					return "", fmt.Errorf("screenshots reorder: App Store version %q (platform=%s) is ambiguous", versionString, platform)
				}
				id = row.ID
			}
		}
	}
	if id == "" {
		return "", fmt.Errorf("screenshots reorder: no version %q found (platform=%s)", versionString, platform)
	}
	return id, nil
}

func exactAppStoreVersionLocalization(ctx context.Context, c *asc.Client, versionID, locale string) (string, error) {
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/appStoreVersionLocalizations"
	var id string
	for page, err := range asc.Pages[metadataASCVersionLocalizationAttrs](ctx, c, path, url.Values{"filter[locale]": {locale}, "limit": {"200"}}) {
		if err != nil {
			return "", fmt.Errorf("screenshots reorder: list App Store version localizations: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "appStoreVersionLocalizations" || row.ID == "" {
				return "", errors.New("screenshots reorder: version returned an invalid localization")
			}
			if row.Attributes.Locale == locale {
				if id != "" {
					return "", fmt.Errorf("screenshots reorder: App Store version locale %q is ambiguous", locale)
				}
				id = row.ID
			}
		}
	}
	if id == "" {
		return "", fmt.Errorf("screenshots reorder: no appStoreVersionLocalization for locale %q", locale)
	}
	return id, nil
}

func resolveCPPScreenshotOrderTarget(ctx context.Context, c *asc.Client, appID string, in screenshotOrderInput) (string, error) {
	if err := requireCustomProductPageForApp(ctx, c, appID, in.customProductPage); err != nil {
		return "", err
	}
	versions, err := collectCustomProductPageVersions(ctx, c, in.customProductPage)
	if err != nil {
		return "", err
	}
	if !hasExactCPPVersion(versions, in.customProductPageVersion) {
		return "", fmt.Errorf("screenshots reorder: Custom Product Page version %q does not belong to page %q", in.customProductPageVersion, in.customProductPage)
	}
	localizations, err := collectCustomProductPageLocalizations(ctx, c, in.customProductPageVersion)
	if err != nil {
		return "", err
	}
	localizationID, err := exactCPPLocalization(localizations, in.locale)
	if err != nil {
		return "", err
	}
	return findExistingScreenshotSet(ctx, c, "appCustomProductPageLocalizations", localizationID, in.deviceSet)
}

func requireCustomProductPageForApp(ctx context.Context, c *asc.Client, appID, pageID string) error {
	found := 0
	path := "/v1/apps/" + url.PathEscape(appID) + "/appCustomProductPages"
	for page, err := range asc.Pages[asc.AppCustomProductPageAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return fmt.Errorf("screenshots reorder: list Custom Product Pages: %w", err)
		}
		for _, row := range page.Data {
			if row.Type != "appCustomProductPages" || row.ID == "" {
				return errors.New("screenshots reorder: app returned an invalid Custom Product Page")
			}
			if row.ID == pageID {
				found++
			}
		}
	}
	if found != 1 {
		return fmt.Errorf("screenshots reorder: Custom Product Page %q is not uniquely owned by app %q", pageID, appID)
	}
	return nil
}

func hasExactCPPVersion(versions []CustomProductPageVersionView, versionID string) bool {
	found := 0
	for _, version := range versions {
		if version.Type != "appCustomProductPageVersions" || version.ID == "" {
			return false
		}
		if version.ID == versionID {
			found++
		}
	}
	return found == 1
}

func exactCPPLocalization(localizations []CustomProductPageLocalizationView, locale string) (string, error) {
	var id string
	for _, localization := range localizations {
		if localization.Type != "appCustomProductPageLocalizations" || localization.ID == "" {
			return "", errors.New("screenshots reorder: Custom Product Page version returned an invalid localization")
		}
		if localization.Attributes.Locale == locale {
			if id != "" {
				return "", fmt.Errorf("screenshots reorder: Custom Product Page locale %q is ambiguous", locale)
			}
			id = localization.ID
		}
	}
	if id == "" {
		return "", fmt.Errorf("screenshots reorder: no Custom Product Page localization for locale %q", locale)
	}
	return id, nil
}

func findExistingScreenshotSet(ctx context.Context, c *asc.Client, parentType, parentID, deviceSet string) (string, error) {
	var path string
	switch parentType {
	case "appStoreVersionLocalizations":
		path = "/v1/appStoreVersionLocalizations/" + url.PathEscape(parentID) + "/appScreenshotSets"
	case "appCustomProductPageLocalizations":
		path = "/v1/appCustomProductPageLocalizations/" + url.PathEscape(parentID) + "/appScreenshotSets"
	default:
		return "", fmt.Errorf("screenshots reorder: unsupported screenshot parent type %q", parentType)
	}
	page, err := asc.Get[asc.Collection[screenshotSetAttrs]](ctx, c, path, url.Values{
		"filter[screenshotDisplayType]": {deviceSet},
		"limit":                         {"2"},
	})
	if err != nil {
		return "", err
	}
	if len(page.Data) != 1 || page.Data[0].Type != "appScreenshotSets" || page.Data[0].ID == "" || page.Data[0].Attributes.ScreenshotDisplayType != deviceSet {
		return "", fmt.Errorf("screenshots reorder: expected exactly one %s screenshot set for %q", deviceSet, parentID)
	}
	return page.Data[0].ID, nil
}

func validateScreenshotOrderMembership(desired []string, members []asc.AppScreenshot) error {
	if len(desired) != len(members) {
		return fmt.Errorf("screenshots reorder: supplied %d screenshot IDs, but fresh target membership has %d", len(desired), len(members))
	}
	membersByID := make(map[string]struct{}, len(members))
	for _, member := range members {
		if _, duplicate := membersByID[member.ID]; duplicate {
			return fmt.Errorf("screenshots reorder: fresh target membership repeats screenshot %q", member.ID)
		}
		membersByID[member.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(desired))
	for _, id := range desired {
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("screenshots reorder: screenshot %q was specified more than once", id)
		}
		if _, owned := membersByID[id]; !owned {
			return fmt.Errorf("screenshots reorder: screenshot %q is not in the fresh target membership", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func sameScreenshotIDOrder(desired []string, members []asc.AppScreenshot) bool {
	if len(desired) != len(members) {
		return false
	}
	for i := range desired {
		if desired[i] != members[i].ID {
			return false
		}
	}
	return true
}
