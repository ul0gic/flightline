package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type AccessibilityDeclarationsResult struct {
	Action       string                          `json:"action"`
	BundleID     string                          `json:"bundleId,omitempty"`
	Declaration  *asc.AccessibilityDeclaration   `json:"declaration,omitempty"`
	Declarations *[]asc.AccessibilityDeclaration `json:"declarations,omitempty"`
	DeletedID    string                          `json:"deletedId,omitempty"`
}

func (r *AccessibilityDeclarationsResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"ID", "DEVICE_FAMILY", "STATE", "EXPLICIT_ANSWERS"}
	if r.Declaration != nil {
		rows = append(rows, accessibilityDeclarationRow(*r.Declaration))
	}
	if r.Declarations != nil {
		for _, declaration := range *r.Declarations {
			rows = append(rows, accessibilityDeclarationRow(declaration))
		}
	}
	if r.DeletedID != "" {
		rows = append(rows, []string{r.DeletedID, "", "deleted", ""})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none)", "", "", ""})
	}
	return headers, rows
}

func accessibilityDeclarationRow(d asc.AccessibilityDeclaration) []string {
	return []string{d.ID, d.DeviceFamily, d.State, strconv.Itoa(accessibilityAnswerCount(d.AccessibilityDeclarationAttributes))}
}

func accessibilityAnswerCount(a asc.AccessibilityDeclarationAttributes) int {
	count := 0
	for _, answer := range []*bool{
		a.SupportsAudioDescriptions, a.SupportsCaptions, a.SupportsDarkInterface,
		a.SupportsDifferentiateWithoutColorAlone, a.SupportsLargerText, a.SupportsReducedMotion,
		a.SupportsSufficientContrast, a.SupportsVoiceControl, a.SupportsVoiceover,
	} {
		if answer != nil {
			count++
		}
	}
	return count
}

func newAccessibilityDeclarationsCommand() *cobra.Command {
	root := &cobra.Command{Use: "accessibility-declarations", Short: "Manage per-device accessibility declarations"}
	root.AddCommand(
		newAccessibilityListCommand(), newAccessibilityGetCommand(),
		newAccessibilityCreateCommand(), newAccessibilityUpdateCommand(),
		newAccessibilityPublishCommand(), newAccessibilityDeleteCommand(),
	)
	return root
}

func newAccessibilityListCommand() *cobra.Command {
	return &cobra.Command{Use: "list <bundleId>", Short: "List accessibility declarations for an app", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		declarations, err := asc.ListAccessibilityDeclarations(cmd.Context(), c, appID)
		if err != nil {
			return err
		}
		if declarations == nil {
			declarations = []asc.AccessibilityDeclaration{}
		}
		return Render(&AccessibilityDeclarationsResult{Action: "list", BundleID: args[0], Declarations: &declarations}, outputMode())
	}}
}

func newAccessibilityGetCommand() *cobra.Command {
	return &cobra.Command{Use: "get <declarationId>", Short: "Read one accessibility declaration", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		declaration, err := asc.GetAccessibilityDeclaration(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		return Render(&AccessibilityDeclarationsResult{Action: "get", Declaration: &declaration}, outputMode())
	}}
}

func newAccessibilityCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Short: "Create a declaration draft with explicit support answers", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("device-family", "", "IPHONE, IPAD, APPLE_TV, APPLE_WATCH, MAC, or VISION")
	cmd.Flags().Bool("confirm", false, "confirm declaration creation")
	answers := newAccessibilityAnswerFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		confirmed, _ := cmd.Flags().GetBool("confirm")
		if !confirmed {
			return errors.New("create requires --confirm")
		}
		family, _ := cmd.Flags().GetString("device-family")
		if !asc.ValidAccessibilityFamily(family) {
			return fmt.Errorf("invalid device family %q", family)
		}
		attributes := answers.attributes(cmd)
		attributes.DeviceFamily = family
		if !attributes.HasAnswers() {
			return errors.New("create requires at least one explicit support answer")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		declaration, err := asc.CreateAccessibilityDeclaration(cmd.Context(), c, appID, attributes)
		if err != nil {
			return err
		}
		return Render(&AccessibilityDeclarationsResult{Action: "create", BundleID: args[0], Declaration: &declaration}, outputMode())
	}
	return cmd
}

func newAccessibilityUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update <declarationId>", Short: "Update explicit support answers on a draft", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("confirm", false, "confirm draft answer update")
	answers := newAccessibilityAnswerFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		confirmed, _ := cmd.Flags().GetBool("confirm")
		if !confirmed {
			return errors.New("update requires --confirm")
		}
		attributes := answers.attributes(cmd)
		if !attributes.HasAnswers() {
			return errors.New("update requires at least one explicit support answer")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		declaration, err := asc.UpdateAccessibilityDeclaration(cmd.Context(), c, args[0], attributes)
		if err != nil {
			return err
		}
		return Render(&AccessibilityDeclarationsResult{Action: "update", Declaration: &declaration}, outputMode())
	}
	return cmd
}

func newAccessibilityPublishCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "publish <declarationId>", Short: "Publish a draft declaration", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("confirm", false, "confirm immediate publication")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		confirmed, _ := cmd.Flags().GetBool("confirm")
		if !confirmed {
			return errors.New("publish requires --confirm")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		declaration, err := asc.PublishAccessibilityDeclaration(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		return Render(&AccessibilityDeclarationsResult{Action: "publish", Declaration: &declaration}, outputMode())
	}
	return cmd
}

func newAccessibilityDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <declarationId>", Short: "Delete a draft declaration", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("confirm", false, "confirm draft deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		confirmed, _ := cmd.Flags().GetBool("confirm")
		if !confirmed {
			return errors.New("delete requires --confirm")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		if err := asc.DeleteAccessibilityDeclaration(cmd.Context(), c, args[0]); err != nil {
			return err
		}
		return Render(&AccessibilityDeclarationsResult{Action: "delete", DeletedID: args[0]}, outputMode())
	}
	return cmd
}

var accessibilityAnswerFlagNames = []string{
	"supports-audio-descriptions", "supports-captions", "supports-dark-interface",
	"supports-differentiate-without-color-alone", "supports-larger-text", "supports-reduced-motion",
	"supports-sufficient-contrast", "supports-voice-control", "supports-voiceover",
}

type accessibilityAnswerFlags struct{ values [9]bool }

func newAccessibilityAnswerFlags(cmd *cobra.Command) *accessibilityAnswerFlags {
	flags := &accessibilityAnswerFlags{}
	for i, name := range accessibilityAnswerFlagNames {
		cmd.Flags().BoolVar(&flags.values[i], name, false, "explicit accessibility support answer")
	}
	return flags
}

func (flags *accessibilityAnswerFlags) attributes(cmd *cobra.Command) asc.AccessibilityDeclarationAttributes {
	var attributes asc.AccessibilityDeclarationAttributes
	answers := []**bool{
		&attributes.SupportsAudioDescriptions, &attributes.SupportsCaptions, &attributes.SupportsDarkInterface,
		&attributes.SupportsDifferentiateWithoutColorAlone, &attributes.SupportsLargerText, &attributes.SupportsReducedMotion,
		&attributes.SupportsSufficientContrast, &attributes.SupportsVoiceControl, &attributes.SupportsVoiceover,
	}
	for i, name := range accessibilityAnswerFlagNames {
		if cmd.Flags().Changed(name) {
			value := flags.values[i]
			*answers[i] = &value
		}
	}
	return attributes
}
