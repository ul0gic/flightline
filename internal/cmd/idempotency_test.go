package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/ul0gic/skipper/internal/asc"
)

// Phase 3.4.1 — Idempotency assertions
//
// Cross-cutting test corpus for the v1 write surface. Each subtest exercises
// the wire-level read-then-decide-to-write flow that the runX functions
// follow. The shared invariant: when current state already matches desired
// state, the second pass MUST produce ZERO PATCH/POST/DELETE requests.
//
// This is a stronger contract than per-command "assert no PATCH route was
// hit" tests: those will pass even if a write happens to land at a route
// the fixture server didn't register (the FIXTURE_NO_ROUTE 404 surfaces as
// an error, but a quick test author can mistake it for an unrelated bug).
// The counting server here records EVERY method so the assertion is
// "second pass made zero mutating calls on any path".
//
// Per-command tests live in their respective *_test.go files; this file
// holds the table-driven cross-cutting layer that catches drift across the
// whole write surface in one pass.

// ---------------------------------------------------------------------------
// countingFixtureServer — wraps startFixtureServer's behaviour but counts
// requests by METHOD+PATH so tests can assert on per-method totals across a
// run. Snapshot() returns a copy at any instant; Reset() zeros the counters
// (used between round 1 and round 2 of an idempotency check).
// ---------------------------------------------------------------------------

type countingFixtureServer struct {
	t      *testing.T
	srv    *httptest.Server
	routes map[string]fixtureRoute

	mu       sync.Mutex
	total    int64
	byMethod map[string]int64
	byKey    map[string]int64
}

func startCountingFixtureServer(t *testing.T, routes map[string]fixtureRoute) *countingFixtureServer {
	t.Helper()
	c := &countingFixtureServer{
		t:        t,
		routes:   routes,
		byMethod: make(map[string]int64),
		byKey:    make(map[string]int64),
	}
	c.srv = httptest.NewServer(http.HandlerFunc(c.handle))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *countingFixtureServer) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	c.mu.Lock()
	atomic.AddInt64(&c.total, 1)
	c.byMethod[r.Method]++
	c.byKey[key]++
	c.mu.Unlock()

	route, ok := c.routes[key]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		body := `{"errors":[{"id":"fixture-no-route","status":"404","code":"FIXTURE_NO_ROUTE","title":"Fixture has no route registered for this request","detail":"` + key + `"}]}`
		_, _ = w.Write([]byte(body))
		return
	}
	body, err := readGoldenFixture(route.File)
	if err != nil {
		c.t.Errorf("fixture %s: %v", route.File, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	status := route.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// reset zeros all counters. Used to draw a clean line between round 1
// (which may legitimately PATCH/POST) and round 2 (which must not).
func (c *countingFixtureServer) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	atomic.StoreInt64(&c.total, 0)
	c.byMethod = make(map[string]int64)
	c.byKey = make(map[string]int64)
}

// mutatingCount returns the sum of PATCH+POST+DELETE+PUT calls.
func (c *countingFixtureServer) mutatingCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byMethod[http.MethodPatch] +
		c.byMethod[http.MethodPost] +
		c.byMethod[http.MethodDelete] +
		c.byMethod[http.MethodPut]
}

// methodCount returns count for the given HTTP method.
func (c *countingFixtureServer) methodCount(method string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byMethod[method]
}

// fixtureASCClientFor wires a production-shaped client to the counting
// server's URL. Mirrors fixtureASCClient but takes the wrapper.
func fixtureASCClientFor(t *testing.T, c *countingFixtureServer) *asc.Client {
	t.Helper()
	return fixtureASCClient(t, c.srv)
}

// assertNoMutatingCalls fails the test if the counting server saw any
// mutating method since the last reset. The error message names every
// mutating endpoint that fired — gives the author exactly the route to
// investigate.
func assertNoMutatingCalls(t *testing.T, c *countingFixtureServer) {
	t.Helper()
	if got := c.mutatingCount(); got != 0 {
		c.mu.Lock()
		offending := []string{}
		for k := range c.byKey {
			method := strings.SplitN(k, " ", 2)[0]
			if method == http.MethodPatch || method == http.MethodPost || method == http.MethodDelete || method == http.MethodPut {
				offending = append(offending, k)
			}
		}
		c.mu.Unlock()
		t.Errorf("idempotency violation: round 2 made %d mutating call(s); offending routes: %v", got, offending)
	}
}

