package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestF7A_PhasedReleaseConstructorsAndConfirmation(t *testing.T) {
	group := newPhasedReleaseCommand()
	for _, action := range []string{"get", "enable", "pause", "resume"} {
		if command, _, err := group.Find([]string{action}); err != nil || command == nil {
			t.Fatalf("action %s err=%v", action, err)
		}
	}
	command := newPhasedReleaseEnableCommand()
	command.SetContext(context.Background())
	if err := runPhasedReleaseEnableWithClient(command, "com.example.app", false, nil, "json"); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("err=%v", err)
	}
}
