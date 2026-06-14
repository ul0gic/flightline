package main

import (
	"os"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/cmd"
	"github.com/ul0gic/flightline/internal/lint"
)

func TestRenderIsDeterministic(t *testing.T) {
	first := renderRules(lint.All())
	second := renderRules(lint.All())
	if first != second {
		t.Fatal("renderRules is not deterministic across calls")
	}
	if a, b := renderCLI(cmd.Root()), renderCLI(cmd.Root()); a != b {
		t.Fatal("renderCLI is not deterministic across calls")
	}
}

func TestPreflightDocCoversEveryRule(t *testing.T) {
	doc := renderRules(lint.All())
	for _, r := range lint.All() {
		id := "`" + r.ID() + "`"
		if !strings.Contains(doc, id) {
			t.Errorf("ruleId %s missing from generated preflight-rules.md", r.ID())
		}
	}
}

func TestCLIDocCoversEveryGroup(t *testing.T) {
	doc := renderCLI(cmd.Root())
	for _, c := range cmd.Root().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		token := "`" + c.Name() + "`"
		if !strings.Contains(doc, token) {
			t.Errorf("command group %q missing from generated cli.md", c.Name())
		}
	}
}

func TestCommittedDocsMatchGenerated(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"../../docs/reference/cli.md", renderCLI(cmd.Root())},
		{"../../docs/reference/preflight-rules.md", renderRules(lint.All())},
	}
	for _, tc := range cases {
		got, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("reading %s: %v", tc.path, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s is out of date; run `make gen-docs`", tc.path)
		}
	}
}
