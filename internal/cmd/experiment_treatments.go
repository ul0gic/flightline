package cmd

import (
	"context"
	"errors"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ExperimentTreatmentListResult struct {
	Treatments []asc.ExperimentTreatment `json:"treatments"`
}

func (r ExperimentTreatmentListResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Treatments))
	for i := range r.Treatments {
		x := &r.Treatments[i]
		rows = append(rows, []string{x.Name, x.AppIconName, x.ID})
	}
	return []string{"NAME", "APP_ICON", "ID"}, rows
}

type ExperimentLocalizationListResult struct {
	Localizations []asc.ExperimentLocalization `json:"localizations"`
}

func (r ExperimentLocalizationListResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Localizations))
	for i := range r.Localizations {
		x := &r.Localizations[i]
		rows = append(rows, []string{x.Locale, x.ID})
	}
	return []string{"LOCALE", "ID"}, rows
}

type ExperimentTreatmentAction struct {
	Action    string                  `json:"action"`
	Treatment asc.ExperimentTreatment `json:"treatment"`
	Changed   bool                    `json:"changed"`
}

func (r ExperimentTreatmentAction) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "NAME", "ID", "CHANGED"}, [][]string{{r.Action, r.Treatment.Name, r.Treatment.ID, strconv.FormatBool(r.Changed)}}
}

type ExperimentLocalizationAction struct {
	Action       string                     `json:"action"`
	Localization asc.ExperimentLocalization `json:"localization"`
	Changed      bool                       `json:"changed"`
}

func (r ExperimentLocalizationAction) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "LOCALE", "ID", "CHANGED"}, [][]string{{r.Action, r.Localization.Locale, r.Localization.ID, strconv.FormatBool(r.Changed)}}
}

func experimentTreatmentFlags(cmd *cobra.Command) {
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	cmd.Flags().String("treatment", "", "treatment ID")
}
func experimentLocalizationFlags(cmd *cobra.Command) {
	experimentTreatmentFlags(cmd)
	cmd.Flags().String("locale", "", "treatment locale")
}

func selectedExperimentTreatment(ctx context.Context, c *asc.Client, cmd *cobra.Command, bundle string) (asc.ProductExperiment, asc.ExperimentTreatment, error) {
	appID, versionID, platform, err := resolveExperimentTarget(ctx, c, cmd, bundle)
	if err != nil {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, err
	}
	expID, _ := cmd.Flags().GetString("experiment")
	exp, err := selectedProductExperiment(ctx, c, appID, versionID, platform, expID)
	if err != nil {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, err
	}
	treatmentID, _ := cmd.Flags().GetString("treatment")
	if treatmentID == "" {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, errors.New("--treatment is required")
	}
	treatment, err := asc.GetExperimentTreatment(ctx, c, exp.ID, treatmentID)
	return exp, treatment, err
}

func selectedExperimentLocalization(ctx context.Context, c *asc.Client, cmd *cobra.Command, bundle string) (asc.ProductExperiment, asc.ExperimentTreatment, asc.ExperimentLocalization, error) {
	exp, treatment, err := selectedExperimentTreatment(ctx, c, cmd, bundle)
	if err != nil {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, asc.ExperimentLocalization{}, err
	}
	locale, _ := cmd.Flags().GetString("locale")
	if locale == "" {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, asc.ExperimentLocalization{}, errors.New("--locale is required")
	}
	list, err := asc.ListExperimentLocalizations(ctx, c, treatment.ID)
	if err != nil {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, asc.ExperimentLocalization{}, err
	}
	var selected *asc.ExperimentLocalization
	for i := range list {
		if list[i].Locale == locale {
			if selected != nil {
				return asc.ProductExperiment{}, asc.ExperimentTreatment{}, asc.ExperimentLocalization{}, errors.New("duplicate treatment locale")
			}
			selected = &list[i]
		}
	}
	if selected == nil {
		return asc.ProductExperiment{}, asc.ExperimentTreatment{}, asc.ExperimentLocalization{}, errors.New("treatment locale not found")
	}
	current, err := asc.GetExperimentLocalization(ctx, c, treatment.ID, selected.ID)
	return exp, treatment, current, err
}

func newExperimentTreatmentsCommand() *cobra.Command {
	root := &cobra.Command{Use: "treatments", Short: "Manage experiment variants"}
	root.AddCommand(newExperimentTreatmentListCommand(), newExperimentTreatmentCreateCommand(), newExperimentTreatmentUpdateCommand(), newExperimentTreatmentDeleteCommand(), newExperimentLocalizationsCommand())
	return root
}
func newExperimentTreatmentListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Args: cobra.ExactArgs(1), Short: "List variants for a selected experiment"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		exp, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id)
		if err != nil {
			return err
		}
		items, err := asc.ListExperimentTreatments(cmd.Context(), c, exp.ID)
		if err != nil {
			return err
		}
		return Render(ExperimentTreatmentListResult{Treatments: items}, outputMode())
	}
	return cmd
}
func newExperimentTreatmentCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Args: cobra.ExactArgs(1), Short: "Create a treatment"}
	experimentTargetFlags(cmd)
	experimentIDFlag(cmd)
	experimentConfirmFlag(cmd)
	cmd.Flags().String("name", "", "treatment name")
	cmd.Flags().String("app-icon-name", "", "optional icon in current app binary")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		icon, _ := cmd.Flags().GetString("app-icon-name")
		if name == "" {
			return errors.New("--name is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, versionID, platform, err := resolveExperimentTarget(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("experiment")
		exp, err := selectedProductExperiment(cmd.Context(), c, appID, versionID, platform, id)
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		out, err := createOrReuseExperimentTreatment(cmd.Context(), c, exp.ID, name, icon)
		if err != nil {
			return err
		}
		return Render(out, outputMode())
	}
	return cmd
}

