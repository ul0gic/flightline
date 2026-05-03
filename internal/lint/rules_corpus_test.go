package lint

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ul0gic/skipper/internal/config"
)

// expectedDiagnostic describes one diagnostic we expect a fail.yaml fixture
// to emit. The match is partial: ruleId + severity must match exactly,
// message must contain MessageSubstring, and Path (when set) must match
// exactly. Any field left zero is unchecked. The JSON shape mirrors the
// production lint.Diagnostic so consumers can paste fragments back and
// forth between the corpus and real output.
type expectedDiagnostic struct {
	RuleID            string `json:"ruleId"`
	Severity          string `json:"severity"`
	MessageSubstring  string `json:"messageSubstring,omitempty"`
	Path              string `json:"path,omitempty"`
	FixHintSubstring  string `json:"fixHintSubstring,omitempty"`
	ReferenceContains string `json:"referenceContains,omitempty"`
}

// fixtureExpectation is the JSON shape on disk:
// { "diagnostics": [ {ruleId, severity, messageSubstring, path}, ... ] }
type fixtureExpectation struct {
	// MinCount lets a fail.expected.json say "at least N diagnostics fired"
	// without enumerating each. Zero means "len(diagnostics) must equal
	// len(Diagnostics)".
	MinCount    int                  `json:"minCount,omitempty"`
	Diagnostics []expectedDiagnostic `json:"diagnostics"`
}

// liveRoutes is the flat path-suffix → JSON-body map used by live-rule
// fixtures. The HTTP method is implicit (the helper matches any). Body
// values are inlined raw JSON so the file is self-describing.
type liveRoutes struct {
	// Default is served when no route key matches. When empty, unknown
	// paths return `{"data":[]}` so most "list returns nothing" rules
	// no-op cleanly.
	Default string            `json:"default,omitempty"`
	Routes  map[string]string `json:"routes"`
}

// ruleByID returns the registered rule with the given id, or nil.
func ruleByID(id string) Rule {
	for _, r := range All() {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

// loadFixture reads pass.yaml or fail*.yaml into a *config.State by going
// through the production loader. Loader errors are reported as test
// fatals so corpus authors notice typos.
func loadFixture(t *testing.T, path string) *config.State {
	t.Helper()
	st, err := config.LoadState(path)
	if err != nil {
		t.Fatalf("load %s: %v", filepath.Base(path), err)
	}
	return st
}

// loadExpectation loads a `fail.expected.json` sidecar. Missing sidecar
// returns nil (the test treats that as "expect ≥1 diagnostic with the
// rule's default id and severity").
func loadExpectation(t *testing.T, path string) *fixtureExpectation {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- fixture path is under testdata/
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("read %s: %v", path, err)
	}
	var exp fixtureExpectation
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return &exp
}

// loadRoutes reads optional routes.json next to a fail.yaml and returns
// nil when absent (offline rule).
func loadRoutes(t *testing.T, path string) *liveRoutes {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- fixture path is under testdata/
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("read %s: %v", path, err)
	}
	var r liveRoutes
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return &r
}

// startRoutesServer mounts a path-suffix-keyed httptest server. Match order:
//
//  1. exact path
//  2. longest path-suffix
//  3. routes.Default (when set)
//  4. `{"data":[]}` fallback so unknown listing endpoints return empty
//     collections rather than 404s
//
// This intentionally swallows method ambiguity (rules only GET); when a
// live rule needs POST/PATCH/DELETE the helper will need keying by method.
func startRoutesServer(t *testing.T, routes *liveRoutes) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		body := pickRouteBody(routes, r.URL.Path)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func pickRouteBody(routes *liveRoutes, path string) string {
	if routes == nil {
		return `{"data":[]}`
	}
	// Exact match wins.
	if body, ok := routes.Routes[path]; ok {
		return body
	}
	// Longest-suffix match.
	type kv struct {
		k string
		v string
	}
	candidates := make([]kv, 0)
	for k, v := range routes.Routes {
		if strings.HasSuffix(path, k) {
			candidates = append(candidates, kv{k, v})
		}
	}
	if len(candidates) > 0 {
		sort.SliceStable(candidates, func(i, j int) bool {
			return len(candidates[i].k) > len(candidates[j].k)
		})
		return candidates[0].v
	}
	if routes.Default != "" {
		return routes.Default
	}
	return `{"data":[]}`
}

// runRuleAgainstFixture executes a single rule against a single fixture
// directory entry (pass.yaml or fail*.yaml) and returns the diagnostics
// it produced.
//
// The function picks live mode when a routes.json sits next to the YAML;
// otherwise it runs offline. SourcePath is always set so strict.* rules
// can read the raw bytes.
func runRuleAgainstFixture(t *testing.T, rule Rule, yamlPath, routesPath string) []Diagnostic {
	t.Helper()
	state := loadFixture(t, yamlPath)
	ctx := CheckContext{
		Ctx:        context.Background(),
		State:      state,
		SourcePath: yamlPath,
	}
	if state != nil {
		ctx.BundleID = state.Metadata.BundleID
		ctx.Version = state.Metadata.Version
	}
	if routes := loadRoutes(t, routesPath); routes != nil {
		srv := startRoutesServer(t, routes)
		ctx.Client = newTestClient(t, srv)
		ctx.Live = true
	}
	return rule.Check(ctx)
}

