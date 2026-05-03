package cmd

import (
	"strings"
	"testing"

	"github.com/ul0gic/skipper/internal/config"
)

// TestWriteStateYAML_HeaderPresent — every fetched state.yaml must
// carry the yaml-language-server schema directive so editors can
// autocomplete.
func TestWriteStateYAML_HeaderPresent(t *testing.T) {
	cp := "© 2026"
	cpPtr := &cp
	st := &config.State{
		APIVersion: "skipper.corelift.io/v1alpha1",
		Kind:       "AppState",
		Metadata:   config.StateMetadata{BundleID: "com.example.app", Version: "1.0"},
		Spec:       config.StateSpec{Version: &config.VersionSpec{Copyright: cpPtr}},
	}

	var sb strings.Builder
	if err := encodeYAMLForTest(&sb, st); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := sb.String()
	if !strings.HasPrefix(out, "# yaml-language-server: $schema=") {
		t.Fatalf("output missing schema header:\n%s", out)
	}
	if !strings.Contains(out, "apiVersion: skipper.corelift.io/v1alpha1") {
		t.Errorf("apiVersion missing in YAML:\n%s", out)
	}
	if !strings.Contains(out, "© 2026") {
		t.Errorf("copyright missing in YAML:\n%s", out)
	}
}
