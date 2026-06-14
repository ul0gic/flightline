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

	"github.com/ul0gic/flightline/internal/config"
)

// expectedDiagnostic is a partial match against a real Diagnostic: ruleId+severity exact, message/path as substrings.
type expectedDiagnostic struct {
	RuleID            string `json:"ruleId"`
	Severity          string `json:"severity"`
	MessageSubstring  string `json:"messageSubstring,omitempty"`
	Path              string `json:"path,omitempty"`
	FixHintSubstring  string `json:"fixHintSubstring,omitempty"`
	ReferenceContains string `json:"referenceContains,omitempty"`
}

// fixtureExpectation is the JSON shape on disk. MinCount=0 means len(Diagnostics) must match exactly.
type fixtureExpectation struct {
	MinCount    int                  `json:"minCount,omitempty"` // 0 = exact count match
	Diagnostics []expectedDiagnostic `json:"diagnostics"`
}

// liveRoutes is the path-suffix → JSON-body map for live-rule fixtures. Unknown paths fall back to Default or `{"data":[]}`.
type liveRoutes struct {
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

// loadFixture loads a YAML fixture through the production loader; fatals on error so authors catch typos.
func loadFixture(t *testing.T, path string) *config.State {
	t.Helper()
	st, err := config.LoadState(path)
	if err != nil {
		t.Fatalf("load %s: %v", filepath.Base(path), err)
	}
	return st
}

// loadExpectation loads a fail.expected.json sidecar. Nil return means "expect ≥1 default-id diagnostic".
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

// loadRoutes reads an optional routes.json; nil means offline rule.
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

// startRoutesServer mounts a path-suffix-keyed httptest server (exact match, then longest suffix, then Default, then `{"data":[]}`).
// Method is ignored; all rules currently only GET.
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

// runRuleAgainstFixture runs rule against a fixture YAML. Live when routes.json is present; SourcePath always set.
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

// matchExpectation asserts diags satisfy exp; nil exp means at least one diagnostic with rule.ID() and severity.
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

// TestRulesCorpus_LiveRulesHaveLiveFixtures guards against silent regressions where the live arm of a Both rule has no fixture.
func TestRulesCorpus_LiveRulesHaveLiveFixtures(t *testing.T) {
	root := filepath.Join("testdata", "rules")
	for _, r := range All() {
		if r.Mode()&ModeLive == 0 {
			continue
		}
		dir := filepath.Join(root, r.ID())
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("rule %s: missing testdata dir %s: %v", r.ID(), dir, err)
			continue
		}
		hasLive := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".routes.json") {
				hasLive = true
				break
			}
		}
		if !hasLive {
			t.Errorf("rule %s is Live or Both mode but has no *.routes.json fixture under %s",
				r.ID(), dir)
		}
	}
}

// TestRulesCorpus walks testdata/rules/<rule_id>/ and asserts pass.yaml produces no diagnostics and fail*.yaml produces ≥1.
// New rules need at minimum pass.yaml + fail.yaml; live rules add routes.json keyed by URL path or suffix.
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
		if f.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		base := strings.TrimSuffix(name, ".yaml")
		yamlPath := filepath.Join(dir, name)
		routesPath := filepath.Join(dir, base+".routes.json")

		switch {
		case base == "pass":
			hasPass = true
			t.Run("pass", func(t *testing.T) {
				runPassFixture(t, rule, yamlPath, routesPath)
			})
		case base == "fail" || strings.HasPrefix(base, "fail_"):
			hasFail = true
			expPath := filepath.Join(dir, base+".expected.json")
			t.Run(base, func(t *testing.T) {
				runFailFixture(t, rule, base, yamlPath, routesPath, expPath)
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

// runPassFixture asserts that rule emits no diagnostics against yamlPath.
// For Info-severity rules, non-info diagnostics from this rule are the failure criterion.
func runPassFixture(t *testing.T, rule Rule, yamlPath, routesPath string) {
	t.Helper()
	diags := runRuleAgainstFixture(t, rule, yamlPath, routesPath)
	if rule.Severity() == SeverityInfo {
		for _, d := range diags {
			if d.RuleID == rule.ID() && (d.Severity == SeverityError || d.Severity == SeverityWarning) {
				t.Errorf("pass.yaml: rule %s produced non-info diag: %+v", rule.ID(), d)
			}
		}
		return
	}
	if len(diags) != 0 {
		t.Errorf("pass.yaml produced %d diagnostics, want 0:\n%s", len(diags), formatDiags(diags))
	}
}

func runFailFixture(t *testing.T, rule Rule, base, yamlPath, routesPath, expPath string) {
	t.Helper()
	diags := runRuleAgainstFixture(t, rule, yamlPath, routesPath)
	exp := loadExpectation(t, expPath)
	matchExpectation(t, base, rule, exp, diags)
}
