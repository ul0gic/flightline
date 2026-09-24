package cmd

import (
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF8B_EventCommandsConfirmBeforeClient(t *testing.T) {
	root := newEventsCommand()
	for _, path := range [][]string{{"create"}, {"update"}, {"delete"}, {"localizations", "create"}, {"localizations", "update"}, {"localizations", "delete"}, {"media", "upload"}, {"media", "delete"}} {
		child, _, err := root.Find(path)
		if err != nil || child == nil {
			t.Fatalf("%v missing: %v", path, err)
		}
		if child.Flags().Lookup("confirm") == nil {
			t.Fatalf("%v lacks confirmation", path)
		}
		args := []string{"com.example.app"}
		if path[len(path)-1] == "upload" {
			args = append(args, "file.png")
		}
		if err := child.RunE(child, args); err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("%v err=%v", path, err)
		}
	}
}

func TestF8B_EventInputRejectsReverseScheduleAndStateFlagAbsent(t *testing.T) {
	cmd := newEventsCreateCommand()
	if cmd.Flags().Lookup("event-state") != nil {
		t.Fatal("event state mutation flag exposed")
	}
	_ = cmd.Flags().Set("reference-name", "fall")
	_ = cmd.Flags().Set("territory-schedules", `[{"territories":["USA"],"publishStart":"2027-01-03T00:00:00Z","eventStart":"2027-01-02T00:00:00Z","eventEnd":"2027-01-04T00:00:00Z"}]`)
	if _, _, err := eventInput(cmd, asc.AppEventAttributes{}, true); err == nil || !strings.Contains(err.Error(), "publishStart") {
		t.Fatalf("err=%v", err)
	}
}
