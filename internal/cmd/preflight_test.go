package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/lint"
)

// preflightFor runs the same rule pass runPreflight does, but bypasses
// newClient (which needs a real .p8) and Render (which writes to stdout).
// Returns the LintResult so tests can assert on it directly.
func preflightFor(t *testing.T, c *asc.Client, bundleID, versionStr string) *LintResult {
	t.Helper()
	stateInput, sourcePath, schemaDiags, err := resolvePreflightState(context.Background(), c, bundleID, versionStr, "IOS")
	if err != nil {
		t.Fatalf("resolvePreflightState: %v", err)
	}
	rules := lint.All()
	runner := lint.NewRunner(rules)
	checkCtx := lint.CheckContext{
		State:      stateInput,
		Client:     c,
		BundleID:   bundleID,
		Version:    versionStr,
		Live:       true,
		Ctx:        context.Background(),
		SourcePath: sourcePath,
	}
	merged := mergeSchemaIntoLint(schemaDiags, runner.Run(checkCtx))
	return &LintResult{
		BundleID:    bundleID,
		Version:     versionStr,
		SourcePath:  sourcePath,
		Mode:        "preflight",
		Diagnostics: merged,
		Summary:     summarize(merged),
	}
}

// happyPathServer simulates a fully clean live state.
func happyPathServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"v-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1","platform":"IOS"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = w.Write([]byte(`{"data":{"id":"b-1","type":"builds","attributes":{"version":"42","processingState":"VALID","usesNonExemptEncryption":false}}}`))
		case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/reviewSubmissions"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/appInfos"):
			_, _ = w.Write([]byte(`{"data":[{"id":"info-1","type":"appInfos","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/ageRatingDeclaration"):
			_, _ = w.Write([]byte(`{"data":{"id":"ar-1","type":"ageRatingDeclarations","attributes":{"violenceCartoonOrFantasy":"NONE","violenceRealistic":"NONE","profanityOrCrudeHumor":"NONE","matureOrSuggestiveThemes":"NONE","horrorOrFearThemes":"NONE","medicalOrTreatmentInformation":"NONE","alcoholTobaccoOrDrugUseOrReferences":"NONE","contests":"NONE","sexualContentOrNudity":"NONE","sexualContentGraphicAndNudity":"NONE","gambling":false,"unrestrictedWebAccess":false}}}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersionLocalizations"):
			_, _ = w.Write([]byte(`{"data":[{"id":"loc-1","type":"appStoreVersionLocalizations","attributes":{"locale":"en-US"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appScreenshotSets"):
			_, _ = w.Write([]byte(`{"data":[{"id":"set-67","type":"appScreenshotSets","attributes":{"screenshotDisplayType":"APP_IPHONE_67"}},{"id":"set-69","type":"appScreenshotSets","attributes":{"screenshotDisplayType":"APP_IPHONE_69"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appScreenshots"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/categories"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/customProductPages"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/betaGroups"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			// Default to empty data — most fetch helpers swallow benign errors.
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRunPreflight_LiveOnlyHappyPathHasNoErrors runs every rule against a
// fixture that simulates a fully clean version. We tolerate Info-severity
// findings (the account-deletion-attested reminder always fires).
func TestRunPreflight_LiveOnlyHappyPathHasNoErrors(t *testing.T) {
	srv := happyPathServer(t)
	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.x", "1.0.1")
	if res.Summary.Error != 0 {
		t.Errorf("Summary.Error = %d, want 0; diagnostics: %+v", res.Summary.Error, res.Diagnostics)
	}
	if res.Mode != "preflight" {
		t.Errorf("Mode = %q, want preflight", res.Mode)
	}
}

// TestRunPreflight_NoBuildSurfacesError seeds a state where the version's
// /build returns null data; build.attached-and-valid should fire.
func TestRunPreflight_NoBuildSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"id":"v-1","type":"appStoreVersions","attributes":{"versionString":"1.0.1","platform":"IOS"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/build"):
			_, _ = w.Write([]byte(`{"data":{"id":"","type":"builds","attributes":{}}}`))
		case strings.HasSuffix(r.URL.Path, "/inAppPurchasesV2"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/reviewSubmissions"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case strings.HasSuffix(r.URL.Path, "/appInfos"):
			_, _ = w.Write([]byte(`{"data":[{"id":"info-1","type":"appInfos","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/ageRatingDeclaration"):
			_, _ = w.Write([]byte(`{"data":{"id":"ar-1","type":"ageRatingDeclarations","attributes":{"violenceCartoonOrFantasy":"NONE","violenceRealistic":"NONE","profanityOrCrudeHumor":"NONE","matureOrSuggestiveThemes":"NONE","horrorOrFearThemes":"NONE","medicalOrTreatmentInformation":"NONE","alcoholTobaccoOrDrugUseOrReferences":"NONE","contests":"NONE","sexualContentOrNudity":"NONE","sexualContentGraphicAndNudity":"NONE","gambling":false,"unrestrictedWebAccess":false}}}`))
		case strings.HasSuffix(r.URL.Path, "/appStoreVersionLocalizations"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	t.Cleanup(srv.Close)

	c := fixtureASCClient(t, srv)
	res := preflightFor(t, c, "com.example.x", "1.0.1")
	found := false
	for _, d := range res.Diagnostics {
		if d.RuleID == "build.attached-and-valid" && d.Severity == lint.SeverityError {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected build.attached-and-valid error; diagnostics: %+v", res.Diagnostics)
	}
}

// TestPreflightResult_JSONShapeStable freezes the preflight JSON envelope
// (shared shape with lint).
func TestPreflightResult_JSONShapeStable(t *testing.T) {
	res := &LintResult{
		BundleID: "com.example.x",
		Version:  "1.0.1",
		Mode:     "preflight",
		Summary:  LintResultSummary{Error: 1, Warning: 2, Info: 3},
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"bundleId", "version", "mode", "diagnostics", "summary"} {
		if _, ok := probe[k]; !ok {
			t.Errorf("preflight JSON missing required key %q", k)
		}
	}
}
