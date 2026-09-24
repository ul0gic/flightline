package cmd

import (
	"strings"
	"testing"
)

func TestF8A_SubmissionAssemblySubmitRequiresSeparateConfirm(t *testing.T) {
	cmd := newSubmissionAssemblyCommand()
	cmd.SetArgs([]string{"submit", "com.example.app", "--version", "1.0", "--submission", "S1"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "pass --confirm") {
		t.Fatalf("err=%v", err)
	}
}
