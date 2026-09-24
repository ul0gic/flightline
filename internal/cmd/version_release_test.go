package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestF7A_VersionReleaseRequiresConfirmation(t *testing.T) {
	command := newVersionReleaseCommand()
	command.SetContext(context.Background())
	if err := runVersionReleaseWithClient(command, "com.example.app", false, nil, "json"); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(command.Long, "never run by state apply") {
		t.Fatalf("help=%q", command.Long)
	}
}
