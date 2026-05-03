package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ul0gic/skipper/internal/lint"
)

// TestPreflightOutput_TopLevelKeysStable runs preflight against a
// synthetic ASC where multiple rules can co-fire and asserts the
// top-level JSON envelope has exactly the locked key set.
func TestPreflightOutput_TopLevelKeysStable(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")

	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := keysOf(probe)
	// Preflight uses the same LintResult envelope; sourcePath only
	// surfaces when --state-file was given (omitempty), so the test
	// path here may or may not include it. Assert the required keys
	// are all present and no unexpected keys leaked in.
	required := []string{"bundleId", "diagnostics", "mode", "summary", "version"}
	for _, k := range required {
		if _, ok := probe[k]; !ok {
			t.Errorf("preflight JSON missing required key %q (got: %v)", k, got)
		}
	}
	for _, k := range got {
		known := false
		for _, allowed := range stableLintTopLevelKeys {
			if k == allowed {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("unexpected preflight top-level key %q (allowed: %v)",
				k, stableLintTopLevelKeys)
		}
	}
}

// TestPreflightOutput_DiagnosticKeysStable: every diagnostic emitted by
// a co-firing fixture must have only the locked per-diagnostic keys.
func TestPreflightOutput_DiagnosticKeysStable(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	if len(res.Diagnostics) == 0 {
		t.Fatal("multi-rule fixture produced 0 diagnostics — fixture is broken")
	}
	b, err := json.Marshal(res.Diagnostics)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var diags []map[string]any
	if err := json.Unmarshal(b, &diags); err != nil {
		t.Fatalf("decode: %v", err)
	}
	union := map[string]struct{}{}
	for _, d := range diags {
		for k := range d {
			union[k] = struct{}{}
		}
	}
	got := make([]string, 0, len(union))
	for k := range union {
		got = append(got, k)
	}
	sort.Strings(got)
	for _, k := range got {
		known := false
		for _, allowed := range stableDiagnosticKeys {
			if k == allowed {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("unexpected diagnostic key %q in preflight output (want subset of %v)",
				k, stableDiagnosticKeys)
		}
	}
	// At least these three must appear when any diagnostic fires.
	for _, want := range []string{"ruleId", "severity", "message"} {
		seen := false
		for _, k := range got {
			if k == want {
				seen = true
				break
			}
		}
		if !seen {
			t.Errorf("required key %q never appeared on preflight diagnostics: %v",
				want, got)
		}
	}
}

// TestPreflightOutput_DiagnosticsAreOrderedStably runs preflight twice
// against the same fixture and asserts the diagnostic stream is byte-
// identical. Stable ordering across runs is a wire-contract requirement
// — consumers that diff outputs between CI runs depend on it.
func TestPreflightOutput_DiagnosticsAreOrderedStably(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	a := preflightFor(t, c, "com.example.stable", "1.0.1")
	b := preflightFor(t, c, "com.example.stable", "1.0.1")
	if !reflect.DeepEqual(a.Diagnostics, b.Diagnostics) {
		t.Errorf("diagnostic ordering drift between runs:\nrun1=%+v\nrun2=%+v",
			a.Diagnostics, b.Diagnostics)
	}
}

// TestPreflightOutput_SeverityIsLowercaseString asserts every diagnostic
// in a co-firing scenario serializes severity as a lowercase token.
func TestPreflightOutput_SeverityIsLowercaseString(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe struct {
		Diagnostics []struct {
			Severity string `json:"severity"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("decode: %v", err)
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

// TestPreflightOutput_AllRuleIDsAppearOnce asserts that when a rule
// fires once per resource, each diagnostic carries a non-empty,
// stable-form ruleId. We don't pin to an exact set (live state varies)
// but we DO assert no ruleId is "" and that the only ruleId values we
// see are either "schema" or one of the registered rule IDs.
func TestPreflightOutput_RuleIDsBelongToRegistry(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	registered := map[string]struct{}{"schema": {}}
	for _, r := range lint.All() {
		registered[r.ID()] = struct{}{}
	}
	for _, d := range res.Diagnostics {
		if d.RuleID == "" {
			t.Errorf("empty ruleId on diagnostic: %+v", d)
			continue
		}
		if _, ok := registered[d.RuleID]; !ok {
			t.Errorf("diagnostic ruleId %q is not registered or 'schema'", d.RuleID)
		}
	}
}

// TestPreflightOutput_ModeIsPreflight verifies the envelope's `mode`
// field discriminates between lint and preflight runs.
func TestPreflightOutput_ModeIsPreflight(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	if res.Mode != "preflight" {
		t.Errorf("Mode = %q, want preflight", res.Mode)
	}
}

// multiRuleFireServer is a fixture that intentionally trips multiple
// live rules at once: build is PROCESSING (build.attached-and-valid),
// IAP is READY_TO_SUBMIT but submission has no items
// (iap.attached-to-review-submission), age-rating live answers are
// blank (version.age-rating-answered), screenshots fixture is missing
// the 6.7" device set (screenshots.required-devices), and IAP review
// screenshot is unset (iap.review-screenshot-exists).
func multiRuleFireServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		path := r.URL.Path
		switch {
		case path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.stable"}}]}`))
		case strings.HasSuffix(path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"v-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1","platform":"IOS"}}]}`))
		case strings.HasSuffix(path, "/build"):
			_, _ = w.Write([]byte(`{"data":{"id":"b-1","type":"builds","attributes":{"version":"42","processingState":"PROCESSING"}}}`))
		case strings.HasSuffix(path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[{"id":"iap-A","type":"inAppPurchases","attributes":{"productId":"com.example.stable.lifetime","state":"READY_TO_SUBMIT"}}]}`))
		case path == "/v1/reviewSubmissions":
			_, _ = w.Write([]byte(`{"data":[{"id":"sub-1","type":"reviewSubmissions","attributes":{"state":"READY_FOR_REVIEW"}}]}`))
		case strings.HasSuffix(path, "/items"):
			// Item references something else — IAP not attached.
			_, _ = w.Write([]byte(`{"data":[{"id":"item-1","type":"reviewSubmissionItems","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"appStoreVersion":{"data":{"type":"appStoreVersions","id":"v-1"}}}}]}`))
		case strings.HasSuffix(path, "/appStoreReviewScreenshot"):
			// No screenshot — fileName + templateUrl both empty.
			_, _ = w.Write([]byte(`{"data":{"id":"rs-1","type":"reviewScreenshots","attributes":{}}}`))
		case strings.HasSuffix(path, "/appInfos"):
			_, _ = w.Write([]byte(`{"data":[{"id":"info-1","type":"appInfos","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}]}`))
		case strings.HasSuffix(path, "/ageRatingDeclaration"):
			// Empty enum strings -> live age-rating not answered.
			_, _ = w.Write([]byte(`{"data":{"id":"ar-1","type":"ageRatingDeclarations","attributes":{}}}`))
		case strings.HasSuffix(path, "/appStoreVersionLocalizations"):
			_, _ = w.Write([]byte(`{"data":[{"id":"loc-1","type":"appStoreVersionLocalizations","attributes":{"locale":"en-US"}}]}`))
		case strings.HasSuffix(path, "/appScreenshotSets"):
			// Only 6.9 — 6.7 is missing.
			_, _ = w.Write([]byte(`{"data":[{"id":"set-69","type":"appScreenshotSets","attributes":{"screenshotDisplayType":"APP_IPHONE_69"}}]}`))
		case strings.HasSuffix(path, "/appScreenshots"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPreflightOutput_MultiRuleFire confirms the fixture above actually
// trips at least three independent rules. Without this guard, future
// regressions in the fixture (e.g. an endpoint flips to "data": null)
// could silently neuter the stability tests above.
func TestPreflightOutput_MultiRuleFire(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	_ = context.Background()
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	rulesFired := map[string]struct{}{}
	for _, d := range res.Diagnostics {
		rulesFired[d.RuleID] = struct{}{}
	}
	want := []string{
		"build.attached-and-valid",
		"iap.attached-to-review-submission",
		"iap.review-screenshot-exists",
		"version.age-rating-answered",
		"screenshots.required-devices",
	}
	for _, w := range want {
		if _, ok := rulesFired[w]; !ok {
			t.Errorf("expected rule %s to fire; fired: %v\nfull diags: %+v",
				w, keysOfStruct(rulesFired), res.Diagnostics)
		}
	}
}

func keysOfStruct(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
