package cmd

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ul0gic/flightline/internal/asc"
)

type ScreenshotListEntry struct {
	FileName string `json:"fileName"`
	State    string `json:"state,omitempty"`
}

type ScreenshotListSet struct {
	DeviceSet   string                `json:"deviceSet"`
	Screenshots []ScreenshotListEntry `json:"screenshots"`
}

type ScreenshotListLocale struct {
	Locale string              `json:"locale"`
	Sets   []ScreenshotListSet `json:"sets"`
}

type ScreenshotListResult struct {
	BundleID string                 `json:"bundleId"`
	Version  string                 `json:"version"`
	Locales  []ScreenshotListLocale `json:"locales"`
}

func (r *ScreenshotListResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"LOCALE", "DEVICE_SET", "COUNT", "FILE_NAMES"}
	for _, loc := range r.Locales {
		if len(loc.Sets) == 0 {
			rows = append(rows, []string{loc.Locale, "(none)", "0", ""})
			continue
		}
		for _, set := range loc.Sets {
			names := make([]string, 0, len(set.Screenshots))
			for _, s := range set.Screenshots {
				names = append(names, s.FileName)
			}
			rows = append(rows, []string{
				loc.Locale, set.DeviceSet, strconv.Itoa(len(set.Screenshots)), strings.Join(names, ", "),
			})
		}
	}
	return headers, rows
}

var screenshotsListCmd = &cobra.Command{
	Use:          "list <bundleId>",
	Short:        "List live screenshot sets and files per locale",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runScreenshotsList,
	Example: `  flightline screenshots list com.example.myapp --version 1.0.1
  flightline screenshots list com.example.myapp --version 1.0.1 --locale en-US
  flightline screenshots list com.example.myapp --version 1.0.1 --output json | jq '.locales'`,
}

var (
	screenshotsListVersion  string
	screenshotsListPlatform string
	screenshotsListLocale   string
)

func init() {
	screenshotsListCmd.Flags().StringVar(&screenshotsListVersion, "version", "", "App Store version string (e.g. 1.0.1)")
	screenshotsListCmd.Flags().StringVar(&screenshotsListPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	screenshotsListCmd.Flags().StringVar(&screenshotsListLocale, "locale", "", "restrict to one BCP-47 locale (e.g. en-US)")
	_ = screenshotsListCmd.MarkFlagRequired("version")
	screenshotsCmd.AddCommand(screenshotsListCmd)
}

func runScreenshotsList(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	c, err := newClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return err
	}
	versionView, err := lookupVersion(ctx, c, appID, screenshotsListVersion, screenshotsListPlatform)
	if err != nil {
		return err
	}
	if versionView == nil {
		return fmt.Errorf("screenshots: no version %q found for %q (platform=%s)", screenshotsListVersion, bundleID, screenshotsListPlatform)
	}

	locales, err := listVersionLocales(ctx, c, versionView.ID)
	if err != nil {
		return err
	}

	result := &ScreenshotListResult{BundleID: bundleID, Version: screenshotsListVersion, Locales: []ScreenshotListLocale{}}
	for _, loc := range locales {
		if screenshotsListLocale != "" && loc.locale != screenshotsListLocale {
			continue
		}
		sets, err := listScreenshotSets(ctx, c, loc.id)
		if err != nil {
			return err
		}
		result.Locales = append(result.Locales, ScreenshotListLocale{Locale: loc.locale, Sets: sets})
	}
	if screenshotsListLocale != "" && len(result.Locales) == 0 {
		return fmt.Errorf("screenshots: no appStoreVersionLocalization for locale %q under version %s", screenshotsListLocale, screenshotsListVersion)
	}
	sort.SliceStable(result.Locales, func(i, j int) bool { return result.Locales[i].Locale < result.Locales[j].Locale })
	return Render(result, outputMode())
}

type versionLocaleRef struct {
	id, locale string
}

func listVersionLocales(ctx context.Context, c *asc.Client, versionID string) ([]versionLocaleRef, error) {
	type locAttrs struct {
		Locale string `json:"locale,omitempty"`
	}
	q := url.Values{"limit": {"50"}}
	out := make([]versionLocaleRef, 0, 8)
	for page, err := range asc.Pages[locAttrs](
		ctx, c, "/v1/appStoreVersions/"+versionID+"/appStoreVersionLocalizations", q,
	) {
		if err != nil {
			return nil, fmt.Errorf("screenshots: list version localizations: %w", err)
		}
		for _, r := range page.Data {
			out = append(out, versionLocaleRef{id: r.ID, locale: r.Attributes.Locale})
		}
	}
	return out, nil
}

func listScreenshotSets(ctx context.Context, c *asc.Client, versionLocID string) ([]ScreenshotListSet, error) {
	type setAttrs struct {
		ScreenshotDisplayType string `json:"screenshotDisplayType,omitempty"`
	}
	q := url.Values{"limit": {"50"}}
	out := make([]ScreenshotListSet, 0, 4)
	for page, err := range asc.Pages[setAttrs](
		ctx, c, "/v1/appStoreVersionLocalizations/"+versionLocID+"/appScreenshotSets", q,
	) {
		if err != nil {
			return nil, fmt.Errorf("screenshots: list screenshot sets: %w", err)
		}
		for _, set := range page.Data {
			shots, err := listSetScreenshots(ctx, c, set.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, ScreenshotListSet{DeviceSet: set.Attributes.ScreenshotDisplayType, Screenshots: shots})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DeviceSet < out[j].DeviceSet })
	return out, nil
}

func listSetScreenshots(ctx context.Context, c *asc.Client, setID string) ([]ScreenshotListEntry, error) {
	type shotAttrs struct {
		FileName           string `json:"fileName,omitempty"`
		AssetDeliveryState *struct {
			State string `json:"state,omitempty"`
		} `json:"assetDeliveryState,omitempty"`
	}
	q := url.Values{"limit": {"200"}}
	out := make([]ScreenshotListEntry, 0, 10)
	for page, err := range asc.Pages[shotAttrs](
		ctx, c, "/v1/appScreenshotSets/"+url.PathEscape(setID)+"/appScreenshots", q,
	) {
		if err != nil {
			return nil, fmt.Errorf("screenshots: list screenshots in set: %w", err)
		}
		for _, s := range page.Data {
			entry := ScreenshotListEntry{FileName: s.Attributes.FileName}
			if s.Attributes.AssetDeliveryState != nil {
				entry.State = s.Attributes.AssetDeliveryState.State
			}
			out = append(out, entry)
		}
	}
	return out, nil
}
