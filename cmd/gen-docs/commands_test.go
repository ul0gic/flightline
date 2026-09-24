package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/ul0gic/flightline/internal/cmd"
)

func TestCLIReferenceIncludesNestedCommandsAndFlags(t *testing.T) {
	root := cmd.Root()
	doc := renderCLI(root)
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		if skipCommand(c) {
			return
		}
		title := strings.TrimPrefix(c.CommandPath(), root.Name()+" ")
		if !strings.Contains(doc, "`"+title+"`") {
			t.Errorf("missing command %s", title)
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden && !strings.Contains(doc, "--"+f.Name) {
				t.Errorf("missing flag %s for %s", f.Name, title)
			}
		})
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	for _, c := range root.Commands() {
		visit(c)
	}
	if !strings.Contains(doc, "--access-type") || !strings.Contains(doc, "ONE_TIME_SNAPSHOT") {
		t.Fatal("analytics flags and examples absent")
	}
}
