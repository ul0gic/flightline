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

	"github.com/ul0gic/flightline/internal/lint"
)

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
	// sourcePath is omitempty (only set with --state-file), so it may be absent
	// here; assert the required keys are present and no unexpected key leaked in.
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

// Every diagnostic must carry only the locked per-diagnostic keys.
func TestPreflightOutput_DiagnosticKeysStable(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	if len(res.Diagnostics) == 0 {
		t.Fatal("multi-rule fixture produced 0 diagnostics: fixture is broken")
	}
	b, err := json.Marshal(res.Diagnostics)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var diags []map[string]any
	if err := json.Unmarshal(b, &diags); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertDiagnosticKeysStable(t, diags, "preflight output")
}

// Diagnostic ordering must be stable across runs: consumers diff outputs
// between CI runs, so the stream is a wire contract.
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

// Severity must serialize as a lowercase token.
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

// Not pinned to an exact set (live state varies): every ruleId must be
// non-empty and either "schema" or a registered rule ID.
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

func TestPreflightOutput_ModeIsPreflight(t *testing.T) {
	srv := multiRuleFireServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.stable", "1.0.1")
	if res.Mode != "preflight" {
		t.Errorf("Mode = %q, want preflight", res.Mode)
	}
}

// Fixture engineered to trip multiple live rules at once; each branch below
// is shaped to fail a specific rule (see the inline notes).
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
			// Item references something else: IAP not attached.
			_, _ = w.Write([]byte(`{"data":[{"id":"item-1","type":"reviewSubmissionItems","attributes":{"state":"READY_FOR_REVIEW"},"relationships":{"appStoreVersion":{"data":{"type":"appStoreVersions","id":"v-1"}}}}]}`))
		case strings.HasSuffix(path, "/appStoreReviewScreenshot"):
			// No screenshot: fileName + templateUrl both empty.
			_, _ = w.Write([]byte(`{"data":{"id":"rs-1","type":"reviewScreenshots","attributes":{}}}`))
		case strings.HasSuffix(path, "/appInfos"):
			_, _ = w.Write([]byte(`{"data":[{"id":"info-1","type":"appInfos","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}]}`))
		case strings.HasSuffix(path, "/ageRatingDeclaration"):
			// Empty enum strings -> live age-rating not answered.
			_, _ = w.Write([]byte(`{"data":{"id":"ar-1","type":"ageRatingDeclarations","attributes":{}}}`))
		case strings.HasSuffix(path, "/appStoreVersionLocalizations"):
			_, _ = w.Write([]byte(`{"data":[{"id":"loc-1","type":"appStoreVersionLocalizations","attributes":{"locale":"en-US"}}]}`))
		case strings.HasSuffix(path, "/appScreenshotSets"):
			// Only 6.9: 6.7 is missing.
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

// Guards the fixture: a regression that stops it tripping multiple rules
// would silently neuter the stability tests above.
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
