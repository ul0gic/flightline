package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadState_Example proves the loader round-trips the canonical example.state.yaml into the typed tree.
func TestLoadState_Example(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	path := filepath.Join(repoRoot, "schemas", "example.state.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("example file missing: %v", err)
	}

	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if s.APIVersion != "flightline.dev/v1alpha1" {
		t.Errorf("apiVersion = %q, want flightline.dev/v1alpha1", s.APIVersion)
	}
	if s.Kind != "AppState" {
		t.Errorf("kind = %q, want AppState", s.Kind)
	}
	if s.Metadata.BundleID != "com.under5.passdmv" {
		t.Errorf("bundleId = %q", s.Metadata.BundleID)
	}
	if s.Spec.Version == nil || s.Spec.Version.Copyright == nil || *s.Spec.Version.Copyright == "" {
		t.Error("spec.version.copyright not loaded")
	}
	if s.Spec.IAP == nil || len(s.Spec.IAP.Products) == 0 {
		t.Error("spec.iap.products not loaded")
	}
	if s.Spec.AgeRating == nil {
		t.Error("spec.ageRating not loaded")
	}
}

// TestLoadState_UnknownField verifies KnownFields(true) catches typos
// that would otherwise silently drift away from the schema.
func TestLoadState_UnknownField(t *testing.T) {
	yaml := `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.app
  version: "1.0"
spec:
  version:
    copyright: "© 2026"
    bogusField: "nope"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("err type = %T, want *LoadError", err)
	}
	if len(le.Diagnostics) == 0 {
		t.Fatal("expected at least one diagnostic")
	}
	if !strings.Contains(le.Diagnostics[0].Message, "bogusField") {
		t.Errorf("diagnostic doesn't mention bogusField: %s", le.Diagnostics[0].Message)
	}
}

// TestLoadState_TypeMismatch: a string-where-sequence-expected surfaces as a yaml.v3 TypeError.
func TestLoadState_TypeMismatch(t *testing.T) {
	yaml := `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.app
  version: "1.0"
spec:
  version:
    copyright:
      - this
      - is
      - a
      - sequence
`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for sequence-in-string slot")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error doesn't mention yaml: %s", err.Error())
	}
}

// TestLoadState_EmptyFile: the empty-file diagnostic.
func TestLoadState_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in error: %s", err.Error())
	}
}

// TestLoadState_MissingFile: clean error when the path doesn't exist.
func TestLoadState_MissingFile(t *testing.T) {
	_, err := LoadState("/nonexistent/flightline-test/state.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLoadState_JSONRoundTrip: the typed tree must JSON-marshal cleanly for `--output json`.
func TestLoadState_JSONRoundTrip(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	s, err := LoadState(filepath.Join(repoRoot, "schemas", "example.state.yaml"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"apiVersion":"flightline.dev/v1alpha1"`) {
		t.Errorf("JSON output missing apiVersion: %s", string(out))
	}
}