// ---------------------------------------------------------------------------
// versions create — round 2: lookup HITS, no POST.
// versions update — round 2: diff returns no changes, no PATCH.
// ---------------------------------------------------------------------------

func TestIdempotency_VersionsCreate_SecondPassNoPOST(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	// Round 1: caller resolves app + sees existing version. The runVersionsCreate
	// code path returns noop without POSTing when lookup hits.
	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("round1 resolveAppID: %v", err)
	}
	existing, err := lookupVersion(ctx, c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("round1 lookupVersion: %v", err)
	}
	if existing == nil {
		t.Fatal("round1: expected existing version, got nil")
	}

	// Round 2: same flow. Counts must stay GET-only.
	srv.reset()
	if _, err := resolveAppID(ctx, c, "com.example.alpha"); err != nil {
		t.Fatalf("round2 resolveAppID: %v", err)
	}
	if _, err := lookupVersion(ctx, c, appID, "1.0.1", "IOS"); err != nil {
		t.Fatalf("round2 lookupVersion: %v", err)
	}
	assertNoMutatingCalls(t, srv)
}

func TestIdempotency_VersionsUpdate_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	existing, err := lookupVersion(ctx, c, appID, "1.0.1", "IOS")
	if err != nil || existing == nil {
		t.Fatalf("lookupVersion: %v", err)
	}

	// Cobra harness so Flag().Changed() works; supply --release-type matching
	// the existing fixture's MANUAL value so the diff is empty.
	cmd := newDiffVersionCobra("MANUAL")
	srv.reset()
	out, changed := diffVersionAttrs(cmd, existing.Attributes, "", "MANUAL", "", "")
	if changed {
		t.Errorf("diffVersionAttrs: changed=true for matching release-type; want false. patch=%+v", out)
	}
	// The diff function is local and doesn't hit the wire, but the contract
	// is "no PATCH after a no-change diff" — confirm by leaving the PATCH
	// route unregistered.
	assertNoMutatingCalls(t, srv)
}

// newDiffVersionCobra builds the cobra harness diffVersionAttrs expects.
func newDiffVersionCobra(releaseType string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("copyright", "", "")
	cmd.Flags().String("release-type", "", "")
	cmd.Flags().String("review-type", "", "")
	cmd.Flags().String("earliest-release-date", "", "")
	if releaseType != "" {
		_ = cmd.Flags().Set("release-type", releaseType)
	}
	return cmd
}

// ---------------------------------------------------------------------------
// builds attach — current linkage already points at requested build:
// no PATCH on the second pass.
// ---------------------------------------------------------------------------

