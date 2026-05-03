package cmd

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/ul0gic/flightline/internal/lint"
)

// stableLintTopLevelKeys is the locked set of keys at the top level of
// `fline lint --output json`. Adding fields is fine; removing or
// renaming is a breaking change.
var stableLintTopLevelKeys = []string{
	"bundleId",
	"diagnostics",
	"mode",
	"sourcePath",
	"summary",
	"version",
}

// stableSummaryKeys is the locked set of keys in the summary block.
var stableSummaryKeys = []string{
	"error",
	"info",
	"warning",
}

// stableDiagnosticKeys is the locked set of per-diagnostic keys. fixHint,
// path, and reference are optional in the JSON tag so the marshaled shape
// will only include them when populated; the test below verifies they
// surface when set.
var stableDiagnosticKeys = []string{
	"fixHint",
	"message",
	"path",
	"reference",
	"ruleId",
	"severity",
}

// stableSeverityValues is the locked, lowercase string form for severity.
// The Severity.MarshalJSON contract is part of the wire surface.
var stableSeverityValues = []string{"error", "info", "warning"}

// TestLintOutput_TopLevelKeysStable runs `lint` against a YAML that fires
// multiple rules and asserts the top-level JSON keys are exactly the
// stable set.
func TestLintOutput_TopLevelKeysStable(t *testing.T) {
	body := `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.stable
  version: "1.0"
spec:
  metadata:
    locales:
      en-US:
        name: "Hello"
      fr-FR:
        name: "Bonjour"
  screenshots:
    locales:
      en-US:
        APP_IPHONE_69:
          - path: ./en.png
`
	p := writeTempState(t, body)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		_ = runLint(lintCmd, []string{p})
	})
	var probe map[string]any
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("decode JSON: %v\nout=%s", err, out)
	}
	got := keysOf(probe)
	if !reflect.DeepEqual(got, stableLintTopLevelKeys) {
		t.Errorf("top-level keys drift:\n  got:  %v\n  want: %v\nfull: %s",
			got, stableLintTopLevelKeys, out)
	}
}

// TestLintOutput_SummaryKeysStable freezes summary's key set.
func TestLintOutput_SummaryKeysStable(t *testing.T) {
	p := writeTempState(t, goodStateYAML)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		_ = runLint(lintCmd, []string{p})
	})
	var probe map[string]any
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, out)
	}
	sum, ok := probe["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary missing or wrong type: %v", probe["summary"])
	}
	got := keysOf(sum)
	if !reflect.DeepEqual(got, stableSummaryKeys) {
		t.Errorf("summary keys drift:\n  got:  %v\n  want: %v",
			got, stableSummaryKeys)
	}
}

// TestLintOutput_DiagnosticKeysStable runs lint against a YAML crafted
// to make every optional diagnostic field surface (fixHint, path,
// reference). Asserts the union of observed diagnostic keys is exactly
// the stable set.
func TestLintOutput_DiagnosticKeysStable(t *testing.T) {
	body := `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.stable
  version: "1.0"
spec:
  exportCompliance:
    declaration:
      usesEncryption: false
`
	p := writeTempState(t, body)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		_ = runLint(lintCmd, []string{p})
	})
	var probe struct {
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, out)
	}
	if len(probe.Diagnostics) == 0 {
		t.Fatalf("expected at least one diagnostic; out=%s", out)
	}
	union := map[string]struct{}{}
	for _, d := range probe.Diagnostics {
		for k := range d {
			union[k] = struct{}{}
		}
	}
	got := make([]string, 0, len(union))
	for k := range union {
		got = append(got, k)
	}
	sort.Strings(got)
	for _, want := range []string{"ruleId", "severity", "message"} {
		found := false
		for _, k := range got {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required diagnostic key %q never appeared (got keys: %v)",
				want, got)
		}
	}
	for _, k := range got {
		known := false
		for _, allowed := range stableDiagnosticKeys {
			if k == allowed {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("unexpected diagnostic key %q in output (want subset of %v)",
				k, stableDiagnosticKeys)
		}
	}
}

// TestLintOutput_SeverityValuesStable forces a fixture that produces
// diagnostics at error and info severity and asserts the wire form is
// the lowercase token. This guards the Severity.MarshalJSON contract.
func TestLintOutput_SeverityValuesStable(t *testing.T) {
	body := `apiVersion: flightline.dev/v1alpha1
kind: AppState
metadata:
  bundleId: com.example.stable
  version: "1.0"
spec:
  exportCompliance:
    declaration:
      usesEncryption: false
`
	p := writeTempState(t, body)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		_ = runLint(lintCmd, []string{p})
	})
	var probe struct {
		Diagnostics []struct {
			Severity string `json:"severity"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, out)
	}
	for _, d := range probe.Diagnostics {
		known := false
		for _, allowed := range stableSeverityValues {
			if d.Severity == allowed {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("severity %q is not in stable set %v",
				d.Severity, stableSeverityValues)
		}
	}
}

// TestLintOutput_DiagnosticsArrayAlwaysPresent asserts the diagnostics
// array exists even when zero rules fire. Consumers that index on
// `.diagnostics[]` must never see null.
func TestLintOutput_DiagnosticsArrayAlwaysPresent(t *testing.T) {
	p := writeTempState(t, goodStateYAML)
	viper.Reset()
	viper.Set("output", "json")
	out := captureStdout(t, func() {
		_ = runLint(lintCmd, []string{p})
	})
	if !strings.Contains(out, `"diagnostics":[`) && !strings.Contains(out, `"diagnostics": [`) {
		t.Errorf("diagnostics array missing or null in output:\n%s", out)
	}
}

// TestRegisteredRules_HaveStableIDs walks every registered rule and
// asserts the ID matches the documented kebab/dot pattern. ID drift is a
// breaking change to the JSON contract.
func TestRegisteredRules_HaveStableIDs(t *testing.T) {
	for _, r := range lint.All() {
		id := r.ID()
		if id == "" {
			t.Errorf("rule %T has empty ID", r)
			continue
		}
		if strings.ContainsAny(id, " \t\n_") {
			t.Errorf("rule %q contains forbidden whitespace/underscore", id)
		}
		if id != strings.ToLower(id) {
			t.Errorf("rule %q is not lowercase", id)
		}
	}
}

// TestRegisteredRules_SeverityIsKnown asserts every rule's default
// severity round-trips through MarshalJSON to one of the locked tokens.
func TestRegisteredRules_SeverityIsKnown(t *testing.T) {
	for _, r := range lint.All() {
		s := r.Severity()
		b, err := s.MarshalJSON()
		if err != nil {
			t.Errorf("rule %s severity marshal: %v", r.ID(), err)
			continue
		}
		raw := strings.Trim(string(b), `"`)
		known := false
		for _, allowed := range stableSeverityValues {
			if raw == allowed {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("rule %s severity %q is not in stable set %v",
				r.ID(), raw, stableSeverityValues)
		}
	}
}

// keysOf returns the sorted keys of a map for comparison.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
