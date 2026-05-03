// load_corpus_test.go — Phase 4.3.1 loader corpus runner.
//
// Drives every YAML in testdata/{good,bad,quirks} through LoadState +
// Validate and asserts the produced diagnostics match the sibling
// .expected.json (or, for goods, that no diagnostics fire).
//
// Sidecar shape:
//
//	{
//	  "diagnostics": [
//	    { "stage": "load",   "line": 7,  "messageContains": "field foo not found" },
//	    { "stage": "schema", "path": "/spec/iap/products/x/type", "messageContains": "must be one of" }
//	  ]
//	}
//
// stage:        "load" for KnownFields/yaml.v3 errors, "schema" for jsonschema validate
// line:         optional line number (load only); 0 means don't check
// path:         optional JSON-Pointer (schema only); "" means don't check
// messageContains: substring match against Diagnostic.Message
//
// Each declared diagnostic must match at least one actual diagnostic;
// extras in the actual list are tolerated (some schema rules emit
// multiple causes — we assert presence of the named one).

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// expectedDiagnostic is the JSON shape of a single sidecar entry.
type expectedDiagnostic struct {
	Stage           string `json:"stage"`
	Path            string `json:"path,omitempty"`
	Line            int    `json:"line,omitempty"`
	MessageContains string `json:"messageContains"`
}

type expectedFile struct {
	Diagnostics []expectedDiagnostic `json:"diagnostics"`
}

// TestLoaderCorpus_Good walks testdata/good/*.yaml and asserts every
// fixture loads cleanly and validates against the embedded schema.
func TestLoaderCorpus_Good(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "good", "*.yaml"))
	if err != nil {
		t.Fatalf("glob good: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no good fixtures found; check testdata/good/")
	}
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			s, err := LoadState(f)
			if err != nil {
				t.Fatalf("LoadState: %v", err)
			}
			if s == nil {
				t.Fatal("LoadState returned nil state with no error")
			}
			diags := Validate(f, s)
			if len(diags) != 0 {
				for _, d := range diags {
					t.Errorf("unexpected schema diagnostic: %s", d)
				}
			}
		})
	}
}

// TestLoaderCorpus_Bad walks testdata/bad/*.yaml and asserts each
// fixture produces a Diagnostic matching its sibling .expected.json.
func TestLoaderCorpus_Bad(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "bad", "*.yaml"))
	if err != nil {
		t.Fatalf("glob bad: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no bad fixtures found; check testdata/bad/")
	}
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			expected := loadExpected(t, f)
			if len(expected.Diagnostics) == 0 {
				t.Fatalf("expected file has zero diagnostics declared; bad fixture must declare at least one")
			}

			loadDiags, schemaDiags := loadAndValidate(t, f)
			if len(loadDiags) == 0 && len(schemaDiags) == 0 {
				t.Fatalf("fixture produced no diagnostics; either it is not actually bad or a validator gap (file QA issue)")
			}

			for i, want := range expected.Diagnostics {
				if !diagnosticMatches(want, loadDiags, schemaDiags) {
					t.Errorf("expected diagnostic[%d] not matched: %+v\nload diags:\n%s\nschema diags:\n%s",
						i, want, formatDiags(loadDiags), formatDiags(schemaDiags))
				}
			}
		})
	}
}

// TestLoaderCorpus_Quirks documents validator gaps as regression markers.
// Each fixture in testdata/quirks/ is currently accepted (load + schema
// both clean) when it shouldn't be — see QA-011 for the underlying
// jsonschema/v6 format-checks-disabled gap and the yaml.v3 yes/no
// boolean-coercion footgun. When QA-011 lands, these tests will start
// failing — at which point flip them into testdata/bad/ with proper
// .expected.json sidecars.
func TestLoaderCorpus_Quirks(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "quirks", "*.yaml"))
	if err != nil {
		t.Fatalf("glob quirks: %v", err)
	}
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			loadDiags, schemaDiags := loadAndValidate(t, f)
			if len(loadDiags) != 0 || len(schemaDiags) != 0 {
				t.Logf("Quirk %s now produces diagnostics — promote it from quirks/ to bad/.\nload: %s\nschema: %s",
					filepath.Base(f), formatDiags(loadDiags), formatDiags(schemaDiags))
				t.Fatal("quirk fixture now caught — promote to testdata/bad/ and add .expected.json (see QA-011)")
			}
		})
	}
}

// loadAndValidate runs both stages of validation on path. Returns
// (loadDiags, schemaDiags). Either may be empty.
func loadAndValidate(t *testing.T, path string) (loadDiags, schemaDiags []Diagnostic) {
	t.Helper()
	s, err := LoadState(path)
	if err != nil {
		var le *LoadError
		if errors.As(err, &le) {
			return le.Diagnostics, nil
		}
		// Non-LoadError surface (e.g. open failure) — wrap as a single
		// load-stage diagnostic so the comparator can match against it.
		return []Diagnostic{{File: path, Severity: SeverityError, Message: err.Error()}}, nil
	}
	return nil, Validate(path, s)
}

// loadExpected reads the .expected.json sidecar for fixture and parses
// it into expectedFile. Failure to parse is fatal: corpus authors must
// keep sidecars in sync.
func loadExpected(t *testing.T, fixture string) expectedFile {
	t.Helper()
	side := strings.TrimSuffix(fixture, ".yaml") + ".expected.json"
	buf, err := os.ReadFile(side) //nolint:gosec // path derived from a glob over testdata/
	if err != nil {
		t.Fatalf("read sidecar %s: %v", side, err)
	}
	var ef expectedFile
	if err := json.Unmarshal(buf, &ef); err != nil {
		t.Fatalf("parse sidecar %s: %v", side, err)
	}
	return ef
}

// diagnosticMatches returns true when at least one actual Diagnostic
// satisfies all the constraints declared in want.
func diagnosticMatches(want expectedDiagnostic, loadDiags, schemaDiags []Diagnostic) bool {
	var pool []Diagnostic
	switch want.Stage {
	case "load":
		pool = loadDiags
	case "schema":
		pool = schemaDiags
	default:
		return false
	}
	for _, got := range pool {
		if want.Path != "" && got.Path != want.Path {
			continue
		}
		if want.Line > 0 {
			// yaml.v3 errors carry the line in the message
			// ("line N: ..."), not in Diagnostic.Line — match
			// substring "line N:" instead.
			needle := lineMarker(want.Line)
			if !strings.Contains(got.Message, needle) {
				continue
			}
		}
		if want.MessageContains != "" && !strings.Contains(got.Message, want.MessageContains) {
			continue
		}
		return true
	}
	return false
}

// lineMarker formats a "line N:" anchor that yaml.v3 emits in its
// TypeError messages.
func lineMarker(n int) string {
	return "line " + itoa(n) + ":"
}

// itoa is a tiny strconv.Itoa to keep the imports minimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func formatDiags(ds []Diagnostic) string {
	if len(ds) == 0 {
		return "  (none)"
	}
	var sb strings.Builder
	for _, d := range ds {
		sb.WriteString("  - ")
		sb.WriteString(d.String())
		sb.WriteString("\n")
	}
	return sb.String()
}
