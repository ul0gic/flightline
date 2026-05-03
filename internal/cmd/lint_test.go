package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// goodStateYAML returns a state that should pass schema + every offline
// rule cleanly. Used as the baseline for mutation tests.
const goodStateYAML = `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0.1"
spec:
  ageRating:
    cartoonOrFantasyViolence: NONE
    realisticViolence: NONE
    prolongedGraphicSadisticRealisticViolence: false
    profanityOrCrudeHumor: NONE
    matureSuggestiveThemes: NONE
    horrorOrFearThemes: NONE
    medicalOrTreatmentInformation: NONE
    alcoholTobaccoOrDrugUseOrReferences: NONE
    contestsAndGambling: NONE
    sexualContentOrNudity: NONE
    sexualContentGraphicAndNudity: NONE
    gambling: false
    unrestrictedWebAccess: false
    kidsAgeBand: SIX_TO_EIGHT
    seventeenPlus: false
  exportCompliance:
    usesNonExemptEncryption: false
`

func writeTempState(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// captureStdout swaps os.Stdout for a buffer for the duration of fn,
// returning everything written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()
	fn()
	_ = w.Close()
	<-done
	os.Stdout = old
	return buf.String()
}

// TestRunLint_GoodStateNoErrors covers the happy path: a clean state
// should not produce error-severity diagnostics. Info diagnostics
// (account-deletion-attested) may fire.
func TestRunLint_GoodStateNoErrors(t *testing.T) {
	p := writeTempState(t, goodStateYAML)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		err := runLint(lintCmd, []string{p})
		if err != nil {
			var le errLintErrors
			if errors.As(err, &le) {
				t.Errorf("got lint errors on good state: %d", le.count)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}
	})
	var res LintResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode JSON: %v\nout=%s", err, out)
	}
	if res.Summary.Error != 0 {
		t.Errorf("Summary.Error = %d, want 0; diagnostics: %+v", res.Summary.Error, res.Diagnostics)
	}
	if res.Mode != "lint" {
		t.Errorf("Mode = %q, want lint", res.Mode)
	}
}

// TestRunLint_MissingAgeRatingFires verifies the rule fires through the
// command path and surfaces in the JSON.
func TestRunLint_MissingAgeRatingFires(t *testing.T) {
	body := `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.x
  version: "1.0.1"
spec:
  exportCompliance:
    usesNonExemptEncryption: false
`
	p := writeTempState(t, body)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		err := runLint(lintCmd, []string{p})
		var le errLintErrors
		if !errors.As(err, &le) {
			t.Errorf("expected errLintErrors, got %v", err)
		}
	})
	var res LintResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode JSON: %v\nout=%s", err, out)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.RuleID == "version.age-rating-answered" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected version.age-rating-answered diagnostic; got: %+v", res.Diagnostics)
	}
}

// TestLintResult_JSONShapeStable freezes the top-level keys so future
// edits cannot remove or rename them without explicit intent.
func TestLintResult_JSONShapeStable(t *testing.T) {
	res := &LintResult{
		BundleID:   "com.example.x",
		Version:    "1.0.1",
		SourcePath: "/abs/state.yaml",
		Mode:       "lint",
		Summary:    LintResultSummary{Error: 1, Warning: 2, Info: 3},
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"bundleId", "version", "sourcePath", "mode", "diagnostics", "summary"} {
		if _, ok := probe[k]; !ok {
			t.Errorf("LintResult JSON missing required key %q", k)
		}
	}
	sum, _ := probe["summary"].(map[string]any)
	for _, k := range []string{"error", "warning", "info"} {
		if _, ok := sum[k]; !ok {
			t.Errorf("Summary JSON missing required key %q", k)
		}
	}
}

func TestLintCommand_TableMode(t *testing.T) {
	p := writeTempState(t, goodStateYAML)
	viper.Reset()
	viper.Set("output", "table")
	out := captureStdout(t, func() {
		err := runLint(lintCmd, []string{p})
		_ = err // table mode may still surface info-level rows
	})
	if !bytes.Contains([]byte(out), []byte("SEVERITY")) {
		t.Errorf("table output missing header; got:\n%s", out)
	}
}

// TestLint_LoaderErrorPropagates ensures a malformed YAML surfaces as a
// loader error before any rule runs.
func TestLint_LoaderErrorPropagates(t *testing.T) {
	p := writeTempState(t, "this is not yaml: [unterminated\n")
	viper.Reset()
	viper.Set("output", "json")
	err := runLint(lintCmd, []string{p})
	if err == nil {
		t.Fatal("expected an error from the loader")
	}
	var le errLintErrors
	if errors.As(err, &le) {
		t.Errorf("expected loader error, got errLintErrors")
	}
	// Loader errors include file:line information.
	msg := err.Error()
	if msg == "" {
		t.Error("empty error message")
	}
}

// _ keeps fmt alive in case the captureStdout helper drops it.
var _ = fmt.Sprintf
