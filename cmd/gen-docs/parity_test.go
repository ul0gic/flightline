package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

func TestPublicDocsDoNotExposeInternalProjectManagement(t *testing.T) {
	internalRef := regexp.MustCompile(`(?i)(\.project|\b(BUG|DBT|ENH|FEAT|ISSUE|PRF|QA|SEC)-[0-9]{3}\b|\bphase [0-9]+\b|\bPRD\b)`)
	paths := []string{"../../README.md"}
	err := filepath.WalkDir("../../docs", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking public docs: %v", err)
	}

	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			if internalRef.MatchString(line) {
				t.Errorf("%s:%d exposes internal project-management language: %s", path, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestPublicDocsKeepLifecycleAndPositioningClaimsPrecise(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)(the first declarative tool|the app store doesn't\. until now|beta-review submit[^\n]{0,120}(triggers apple review|only (step|action)))`)
	paths := []string{"../../README.md", "../../docs/guides/state-as-code.md", "../../docs/concepts/three-layer-model.md"}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if match := forbidden.FindString(string(content)); match != "" {
			t.Errorf("%s contains an imprecise lifecycle or novelty claim: %q", path, match)
		}
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}
	for _, required := range []string{"Fastlane Deliver", "external TestFlight", "Submit for Review in ASC"} {
		if !strings.Contains(string(readme), required) {
			t.Errorf("README is missing required positioning boundary %q", required)
		}
	}
}
