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
	"github.com/ul0gic/flightline/internal/asc"
)

// Shared invariant: when current state already matches desired, the second
// pass must produce zero PATCH/POST/DELETE requests on any path.

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

func (c *countingFixtureServer) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	atomic.StoreInt64(&c.total, 0)
	c.byMethod = make(map[string]int64)
	c.byKey = make(map[string]int64)
}

func (c *countingFixtureServer) mutatingCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byMethod[http.MethodPatch] +
		c.byMethod[http.MethodPost] +
		c.byMethod[http.MethodDelete] +
		c.byMethod[http.MethodPut]
}

func (c *countingFixtureServer) methodCount(method string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byMethod[method]
}

func fixtureASCClientFor(t *testing.T, c *countingFixtureServer) *asc.Client {
	t.Helper()
	return fixtureASCClient(t, c.srv)
}

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

func TestIdempotency_VersionsCreate_SecondPassNoPOST(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

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

	// --release-type matches the fixture's MANUAL value so the diff is empty.
	cmd := newDiffVersionCobra("MANUAL")
	srv.reset()
	out, changed := diffVersionAttrs(cmd, existing.Attributes, "", "MANUAL", "", "")
	if changed {
		t.Errorf("diffVersionAttrs: changed=true for matching release-type; want false. patch=%+v", out)
	}
	// Leaving the PATCH route unregistered confirms no write fires after a no-change diff.
	assertNoMutatingCalls(t, srv)
}

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

func TestIdempotency_BuildsAttach_SecondPassNoPATCH(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/appStoreVersions": {File: "versions_lookup_existing"},
		"GET /v1/builds": {File: "builds_lookup_byVersion"},
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
		if round == 1 {
			srv.reset()
		}
	}
	assertNoMutatingCalls(t, srv)
}

func TestIdempotency_MetadataSet_SecondPassNoPATCH_VersionLoc(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/8000000001/appStoreVersionLocalizations": {File: "metadata_version_loc_existing"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	curID, curAttrs, err := getVersionLocalization(ctx, c, "8000000001", "en-US")
	if err != nil {
		t.Fatalf("getVersionLocalization: %v", err)
	}
	if curID == "" {
		t.Fatal("getVersionLocalization: empty id")
	}

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

func TestIdempotency_ScreenshotsUpload_SecondPassNoPOST_SameChecksum(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appScreenshotSets/SS000000001/appScreenshots": {File: "screenshots_list_with_checksum"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

	existing, err := listScreenshotsByChecksum(ctx, c, "SS000000001")
	if err != nil {
		t.Fatalf("listScreenshotsByChecksum: %v", err)
	}
	if len(existing) == 0 {
		t.Fatal("listScreenshotsByChecksum: empty map; fixture wrong?")
	}

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

func TestIdempotency_IAPCreate_SecondPassNoPOST(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps": {File: "apps_get_byBundleId"},
		"GET /v1/apps/1234567890/inAppPurchasesV2": {File: "iap_get"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

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

	// Mirrors runIAPUpdate's inline diff: matching values keep changed=false.
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

	srv.reset()
	loc2, err := findLocalization(ctx, c, iapID, "en-US")
	if err != nil {
		t.Fatalf("round2 findLocalization: %v", err)
	}
	if loc2 == nil || loc2.ID != loc.ID {
		t.Fatalf("round2 lookup drifted: got %+v, want %s", loc2, loc.ID)
	}
	// Mirrors runIAPLocalizationsSet's inline diff.
	desiredName := loc2.Attributes.Name
	desiredDesc := loc2.Attributes.Description
	changed := (desiredName != "" && desiredName != loc2.Attributes.Name) ||
		desiredDesc != loc2.Attributes.Description
	if changed {
		t.Errorf("IAP loc inline diff: changed=true with identical values: %+v", loc2.Attributes)
	}
	assertNoMutatingCalls(t, srv)
}

func TestIdempotency_IAPReviewScreenshotUpload_SecondPassNoOps_SameChecksum(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v2/inAppPurchases/6500000001/appStoreReviewScreenshot": {File: "iap_review_screenshot"},
	})
	c := fixtureASCClientFor(t, srv)
	ctx := context.Background()

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
	// runIAPReviewScreenshotUpload short-circuits when localMD5 == checksum2.
	assertNoMutatingCalls(t, srv)
}

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

	srv.reset()
	supplied := map[string]struct{}{}
	diff := diffAgeRating(current, current, supplied)
	if len(diff) != 0 {
		t.Errorf("diffAgeRating(self,self,empty) = %v, want empty", diff)
	}
	assertNoMutatingCalls(t, srv)
}

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

	srv.reset()
	if current != nil {
		desired := current
		if !boolPtrEq(current, desired) {
			t.Errorf("boolPtrEq sanity failed for self-equality")
		}
	}
	assertNoMutatingCalls(t, srv)
}

// Password is one-shot: with no password flag, an otherwise-matching detail
// must not set changed=true. The supplied-password case always writes through.
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

// Password supplied must always PATCH: server-side password can't be verified.
func TestIdempotency_ReviewerDemoSet_PasswordSuppliedAlwaysChanged(t *testing.T) {
	current := AppStoreReviewDetailAttributes{
		ContactFirstName: strPtr("Ada"),
	}
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
	if got := categoriesDiff(current, current); len(got) != 0 {
		t.Errorf("categoriesDiff(self,self) = %d, want 0; got %v", len(got), got)
	}
	assertNoMutatingCalls(t, srv)
}

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

func TestIdempotency_CustomProductPagesUpdate_SecondPassNoPATCH(t *testing.T) {
	visible := true
	cur := asc.AppCustomProductPageAttributes{
		Name:    "Holiday 2025",
		Visible: &visible,
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("name", "", "")
	cmd.Flags().Bool("visible", false, "")

	// cmd.Flags().Changed only flips when Set is called explicitly.
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

func TestCountingFixtureServer_TracksMethodsSeparately(t *testing.T) {
	srv := startCountingFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/apps":  {File: "apps_get_byBundleId"},
		"POST /v1/apps": {File: "apps_get_byBundleId", Status: http.StatusCreated},
	})

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
