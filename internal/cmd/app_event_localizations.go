package cmd

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type AppEventLocalizationsResult struct {
	Localizations []asc.AppEventLocalizationView `json:"localizations"`
}

func (r AppEventLocalizationsResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Localizations))
	for _, l := range r.Localizations {
		rows = append(rows, []string{l.ID, l.Attributes.Locale, l.Attributes.Name})
	}
	return []string{"ID", "LOCALE", "NAME"}, rows
}

type AppEventLocalizationActionResult struct {
	Action       string                        `json:"action"`
	ID           string                        `json:"id"`
	Changed      bool                          `json:"changed"`
	Localization *asc.AppEventLocalizationView `json:"localization,omitempty"`
}

func (r AppEventLocalizationActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "CHANGED"}, [][]string{{r.Action, r.ID, strconv.FormatBool(r.Changed)}}
}

func newEventLocalizationsCommand() *cobra.Command {
	root := &cobra.Command{Use: "localizations", Short: "Manage event localizations"}
	root.AddCommand(newEventLocalizationsListCommand(), newEventLocalizationsGetCommand(), newEventLocalizationsCreateCommand(), newEventLocalizationsUpdateCommand(), newEventLocalizationsDeleteCommand())
	return root
}
func localizationTargetFlags(cmd *cobra.Command) {
	eventTargetFlags(cmd)
	cmd.Flags().String("localization", "", "event localization ID")
}
func localizationTarget(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("localization")
	if id == "" {
		return "", errors.New("--localization is required")
	}
	return id, nil
}
func localizationFields(cmd *cobra.Command) {
	cmd.Flags().String("locale", "", "locale code")
	cmd.Flags().String("name", "", "event display name")
	cmd.Flags().String("short-description", "", "event card description")
	cmd.Flags().String("long-description", "", "event details description")
}
func localizationInput(cmd *cobra.Command, base asc.AppEventLocalizationAttributes, create bool) (asc.AppEventLocalizationAttributes, map[string]any, error) {
	patch := map[string]any{}
	for _, f := range []struct{ flag, wire string }{{"locale", "locale"}, {"name", "name"}, {"short-description", "shortDescription"}, {"long-description", "longDescription"}} {
		if cmd.Flags().Changed(f.flag) {
			v, _ := cmd.Flags().GetString(f.flag)
			patch[f.wire] = v
			switch f.wire {
			case "locale":
				base.Locale = v
			case "name":
				base.Name = v
			case "shortDescription":
				base.ShortDescription = v
			case "longDescription":
				base.LongDescription = v
			}
		}
	}
	if create && patch["locale"] == nil {
		return base, nil, errors.New("create localization requires --locale")
	}
	if err := asc.ValidateAppEventLocalization(base); err != nil {
		return base, nil, err
	}
	return base, patch, nil
}
func eventLocalizationContext(cmd *cobra.Command, bundle string) (*asc.Client, string, error) {
	eventID, err := eventTarget(cmd)
	if err != nil {
		return nil, "", err
	}
	c, appID, err := eventClient(cmd.Context(), bundle)
	if err != nil {
		return nil, "", err
	}
	if _, err := asc.FindAppEvent(cmd.Context(), c, appID, eventID); err != nil {
		return nil, "", err
	}
	return c, eventID, nil
}
func draftEventContext(cmd *cobra.Command, bundle string) (*asc.Client, string, error) {
	c, eventID, err := eventLocalizationContext(cmd, bundle)
	if err != nil {
		return nil, "", err
	}
	event, err := asc.GetAppEvent(cmd.Context(), c, eventID)
	if err != nil {
		return nil, "", err
	}
	if event.Attributes.EventState != "DRAFT" {
		return nil, "", errors.New("event localizations can be changed only in DRAFT")
	}
	return c, eventID, nil
}
func newEventLocalizationsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Short: "List every localization for an owned event", Args: cobra.ExactArgs(1)}
	eventTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, eventID, err := eventLocalizationContext(cmd, args[0])
		if err != nil {
			return err
		}
		items, err := asc.ListAppEventLocalizations(cmd.Context(), c, eventID)
		if err != nil {
			return err
		}
		return Render(AppEventLocalizationsResult{Localizations: items}, outputMode())
	}
	return cmd
}
func newEventLocalizationsGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Short: "Read one event localization", Args: cobra.ExactArgs(1)}
	localizationTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := localizationTarget(cmd)
		if err != nil {
			return err
		}
		c, eventID, err := eventLocalizationContext(cmd, args[0])
		if err != nil {
			return err
		}
		loc, err := asc.FindAppEventLocalization(cmd.Context(), c, eventID, id)
		if err != nil {
			return err
		}
		return Render(AppEventLocalizationActionResult{Action: "get", ID: id, Localization: &loc}, outputMode())
	}
	return cmd
}
func newEventLocalizationsCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Short: "Create a draft event localization", Args: cobra.ExactArgs(1)}
	eventTargetFlags(cmd)
	localizationFields(cmd)
	cmd.Flags().Bool("confirm", false, "confirm localization creation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		attrs, _, err := localizationInput(cmd, asc.AppEventLocalizationAttributes{}, true)
		if err != nil {
			return err
		}
		c, eventID, err := draftEventContext(cmd, args[0])
		if err != nil {
			return err
		}
		loc, changed, err := asc.CreateAppEventLocalization(cmd.Context(), c, eventID, attrs)
		if err != nil {
			return err
		}
		return Render(AppEventLocalizationActionResult{Action: "create", ID: loc.ID, Changed: changed, Localization: &loc}, outputMode())
	}
	return cmd
}
func newEventLocalizationsUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update <bundleId>", Short: "Update draft event localization text", Args: cobra.ExactArgs(1)}
	localizationTargetFlags(cmd)
	localizationFields(cmd)
	cmd.Flags().Bool("confirm", false, "confirm localization update")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		id, err := localizationTarget(cmd)
		if err != nil {
			return err
		}
		c, eventID, err := draftEventContext(cmd, args[0])
		if err != nil {
			return err
		}
		current, err := asc.FindAppEventLocalization(cmd.Context(), c, eventID, id)
		if err != nil {
			return err
		}
		attrs, patch, err := localizationInput(cmd, current.Attributes, false)
		if err != nil {
			return err
		}
		if attrs == current.Attributes {
			return Render(AppEventLocalizationActionResult{Action: "skipped", ID: id, Localization: &current}, outputMode())
		}
		loc, changed, err := asc.UpdateAppEventLocalization(cmd.Context(), c, eventID, id, patch)
		if err != nil {
			return err
		}
		return Render(AppEventLocalizationActionResult{Action: "update", ID: id, Changed: changed, Localization: &loc}, outputMode())
	}
	return cmd
}
func newEventLocalizationsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Short: "Delete a draft event localization", Args: cobra.ExactArgs(1)}
	localizationTargetFlags(cmd)
	cmd.Flags().Bool("confirm", false, "confirm localization deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		id, err := localizationTarget(cmd)
		if err != nil {
			return err
		}
		c, eventID, err := draftEventContext(cmd, args[0])
		if err != nil {
			return err
		}
		if err := asc.DeleteAppEventLocalization(cmd.Context(), c, eventID, id); err != nil {
			return err
		}
		return Render(AppEventLocalizationActionResult{Action: "delete", ID: id, Changed: true}, outputMode())
	}
	return cmd
}
