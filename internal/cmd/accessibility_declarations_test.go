package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestF4A_AnswerFlagsPreserveExplicitFalse(t *testing.T) {
	cmd := newAccessibilityCreateCommand()
	if err := cmd.Flags().Set("device-family", "IPHONE"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("supports-voiceover", "false"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("supports-captions", "true"); err != nil {
		t.Fatal(err)
	}
	flags := accessibilityAnswerFlags{}
	flags.values[8] = false
	flags.values[1] = true
	answers := flags.attributes(cmd)
	if answers.SupportsVoiceover == nil || *answers.SupportsVoiceover || answers.SupportsCaptions == nil || !*answers.SupportsCaptions || answers.SupportsDarkInterface != nil {
		t.Fatalf("answers=%+v", answers)
	}
}

func TestF4A_CommandsRequireDeliberatePublicationAndDeletion(t *testing.T) {
	for _, command := range []*cobra.Command{newAccessibilityCreateCommand(), newAccessibilityUpdateCommand(), newAccessibilityPublishCommand(), newAccessibilityDeleteCommand()} {
		command.SetArgs([]string{"D1"})
		if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("command %s err=%v", command.Name(), err)
		}
	}
}

func TestF4A_EmptyListJSONIsArray(t *testing.T) {
	empty := []asc.AccessibilityDeclaration{}
	buf, err := json.Marshal(&AccessibilityDeclarationsResult{Action: "list", Declarations: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), `"declarations":[]`) {
		t.Fatalf("list JSON=%s", buf)
	}
}

func TestF4A_CommandTreeConstructsAllVerbs(t *testing.T) {
	root := newAccessibilityDeclarationsCommand()
	want := map[string]bool{"list": false, "get": false, "create": false, "update": false, "publish": false, "delete": false}
	for _, command := range root.Commands() {
		if _, ok := want[command.Name()]; !ok {
			t.Fatalf("unexpected command %q", command.Name())
		}
		want[command.Name()] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing command %q", name)
		}
	}
}
