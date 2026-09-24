package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

// AppTagView is an assigned App Store tag with its current visibility state.
type AppTagView struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Attributes asc.AppTagAttributes `json:"attributes"`
}

// AppTagList is the stable JSON and table view for assigned tags.
type AppTagList struct {
	Tags []AppTagView `json:"tags"`
}

func (l AppTagList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"NAME", "VISIBLE_IN_APP_STORE", "ID"}
	rows = make([][]string, 0, len(l.Tags))
	for i := range l.Tags {
		tag := &l.Tags[i]
		rows = append(rows, []string{tag.Attributes.Name, boolPtrStr(tag.Attributes.VisibleInAppStore), tag.ID})
	}
	return headers, rows
}

// AppTagVisibilityResult reports the selected tag and whether a PATCH was needed.
type AppTagVisibilityResult struct {
	Tag     AppTagView `json:"tag"`
	Changed bool       `json:"changed"`
}

func (r AppTagVisibilityResult) TableRows() (headers []string, rows [][]string) {
	return (&r.Tag).TableRows()
}

func (v *AppTagView) TableRows() (headers []string, rows [][]string) {
	return []string{"FIELD", "VALUE"}, [][]string{
		{"ID", v.ID},
		{"NAME", v.Attributes.Name},
		{"VISIBLE_IN_APP_STORE", boolPtrStr(v.Attributes.VisibleInAppStore)},
	}
}

// newAppTagsCommand builds the app-tags command tree for lead-owned registration.
func newAppTagsCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "app-tags",
		Short: "Inspect assigned App Store tags and manage their visibility",
	}

	list := &cobra.Command{
		Use:          "list <bundleId>",
		Short:        "List tags assigned to an app",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			view, err := listAppTags(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return Render(view, outputMode())
		},
	}

	setVisibility := &cobra.Command{
		Use:          "set-visibility <bundleId> <tagId>",
		Short:        "Set visibility for a tag already assigned to an app",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, err := cmd.Flags().GetBool("confirm")
			if err != nil {
				return err
			}
			if !confirmed {
				return errors.New("app-tags: pass --confirm to update tag visibility")
			}
			visibleValue, err := cmd.Flags().GetString("visible-in-app-store")
			if err != nil {
				return err
			}
			visible, err := strconv.ParseBool(visibleValue)
			if err != nil {
				return errors.New("app-tags: --visible-in-app-store must be true or false")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			result, err := setAppTagVisibility(cmd.Context(), client, args[0], args[1], visible)
			if err != nil {
				return err
			}
			return Render(result, outputMode())
		},
	}
	setVisibility.Flags().Bool("confirm", false, "confirm the visibility update")
	setVisibility.Flags().String("visible-in-app-store", "", "required target visibility: true or false")
	_ = setVisibility.MarkFlagRequired("confirm")
	_ = setVisibility.MarkFlagRequired("visible-in-app-store")

	group.AddCommand(list, setVisibility)
	return group
}

func listAppTags(ctx context.Context, client *asc.Client, appIdentifier string) (AppTagList, error) {
	appID, err := resolveAppID(ctx, client, appIdentifier)
	if err != nil {
		return AppTagList{}, err
	}
	tags, err := asc.ListAppTags(ctx, client, appID)
	if err != nil {
		return AppTagList{}, err
	}
	result := AppTagList{Tags: make([]AppTagView, 0, len(tags))}
	for _, tag := range tags {
		result.Tags = append(result.Tags, AppTagView{ID: tag.ID, Type: tag.Type, Attributes: tag.Attributes})
	}
	return result, nil
}

func setAppTagVisibility(ctx context.Context, client *asc.Client, appIdentifier, tagID string, visible bool) (AppTagVisibilityResult, error) {
	if tagID == "" {
		return AppTagVisibilityResult{}, errors.New("app-tags: tag ID is required")
	}
	result, err := listAppTags(ctx, client, appIdentifier)
	if err != nil {
		return AppTagVisibilityResult{}, err
	}
	for _, tag := range result.Tags {
		if tag.ID != tagID {
			continue
		}
		if tag.Attributes.VisibleInAppStore == nil {
			return AppTagVisibilityResult{}, fmt.Errorf("app-tags: current visibility is missing for assigned tag %q", tagID)
		}
		if *tag.Attributes.VisibleInAppStore == visible {
			return AppTagVisibilityResult{Tag: tag, Changed: false}, nil
		}
		updated, err := asc.PatchAppTagVisibility(ctx, client, tagID, visible)
		if err != nil {
			return AppTagVisibilityResult{}, err
		}
		if updated.ID != tagID || updated.Type != "appTags" || updated.Attributes.VisibleInAppStore == nil || *updated.Attributes.VisibleInAppStore != visible {
			return AppTagVisibilityResult{}, fmt.Errorf("app-tags: visibility update response for tag %q is incomplete or inconsistent; run app-tags list to inspect current state", tagID)
		}
		return AppTagVisibilityResult{Tag: AppTagView{ID: updated.ID, Type: updated.Type, Attributes: updated.Attributes}, Changed: true}, nil
	}
	return AppTagVisibilityResult{}, fmt.Errorf("app-tags: tag %q is not assigned to app %q", tagID, appIdentifier)
}