// matchExpectation asserts diags satisfy exp. When exp is nil, the test
// only checks "at least one diagnostic with rule.ID() and rule.Severity()".
func matchExpectation(t *testing.T, label string, rule Rule, exp *fixtureExpectation, diags []Diagnostic) {
	t.Helper()
	if exp == nil {
		if len(diags) == 0 {
			t.Errorf("%s: rule %s produced no diagnostics, expected at least one", label, rule.ID())
			return
		}
		seen := false
		for _, d := range diags {
			if d.RuleID == rule.ID() && d.Severity == rule.Severity() {
				seen = true
				break
			}
		}
		if !seen {
			t.Errorf("%s: no diagnostic with ruleID=%s severity=%s; got: %+v",
				label, rule.ID(), rule.Severity(), diags)
		}
		return
	}
	if exp.MinCount > 0 {
		if len(diags) < exp.MinCount {
			t.Errorf("%s: got %d diagnostics, want at least %d: %+v",
				label, len(diags), exp.MinCount, diags)
		}
	}
	for _, want := range exp.Diagnostics {
		if !findDiagnostic(diags, want) {
			t.Errorf("%s: missing expected diagnostic %+v\nactual: %s",
				label, want, formatDiags(diags))
		}
	}
}

func findDiagnostic(diags []Diagnostic, want expectedDiagnostic) bool {
	for _, d := range diags {
		if want.RuleID != "" && d.RuleID != want.RuleID {
			continue
		}
		if want.Severity != "" && d.Severity.String() != want.Severity {
			continue
		}
		if want.MessageSubstring != "" && !strings.Contains(d.Message, want.MessageSubstring) {
			continue
		}
		if want.Path != "" && d.Path != want.Path {
			continue
		}
		if want.FixHintSubstring != "" && !strings.Contains(d.FixHint, want.FixHintSubstring) {
			continue
		}
		if want.ReferenceContains != "" && !strings.Contains(d.Reference, want.ReferenceContains) {
			continue
		}
		return true
	}
	return false
}

func formatDiags(diags []Diagnostic) string {
	if len(diags) == 0 {
		return "(none)"
	}
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = "  " + d.RuleID + " [" + d.Severity.String() + "] " + d.Path + " :: " + d.Message
	}
	return strings.Join(out, "\n")
}

// TestRulesCorpus is the table-driven driver: it walks
// testdata/rules/<rule_id>/ subdirectories and asserts each pass.yaml
// fires no diagnostics for its rule, and each fail*.yaml fires at least
// one matching the optional sidecar expectation.
//
// New rules drop a directory into testdata/rules/ and the test picks it
// up automatically. Each rule MUST have at minimum:
//   - pass.yaml: valid state where the rule does NOT fire
//   - fail.yaml: state where the rule fires
//
// fail_<variant>.yaml is supported for rules with multiple distinct
// failure modes; the variant suffix becomes the subtest name.
//
// Live rules add a routes.json keyed by URL path or path-suffix.
func TestRulesCorpus(t *testing.T) {
	root := filepath.Join("testdata", "rules")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		ruleID := ent.Name()
		rule := ruleByID(ruleID)
		if rule == nil {
			t.Errorf("testdata/rules/%s/ has no matching registered rule", ruleID)
			continue
		}
		t.Run(ruleID, func(t *testing.T) {
			runRuleCorpus(t, rule, filepath.Join(root, ruleID))
		})
	}
}

func runRuleCorpus(t *testing.T, rule Rule, dir string) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	hasPass := false
	hasFail := false
	for _, f := range files {
		name := f.Name()
		if f.IsDir() {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		yamlPath := filepath.Join(dir, name)
		base := strings.TrimSuffix(name, ".yaml")
		routesPath := filepath.Join(dir, base+".routes.json")

		switch {
		case base == "pass":
			hasPass = true
			t.Run("pass", func(t *testing.T) {
				diags := runRuleAgainstFixture(t, rule, yamlPath, routesPath)
				// A rule whose default severity is Info MAY always emit
				// its Info reminder (e.g. version.account-deletion-attested
				// has no API surface to verify against, so the reminder
				// fires unconditionally). For those rules the "pass"
				// criterion is "no error- or warning-severity diagnostics
				// from this rule"; Info diagnostics are allowed.
				if rule.Severity() == SeverityInfo {
					for _, d := range diags {
						if d.RuleID == rule.ID() &&
							(d.Severity == SeverityError || d.Severity == SeverityWarning) {
							t.Errorf("pass.yaml: rule %s produced non-info diag: %+v",
								rule.ID(), d)
						}
					}
					return
				}
				if len(diags) != 0 {
					t.Errorf("pass.yaml produced %d diagnostics, want 0:\n%s",
						len(diags), formatDiags(diags))
				}
			})
		case base == "fail" || strings.HasPrefix(base, "fail_"):
			hasFail = true
			expPath := filepath.Join(dir, base+".expected.json")
			t.Run(base, func(t *testing.T) {
				diags := runRuleAgainstFixture(t, rule, yamlPath, routesPath)
				exp := loadExpectation(t, expPath)
				matchExpectation(t, base, rule, exp, diags)
			})
		}
	}
	if !hasPass {
		t.Errorf("rule %s: missing pass.yaml under %s", rule.ID(), dir)
	}
	if !hasFail {
		t.Errorf("rule %s: missing at least one fail*.yaml under %s", rule.ID(), dir)
	}
}
