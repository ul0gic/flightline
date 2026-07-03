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

// Baseline that passes schema + every offline rule; mutation tests diverge from it.
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

func TestRunLint_GoodStateNoErrors(t *testing.T) {
	p := writeTempState(t, goodStateYAML)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		err := runLint(lintCmd, []string{p})
		if err != nil {
			t.Errorf("unexpected error on good state: %v", err)
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
		if ExitCode(err) != 1 {
			t.Errorf("ExitCode = %d, want 1; err = %v", ExitCode(err), err)
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

// Freezes the top-level JSON keys: removing or renaming one is a contract break.
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
		_ = err
	})
	if !bytes.Contains([]byte(out), []byte("SEVERITY")) {
		t.Errorf("table output missing header; got:\n%s", out)
	}
}

// A malformed YAML must surface as a loader error before any rule runs.
func TestLint_LoaderErrorPropagates(t *testing.T) {
	p := writeTempState(t, "this is not yaml: [unterminated\n")
	viper.Reset()
	viper.Set("output", "json")
	err := runLint(lintCmd, []string{p})
	if err == nil {
		t.Fatal("expected an error from the loader")
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		t.Errorf("expected loader error, got ExitError with code %d", exitErr.Code)
	}
	msg := err.Error()
	if msg == "" {
		t.Error("empty error message")
	}
}

// _ keeps fmt alive in case the captureStdout helper drops it.
var _ = fmt.Sprintf
