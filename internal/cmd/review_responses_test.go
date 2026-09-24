package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestF7B_ReviewResponseBodyFilePreservesExactBytes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "response.txt")
	want := "  Thank you for the report.\nWe are investigating.\n"
	if err := os.WriteFile(file, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newReviewResponseCreateCommand()
	if err := cmd.Flags().Set("body-file", file); err != nil {
		t.Fatal(err)
	}
	got, err := reviewResponseBody(cmd)
	if err != nil || got != want {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestF7B_ReviewResponseRequiresOneNonemptyBodySource(t *testing.T) {
	cmd := newReviewResponseCreateCommand()
	if _, err := reviewResponseBody(cmd); err == nil {
		t.Fatal("missing body accepted")
	}
	if err := cmd.Flags().Set("body", "  "); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewResponseBody(cmd); err == nil {
		t.Fatal("whitespace-only body accepted")
	}
	if err := cmd.Flags().Set("body-file", "response.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewResponseBody(cmd); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err=%v", err)
	}
}

func TestF7B_ReviewResponseMutationRequiresConfirmBeforeClient(t *testing.T) {
	root := newReviewResponsesCommand()
	for _, name := range []string{"create", "delete"} {
		child, _, err := root.Find([]string{name})
		if err != nil || child == nil || child.Flags().Lookup("confirm") == nil {
			t.Fatalf("%s lacks confirm: %v", name, err)
		}
		if err := child.RunE(child, []string{"com.example.app"}); err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("%s err=%v", name, err)
		}
	}
}