func createOrReuseExperimentTreatment(ctx context.Context, c *asc.Client, experimentID, name, icon string) (ExperimentTreatmentAction, error) {
	items, err := asc.ListExperimentTreatments(ctx, c, experimentID)
	if err != nil {
		return ExperimentTreatmentAction{}, err
	}
	for i := range items {
		if items[i].Name == name {
			if items[i].AppIconName != icon {
				return ExperimentTreatmentAction{}, errors.New("treatment name already exists with a different icon")
			}
			return ExperimentTreatmentAction{Action: "existing", Treatment: items[i]}, nil
		}
	}
	if len(items) >= 3 {
		return ExperimentTreatmentAction{}, errors.New("experiment already has three treatments")
	}
	item, err := asc.CreateExperimentTreatment(ctx, c, experimentID, name, icon)
	if err != nil {
		return ExperimentTreatmentAction{}, err
	}
	return ExperimentTreatmentAction{Action: "created", Treatment: item, Changed: true}, nil
}
func newExperimentTreatmentUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update <bundleId>", Args: cobra.ExactArgs(1), Short: "Update treatment name or app icon"}
	experimentTreatmentFlags(cmd)
	experimentConfirmFlag(cmd)
	cmd.Flags().String("name", "", "new name")
	cmd.Flags().String("app-icon-name", "", "new app icon name")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		var name, icon *string
		if cmd.Flags().Changed("name") {
			v, _ := cmd.Flags().GetString("name")
			name = &v
		}
		if cmd.Flags().Changed("app-icon-name") {
			v, _ := cmd.Flags().GetString("app-icon-name")
			icon = &v
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		exp, treatment, err := selectedExperimentTreatment(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		item, changed, err := asc.UpdateExperimentTreatment(cmd.Context(), c, exp.ID, treatment.ID, name, icon)
		if err != nil {
			return err
		}
		return Render(ExperimentTreatmentAction{Action: "updated", Treatment: item, Changed: changed}, outputMode())
	}
	return cmd
}
func newExperimentTreatmentDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Args: cobra.ExactArgs(1), Short: "Delete an unstarted treatment"}
	experimentTreatmentFlags(cmd)
	experimentConfirmFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		exp, treatment, err := selectedExperimentTreatment(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		if err := asc.DeleteExperimentTreatment(cmd.Context(), c, exp.ID, treatment.ID); err != nil {
			return err
		}
		return Render(ExperimentTreatmentAction{Action: "deleted", Treatment: treatment, Changed: true}, outputMode())
	}
	return cmd
}

func newExperimentLocalizationsCommand() *cobra.Command {
	root := &cobra.Command{Use: "localizations", Short: "Manage treatment locales"}
	root.AddCommand(newExperimentLocalizationListCommand(), newExperimentLocalizationCreateCommand(), newExperimentLocalizationDeleteCommand(), newExperimentAssetsCommand())
	return root
}
func newExperimentLocalizationListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Args: cobra.ExactArgs(1), Short: "List treatment locales"}
	experimentTreatmentFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		_, treatment, err := selectedExperimentTreatment(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		items, err := asc.ListExperimentLocalizations(cmd.Context(), c, treatment.ID)
		if err != nil {
			return err
		}
		return Render(ExperimentLocalizationListResult{Localizations: items}, outputMode())
	}
	return cmd
}
func newExperimentLocalizationCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Args: cobra.ExactArgs(1), Short: "Create a treatment locale"}
	experimentLocalizationFlags(cmd)
	experimentConfirmFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		locale, _ := cmd.Flags().GetString("locale")
		if locale == "" {
			return errors.New("--locale is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		exp, treatment, err := selectedExperimentTreatment(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		items, err := asc.ListExperimentLocalizations(cmd.Context(), c, treatment.ID)
		if err != nil {
			return err
		}
		for i := range items {
			if items[i].Locale == locale {
				return Render(ExperimentLocalizationAction{Action: "existing", Localization: items[i]}, outputMode())
			}
		}
		item, err := asc.CreateExperimentLocalization(cmd.Context(), c, treatment.ID, locale)
		if err != nil {
			return err
		}
		return Render(ExperimentLocalizationAction{Action: "created", Localization: item, Changed: true}, outputMode())
	}
	return cmd
}
func newExperimentLocalizationDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Args: cobra.ExactArgs(1), Short: "Delete a treatment locale"}
	experimentLocalizationFlags(cmd)
	experimentConfirmFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireExperimentConfirm(cmd); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		exp, treatment, loc, err := selectedExperimentLocalization(cmd.Context(), c, cmd, args[0])
		if err != nil {
			return err
		}
		if !asc.CanEditProductExperiment(exp) {
			return errors.New("experiment is not an editable draft")
		}
		if err := asc.DeleteExperimentLocalization(cmd.Context(), c, treatment.ID, loc.ID); err != nil {
			return err
		}
		return Render(ExperimentLocalizationAction{Action: "deleted", Localization: loc, Changed: true}, outputMode())
	}
	return cmd
}
