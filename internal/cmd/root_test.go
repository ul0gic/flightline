package cmd

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func groupCommands(cmd *cobra.Command, out *[]*cobra.Command) {
	if cmd.HasSubCommands() {
		*out = append(*out, cmd)
	}
	for _, child := range cmd.Commands() {
		groupCommands(child, out)
	}
}

func TestRejectUnknownSubcommands_AllGroups(t *testing.T) {
	rejectUnknownSubcommands(rootCmd)

	var groups []*cobra.Command
	groupCommands(rootCmd, &groups)
	if len(groups) < 20 {
		t.Fatalf("found %d group commands, expected the full tree (>=20)", len(groups))
	}

	for _, g := range groups {
		t.Run(strings.ReplaceAll(g.CommandPath(), " ", "_"), func(t *testing.T) {
			if g.Run == nil && g.RunE == nil {
				t.Fatalf("group %q has no RunE after rejectUnknownSubcommands", g.CommandPath())
			}
			if g.RunE == nil {
				return // group defines its own Run; not our concern
			}
			err := g.RunE(g, []string{"definitely-bogus-subcommand"})
			if err == nil {
				t.Errorf("group %q accepted an unknown subcommand", g.CommandPath())
			} else if !strings.Contains(err.Error(), "definitely-bogus-subcommand") {
				t.Errorf("group %q error %q does not name the bad subcommand", g.CommandPath(), err)
			}
		})
	}
}

func TestRejectUnknownSubcommands_NoArgsShowsHelp(t *testing.T) {
	rejectUnknownSubcommands(rootCmd)

	var groups []*cobra.Command
	groupCommands(rootCmd, &groups)
	for _, g := range groups {
		if g == rootCmd || g.RunE == nil {
			continue
		}
		g.SetOut(io.Discard)
		g.SetErr(io.Discard)
		if err := g.RunE(g, nil); err != nil {
			t.Errorf("group %q with no args should show help without error, got %v", g.CommandPath(), err)
		}
		g.SetOut(nil)
		g.SetErr(nil)
		break // one representative group is enough for the help path
	}
}