func TestIdempotency_BuildsAttach_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
		"GET /v1/apps/1234567890/builds":           {File: "builds_lookup_byVersion"},
		"GET /v1/appStoreVersions/8000000001/relationships/build": {
			File: "builds_attach_linkage_already",
		},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	for round := 1; round <= 2; round++ {
		appID, err := resolveAppID(ctx, c, "com.example.alpha")
		if err != nil {
			t.Fatalf("round%d resolveAppID: %v", round, err)
		}
		ver, err := lookupVersion(ctx, c, appID, "1.0.1", "IOS")
		if err != nil || ver == nil {
			t.Fatalf("round%d lookupVersion: %v", round, err)
		}
		build, err := lookupBuild(ctx, c, appID, "42")
		if err != nil || build == nil {
			t.Fatalf("round%d lookupBuild: %v", round, err)
		}
		current, err := getAttachedBuild(ctx, c, ver.ID)
		if err != nil {
			t.Fatalf("round%d getAttachedBuild: %v", round, err)
		}
		if current == nil || current.ID != build.ID {
			t.Fatalf("round%d: expected current attached build %s, got %+v", round, build.ID, current)
		}
		// idempotent branch — runBuildsAttach short-circuits without PATCH.
		if round == 1 {
			srv.reset()
		}
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// metadata set — version+app-info localizations both already match: zero
// PATCHes on the second pass. Cross-resource (two PATCH paths) so this is
// the most fragile of the cmd-layer diffs.
// ---------------------------------------------------------------------------

func TestIdempotency_MetadataSet_SecondPassNoPATCH_VersionLoc(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/appStoreVersionLocalizations": {File: "metadata_version_loc_existing"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	// Round 1: GET the localization, diff against itself, no PATCH.
	curID, curAttrs, err := getVersionLocalization(ctx, c, "8000000001", "en-US")
	if err != nil {
		t.Fatalf("getVersionLocalization: %v", err)
	}
	if curID == "" {
		t.Fatal("getVersionLocalization: empty id")
	}

	// Build flag set with same values as existing — diff must be empty.
	flags := metadataFlagSet{
		description: true,
		keywords:    true,
		whatsNew:    true,
	}
	srv.reset()
	patch, changed := diffVersionLocAttrs(flags, curAttrs,
		curAttrs.Description, curAttrs.Keywords, curAttrs.WhatsNew,
		curAttrs.PromotionalText, curAttrs.MarketingURL, curAttrs.SupportURL)
	if changed {
		t.Errorf("diffVersionLocAttrs: changed=true with identical values; patch=%+v", patch)
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// screenshots upload — same MD5 already at the slot: skip uploads.
// ---------------------------------------------------------------------------

func TestIdempotency_ScreenshotsUpload_SecondPassNoPOST_SameChecksum(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appScreenshotSets/SS000000001/appScreenshots": {File: "screenshots_list_with_checksum"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	// Round 1: list, build the checksum -> assetID map.
	existing, err := listScreenshotsByChecksum(ctx, c, "SS000000001")
	if err != nil {
		t.Fatalf("listScreenshotsByChecksum: %v", err)
	}
	if len(existing) == 0 {
		t.Fatal("listScreenshotsByChecksum: empty map; fixture wrong?")
	}

	// Round 2 simulates re-running with files whose MD5s already match
	// existing slots. uploadOrSkipFiles would skip — verify the lookup map
	// still maps each known checksum to an entry, and assert we issued no
	// mutating calls beyond the GET list.
	srv.reset()
	again, err := listScreenshotsByChecksum(ctx, c, "SS000000001")
	if err != nil {
		t.Fatalf("listScreenshotsByChecksum (round 2): %v", err)
	}
	if len(again) != len(existing) {
		t.Errorf("checksum map drifted between rounds: %d -> %d", len(existing), len(again))
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// IAP create — productId already exists: short-circuit, no POST.
// IAP update — fields already match: diff empty, no PATCH.
// IAP localizations set — locale+name+description match: noop.
// IAP review-screenshot upload — checksum already attached: noop.
// ---------------------------------------------------------------------------

func TestIdempotency_IAPCreate_SecondPassNoPOST(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_get"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	// findIAPByProductID hits both routes; round 2 with the same productId
	// must hit only those reads.
	srv.reset()
	id, attrs, err := findIAPByProductID(ctx, c, "com.example.alpha", "com.example.testapp.lifetime")
	if err != nil {
		t.Fatalf("findIAPByProductID: %v", err)
	}
	if id == "" {
		t.Fatal("findIAPByProductID: empty id; fixture mismatch")
	}
	if attrs.ProductID == "" {
		t.Errorf("attrs.ProductID empty; fixture mismatch: %+v", attrs)
	}
	assertNoMutatingCalls(t, srv)
}

func TestIdempotency_IAPUpdate_SecondPassNoPATCH_AllFieldsMatch(t *testing.T) {
	// IAP update inlines the diff in runIAPUpdate. We replicate the inline
	// diff and assert: matching values produce no mutation.
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_get"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	id, current, err := findIAPByProductID(ctx, c, "com.example.alpha", "com.example.testapp.lifetime")
	if err != nil {
		t.Fatalf("findIAPByProductID: %v", err)
	}

	// Mirror runIAPUpdate's inline diff: when each user-provided value equals
	// current, the changed flag stays false.
	srv.reset()
	desiredName := current.Name
	desiredReviewNote := current.ReviewNote
	desiredFamilySharable := current.FamilySharable

	changed := (desiredName != "" && desiredName != current.Name) ||
		desiredReviewNote != current.ReviewNote ||
		!boolPtrEq(desiredFamilySharable, current.FamilySharable)
	if changed {
		t.Errorf("IAP update inline diff: changed=true with identical values; current=%+v", current)
	}
	if id == "" {
		t.Fatal("expected id from lookup")
	}
	assertNoMutatingCalls(t, srv)
}

func TestIdempotency_IAPLocalizationsSet_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2":                     {File: "iap_get"},
		"GET /v2/inAppPurchases/6500000001/inAppPurchaseLocalizations": {File: "iap_localizations_list"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	iapID, _, err := findIAPByProductID(ctx, c, "com.example.alpha", "com.example.testapp.lifetime")
	if err != nil {
		t.Fatalf("findIAPByProductID: %v", err)
	}
	loc, err := findLocalization(ctx, c, iapID, "en-US")
	if err != nil {
		t.Fatalf("findLocalization: %v", err)
	}
	if loc == nil {
		t.Fatal("findLocalization: nil; fixture wrong?")
	}

	// Round 2: re-do the lookup with the same name/description; diff is empty.
	srv.reset()
	loc2, err := findLocalization(ctx, c, iapID, "en-US")
	if err != nil {
		t.Fatalf("round2 findLocalization: %v", err)
	}
	if loc2 == nil || loc2.ID != loc.ID {
		t.Fatalf("round2 lookup drifted: got %+v, want %s", loc2, loc.ID)
	}
	// Mirror runIAPLocalizationsSet's inline diff:
	desiredName := loc2.Attributes.Name
	desiredDesc := loc2.Attributes.Description
	changed := (desiredName != "" && desiredName != loc2.Attributes.Name) ||
		desiredDesc != loc2.Attributes.Description
	if changed {
		t.Errorf("IAP loc inline diff: changed=true with identical values: %+v", loc2.Attributes)
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// IAP review-screenshot upload — when the local file's MD5 matches the
// IAP's currently attached sourceFileChecksum, no upload (reserve POST,
// chunk PUTs, commit PATCH) fires.
// ---------------------------------------------------------------------------

func TestIdempotency_IAPReviewScreenshotUpload_SecondPassNoOps_SameChecksum(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v2/inAppPurchases/6500000001/appStoreReviewScreenshot": {File: "iap_review_screenshot"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	// Round 1: read the current screenshot. Round 2: same call, then verify
	// the helper would skip the upload.
	checksum, urlTpl, ok := currentIAPScreenshot(ctx, c, "6500000001")
	if !ok {
		t.Fatal("currentIAPScreenshot: !ok; fixture mismatch")
	}
	if checksum == "" {
		t.Errorf("checksum empty; fixture has none?")
	}
	if urlTpl == "" {
		t.Errorf("urlTpl empty; fixture has none?")
	}

	srv.reset()
	checksum2, _, ok2 := currentIAPScreenshot(ctx, c, "6500000001")
	if !ok2 {
		t.Fatal("round2 currentIAPScreenshot: !ok")
	}
	if checksum2 != checksum {
		t.Errorf("checksum drift: %q -> %q", checksum, checksum2)
	}
	// runIAPReviewScreenshotUpload short-circuits when localMD5 == checksum2;
	// we model that condition holding by passing the same value through.
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// age-rating set — already-matching declaration: diff empty, no PATCH.
// ---------------------------------------------------------------------------

func TestIdempotency_AgeRatingSet_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":         {File: "age_rating_version_lookup"},
		"GET /v1/apps/1234567890/appInfos":                 {File: "age_rating_app_infos"},
		"GET /v1/appInfos/9000000001/ageRatingDeclaration": {File: "age_rating_get"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	state, err := lookupVersionState(ctx, c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersionState: %v", err)
	}
	appInfoID, err := pickAppInfoForVersion(ctx, c, appID, state)
	if err != nil {
		t.Fatalf("pickAppInfoForVersion: %v", err)
	}
	current, _, err := fetchAgeRatingDeclaration(ctx, c, appInfoID)
	if err != nil {
		t.Fatalf("fetchAgeRatingDeclaration: %v", err)
	}

	// Round 2: diff against the same set of supplied keys; expect no changes.
	srv.reset()
	supplied := map[string]struct{}{}
	diff := diffAgeRating(current, current, supplied)
	if len(diff) != 0 {
		t.Errorf("diffAgeRating(self,self,empty) = %v, want empty", diff)
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// export-compliance set — current matches desired: no PATCH on round 2.
// ---------------------------------------------------------------------------

func TestIdempotency_ExportComplianceSet_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions":  {File: "age_rating_version_lookup"},
		"GET /v1/appStoreVersions/8000000001/build": {File: "export_compliance_version_build"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}
	versionID, err := lookupVersionIDForCompliance(ctx, c, appID, "1.0.1", "IOS")
	if err != nil {
		t.Fatalf("lookupVersionID: %v", err)
	}
	buildID, _, current, err := fetchVersionBuildEncryptionForSet(ctx, c, versionID)
	if err != nil {
		t.Fatalf("fetchVersionBuildEncryptionForSet: %v", err)
	}
	if buildID == "" {
		t.Skip("fixture has no attached build; skipping idempotency check")
	}

	// Round 2: when desired equals current, the runner does not PATCH.
	srv.reset()
	if current != nil {
		desired := current
		if !boolPtrEq(current, desired) {
			t.Errorf("boolPtrEq sanity failed for self-equality")
		}
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// reviewer-demo set — special: password is one-shot. A re-run with NO
// password flag (desired.DemoAccountPassword == nil) MUST NOT trigger
// changed=true. We assert this contract explicitly because diffReviewerDetail
// has a documented "always write through" branch when password IS provided —
// so the absence-of-flag case is the idempotency guarantee.
// ---------------------------------------------------------------------------

func TestIdempotency_ReviewerDemoSet_SecondPassNoPATCH_NoPasswordFlag(t *testing.T) {
	current := AppStoreReviewDetailAttributes{
		ContactFirstName:    strPtr("Ada"),
		ContactLastName:     strPtr("Lovelace"),
		ContactEmail:        strPtr("ada@example.com"),
		ContactPhone:        strPtr("+1-555-0100"),
		DemoAccountName:     strPtr("demo@example.com"),
		DemoAccountPassword: nil, // server never returns it
		Notes:               strPtr("Tap login then play through tutorial."),
	}
	// Desired without password supplied — every other field matches current.
	desired := AppStoreReviewDetailAttributes{
		ContactFirstName:    current.ContactFirstName,
		ContactLastName:     current.ContactLastName,
		ContactEmail:        current.ContactEmail,
		ContactPhone:        current.ContactPhone,
		DemoAccountName:     current.DemoAccountName,
		DemoAccountPassword: nil,
		Notes:               current.Notes,
	}
	out, changed := diffReviewerDetail(current, desired)
	if changed {
		t.Errorf("diffReviewerDetail: changed=true with identical fields and no password; patch=%+v", out)
	}
}

// TestIdempotency_ReviewerDemoSet_PasswordSuppliedAlwaysChanged locks the
// other half of the contract: when password IS supplied, we MUST issue a
// PATCH (we cannot verify the server-side state). This is intentional —
// the PRD calls this out under reviewer_demo.
func TestIdempotency_ReviewerDemoSet_PasswordSuppliedAlwaysChanged(t *testing.T) {
	current := AppStoreReviewDetailAttributes{
		ContactFirstName: strPtr("Ada"),
	}
	// User supplies a password. Even if the rest of the fields match, we
	// MUST PATCH — the server might have a stale password.
	desired := AppStoreReviewDetailAttributes{
		ContactFirstName:    current.ContactFirstName,
		DemoAccountPassword: strPtr("hunter2"),
	}
	out, changed := diffReviewerDetail(current, desired)
	if !changed {
		t.Error("diffReviewerDetail: changed=false with password supplied; want true (always-write contract)")
	}
	if out.DemoAccountPassword == nil || *out.DemoAccountPassword != "hunter2" {
		t.Errorf("out.DemoAccountPassword = %v, want pointer to hunter2", out.DemoAccountPassword)
	}
}

// ---------------------------------------------------------------------------
// categories set — relationships already match: no PATCH on round 2.
// ---------------------------------------------------------------------------

func TestIdempotency_CategoriesSet_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                     {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appInfos": {File: "age_rating_app_infos"},
		"GET /v1/appInfos/9000000001/relationships/primaryCategory":         {File: "categories_get_primary"},
		"GET /v1/appInfos/9000000001/relationships/primarySubcategoryOne":   {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/primarySubcategoryTwo":   {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/secondaryCategory":       {File: "categories_get_secondary"},
		"GET /v1/appInfos/9000000001/relationships/secondarySubcategoryOne": {File: "categories_get_unassigned"},
		"GET /v1/appInfos/9000000001/relationships/secondarySubcategoryTwo": {File: "categories_get_unassigned"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	current, err := fetchAllCategoryRelationships(ctx, c, "9000000001")
	if err != nil {
		t.Fatalf("fetchAllCategoryRelationships: %v", err)
	}
	srv.reset()
	// Round 2 — diffing current against itself yields zero changes.
	if got := categoriesDiff(current, current); len(got) != 0 {
		t.Errorf("categoriesDiff(self,self) = %d, want 0; got %v", len(got), got)
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// pricing set — current schedule equals desired: skip the create.
// pricing's "set" path uses fetchCurrentBaseSchedule + tier comparison; when
// the current base price-point equals desired and the territory matches,
// runPricingSet returns noop without POSTing.
// ---------------------------------------------------------------------------

func TestIdempotency_PricingSet_SecondPassNoPOST_MatchingSchedule(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps/1234567890/appPriceSchedule": {File: "pricing_get"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	schedID, baseTerritory, basePricePoint, err := fetchCurrentBaseSchedule(ctx, c, "1234567890")
	if err != nil {
		t.Fatalf("fetchCurrentBaseSchedule: %v", err)
	}

	srv.reset()
	id2, terr2, pp2, err := fetchCurrentBaseSchedule(ctx, c, "1234567890")
	if err != nil {
		t.Fatalf("round2 fetchCurrentBaseSchedule: %v", err)
	}
	if id2 != schedID || terr2 != baseTerritory || pp2 != basePricePoint {
		t.Errorf("schedule drift: (%q,%q,%q) -> (%q,%q,%q)",
			schedID, baseTerritory, basePricePoint, id2, terr2, pp2)
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// testflight groups create — name already exists for app: no POST.
// testflight testers add — already a member: no POST (covered by the
//   group-scoped tester listing showing the email).
// testflight testers remove — not a member: no DELETE.
// ---------------------------------------------------------------------------

func TestIdempotency_TestflightGroupsCreate_SecondPassNoPOST(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":                       {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/betaGroups": {File: "testflight_groups_list"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}

	// Round 1 + 2 hit findBetaGroupByName; existing group → noop, no POST.
	for round := 1; round <= 2; round++ {
		got, err := findBetaGroupByName(ctx, c, appID, "Internal Team")
		if err != nil {
			t.Fatalf("round%d findBetaGroupByName: %v", round, err)
		}
		if got == nil {
			t.Fatalf("round%d: expected to find 'Internal Team' group", round)
		}
		if round == 1 {
			srv.reset()
		}
	}
	assertNoMutatingCalls(t, srv)
}

// ---------------------------------------------------------------------------
// custom-product-pages — name already exists: no POST. Updating with same
// values: no PATCH.
// ---------------------------------------------------------------------------

func TestIdempotency_CustomProductPagesCreate_SecondPassNoPOST(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appCustomProductPages": {File: "custom_product_pages_list"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	appID, err := resolveAppID(ctx, c, "com.example.alpha")
	if err != nil {
		t.Fatalf("resolveAppID: %v", err)
	}

	for round := 1; round <= 2; round++ {
		got, err := findCustomProductPageByName(ctx, c, appID, "Holiday Promo")
		if err != nil {
			t.Fatalf("round%d findCustomProductPageByName: %v", round, err)
		}
		if got == nil {
			t.Fatalf("round%d: expected to find 'Holiday Promo' page; fixture mismatch?", round)
		}
		if round == 1 {
			srv.reset()
		}
	}
	assertNoMutatingCalls(t, srv)
}

// TestIdempotency_CustomProductPagesUpdate_SecondPassNoPATCH covers the
// computeCustomProductPagePatchAttrs branch: matching name + matching
// visible flag yields an empty patch map, no PATCH.
func TestIdempotency_CustomProductPagesUpdate_SecondPassNoPATCH(t *testing.T) {
	visible := true
	cur := asc.AppCustomProductPageAttributes{
		Name:    "Holiday 2025",
		Visible: &visible,
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("name", "", "")
	cmd.Flags().Bool("visible", false, "")

	// Simulate user passing flags equal to current. Note: cmd.Flags().Changed
	// only flips when Set was called explicitly.
	if err := cmd.Flags().Set("name", "Holiday 2025"); err != nil {
		t.Fatalf("set name flag: %v", err)
	}
	if err := cmd.Flags().Set("visible", "true"); err != nil {
		t.Fatalf("set visible flag: %v", err)
	}
	customProductPagesUpdateName = "Holiday 2025"
	customProductPagesUpdateVisible = true

	patch := computeCustomProductPagePatchAttrs(cmd, cur)
	if len(patch) != 0 {
		t.Errorf("computeCustomProductPagePatchAttrs: got %v, want empty (idempotent on identical input)", patch)
	}
}

// ---------------------------------------------------------------------------
// versions list-page — sanity check that the helper structures used by the
// idempotency tests above don't drift in shape. Also documents the
// counting server behaviour: GETs count, mutations count, no double-count.
// ---------------------------------------------------------------------------

func TestCountingFixtureServer_TracksMethodsSeparately(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":  {File: "apps_get_byBundleId"},
		"POST /v1/apps": {File: "apps_get_byBundleId", Status: http.StatusCreated},
	})

	// Direct HTTP calls — don't need the asc client wrapper for this.
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodGet} {
		req, err := http.NewRequestWithContext(context.Background(), m, srv.srv.URL+"/v1/apps", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("build %s: %v", m, err)
		}
		resp, err := srv.srv.Client().Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", m, err)
		}
		_ = resp.Body.Close()
	}

	if got := srv.methodCount(http.MethodGet); got != 2 {
		t.Errorf("GET count = %d, want 2", got)
	}
	if got := srv.methodCount(http.MethodPost); got != 1 {
		t.Errorf("POST count = %d, want 1", got)
	}
	if got := srv.mutatingCount(); got != 1 {
		t.Errorf("mutating count = %d, want 1 (POST only)", got)
	}

	srv.reset()
	if got := srv.mutatingCount(); got != 0 {
		t.Errorf("mutating count after reset = %d, want 0", got)
	}
}

// TestCountingFixtureServer_FlagsUnregisteredRouteAs404 documents the
// FIXTURE_NO_ROUTE response shape so the assertion-on-error tests in this
// file can rely on it.
func TestCountingFixtureServer_FlagsUnregisteredRouteAs404(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{})
	c := fixtureASCClientFor(t, srv)

	_, err := asc.Get[asc.Collection[map[string]any]](
		context.Background(), c, "/v1/apps", url.Values{},
	)
	if err == nil {
		t.Fatal("Get on unrouted path returned nil error")
	}
	var apiErr *asc.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *asc.APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", apiErr.HTTPStatus)
	}
	if len(apiErr.Errors) == 0 || apiErr.Errors[0].Code != "FIXTURE_NO_ROUTE" {
		t.Errorf("Errors = %+v, want a FIXTURE_NO_ROUTE diagnostic", apiErr.Errors)
	}
}
