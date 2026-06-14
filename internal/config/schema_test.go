package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_Example proves the embedded schema stays in sync with the canonical example.state.yaml.
func TestValidate_Example(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	path := filepath.Join(repoRoot, "schemas", "example.state.yaml")
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	diags := Validate(path, s)
	if len(diags) != 0 {
		for _, d := range diags {
			t.Errorf("unexpected diag: %s", d)
		}
	}
}

// TestValidate_BadEnum: an out-of-range enum must surface a Diagnostic whose Path names the field.
func TestValidate_BadEnum(t *testing.T) {
	rt := "BOGUS_RELEASE"
	s := &State{
		APIVersion: "flightline.dev/v1alpha1",
		Kind:       "AppState",
		Metadata:   StateMetadata{BundleID: "com.example.app", Version: "1.0"},
		Spec: StateSpec{
			Version: &VersionSpec{ReleaseType: &rt},
		},
	}
	diags := Validate("test.yaml", s)
	if len(diags) == 0 {
		t.Fatal("expected diagnostic for bogus releaseType")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Path, "/spec/version/releaseType") {
			found = true
		}
	}
	if !found {
		t.Errorf("no diagnostic targeted /spec/version/releaseType; got: %+v", diags)
	}
}

// TestValidate_MissingRequired: apiVersion + kind + metadata are
// required at the top level. Empty State must produce diagnostics.
func TestValidate_MissingRequired(t *testing.T) {
	s := &State{}
	diags := Validate("test.yaml", s)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for empty state")
	}
}

// TestValidate_Pattern: bundleId pattern rejects spaces.
func TestValidate_Pattern(t *testing.T) {
	s := &State{
		APIVersion: "flightline.dev/v1alpha1",
		Kind:       "AppState",
		Metadata:   StateMetadata{BundleID: "has spaces", Version: "1.0"},
	}
	diags := Validate("test.yaml", s)
	if len(diags) == 0 {
		t.Fatal("expected pattern violation diagnostic")
	}
	hit := false
	for _, d := range diags {
		if strings.Contains(d.Path, "/metadata/bundleId") {
			hit = true
		}
	}
	if !hit {
		t.Errorf("no diagnostic on /metadata/bundleId; got: %+v", diags)
	}
}

// TestValidate_NilState: defensive guard.
func TestValidate_NilState(t *testing.T) {
	diags := Validate("test.yaml", nil)
	if len(diags) != 1 {
		t.Fatalf("got %d diags, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "nil") {
		t.Errorf("diag = %q", diags[0].Message)
	}
}

// TestSchemaURL: guard against accidental URL drift; the constant is
// part of the YAML header in fetched files.
func TestSchemaURL(t *testing.T) {
	if SchemaURL != "https://flightline.dev/schemas/v1alpha1/state.schema.json" {
		t.Errorf("SchemaURL drifted: %s", SchemaURL)
	}
}

// TestEmbeddedSchemaInSync: the embedded schema.json must match the canonical schema byte-for-byte (`make sync-schema`).
func TestEmbeddedSchemaInSync(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	canonical, err := os.ReadFile(filepath.Join(repoRoot, "schemas", "flightline.schema.json"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	if !bytes.Equal(canonical, embeddedSchemaJSON) {
		t.Fatal("internal/config/schema.json drifted from schemas/flightline.schema.json: run `make sync-schema`")
	}
}
