// apply_idempotency_test.go — Phase 4.3.3 Apply idempotency contract.
//
// The L2 keystone write contract: applying a desired state.yaml to live
// ASC, then re-applying the same file against the same backend, must
// produce ZERO mutating wire calls (PATCH/POST/DELETE) on the second run.
//
// This file backs the contract end-to-end against a stateful in-memory
// fixture server that:
//
//  1. Serves an initial live state DIFFERENT from desired.
//  2. Reflects every PATCH/POST it receives into its in-memory model.
//  3. Counts requests by method + path so the second run can be asserted
//     to be GET-only.
//
// Surfaces covered (in order of dispatch in apply.go):
//   - /spec/version/copyright              (PATCH /v1/appStoreVersions/{id})
//   - /spec/ageRating/<field>              (PATCH /v1/ageRatingDeclarations/{id})
//   - /spec/categories/primary             (PATCH /v1/appInfos/{id}/relationships/primaryCategory)
//   - /spec/metadata/locales/en-US/description (PATCH /v1/appStoreVersionLocalizations/{id})
//   - /spec/iap/products/<id>              (POST /v2/inAppPurchases)
//
// Excluded by design:
//   - screenshots, IAP review screenshot, customProductPages.localizations
//     return typed errors deferring to L1 verbs (QA-010); they're not
//     subject to the mutating-call contract here.

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ul0gic/skipper/internal/config"
	"github.com/ul0gic/skipper/internal/plan"
)

// statefulApplyFixture is an httptest server that holds an in-memory
// model of every resource Apply might touch and reflects PATCHes/POSTs
// into that model so the second fetch sees the new state.
//
// All access goes through mu so the fixture is safe to use across
// concurrent dispatchers (Apply itself is sequential, but t.Parallel
// might race other tests sharing a process; mu is cheap insurance).
type statefulApplyFixture struct {
	t  *testing.T
	mu sync.Mutex

	srv *httptest.Server

	// Mutable model.
	versionAttrs           map[string]any // /v1/appStoreVersions/VER1
	ageRatingAttrs         map[string]any // /v1/ageRatingDeclarations/AR1
	primaryCategory        string
	secondaryCategory      string
	versionLocaleEnUSAttrs map[string]any // VL1 attrs
	iaps                   []iapEntry
	nextIAPID              int

	// Counters.
	totalRequests   int
	byMethod        map[string]int
	mutatingByRoute map[string]int // method+path → count, only tracked for non-GET
}

type iapEntry struct {
	id    string
	attrs map[string]any
}

func newStatefulApplyFixture(t *testing.T) *statefulApplyFixture {
	t.Helper()
	f := &statefulApplyFixture{
		t: t,
		versionAttrs: map[string]any{
			"versionString": "1.0",
			"platform":      "IOS",
			"copyright":     "© OLD COPYRIGHT",
			"releaseType":   "MANUAL",
		},
		ageRatingAttrs: map[string]any{
			"violenceCartoonOrFantasy": "NONE",
			"gambling":                 false,
		},
		primaryCategory:   "EDUCATION",
		secondaryCategory: "REFERENCE",
		versionLocaleEnUSAttrs: map[string]any{
			"locale":          "en-US",
			"description":     "old description",
			"keywords":        "a,b",
			"whatsNew":        "old whats new",
			"promotionalText": "old promo",
			"marketingUrl":    "https://x.com/m",
			"supportUrl":      "https://x.com/s",
		},
		iaps:            nil, // start empty so apply round 1 creates one
		nextIAPID:       7000,
		byMethod:        make(map[string]int),
		mutatingByRoute: make(map[string]int),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *statefulApplyFixture) resetCounters() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totalRequests = 0
	f.byMethod = make(map[string]int)
	f.mutatingByRoute = make(map[string]int)
}

func (f *statefulApplyFixture) mutatingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, v := range f.mutatingByRoute {
		_ = k
		n += v
	}
	return n
}

func (f *statefulApplyFixture) mutatingRoutes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.mutatingByRoute))
	for k := range f.mutatingByRoute {
		out = append(out, k)
	}
	return out
}

// handle dispatches one request. Reads/writes the in-memory model under
// mu and counts.
func (f *statefulApplyFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.totalRequests++
	f.byMethod[r.Method]++
	if r.Method != http.MethodGet {
		f.mutatingByRoute[r.Method+" "+r.URL.Path]++
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	// --- core lookups -------------------------------------------------
	switch r.URL.Path {
	case "/v1/apps":
		_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`)
		return
	case "/v1/apps/APP1/appStoreVersions":
		f.mu.Lock()
		body := jsonObj(map[string]any{
			"data": []any{
				map[string]any{
					"type":       "appStoreVersions",
					"id":         "VER1",
					"attributes": f.versionAttrs,
				},
			},
			"links": map[string]any{},
		})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	case "/v1/apps/APP1/appInfos":
		_, _ = io.WriteString(w, `{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`)
		return
	case "/v1/appInfos/AINFO1/ageRatingDeclaration":
		f.mu.Lock()
		body := jsonObj(map[string]any{
			"data": map[string]any{
				"type":       "ageRatingDeclarations",
				"id":         "AR1",
				"attributes": f.ageRatingAttrs,
			},
		})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	case "/v1/appStoreVersions/VER1/build":
		_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42","usesNonExemptEncryption":false}}}`)
		return
	case "/v1/builds/BUILD1":
		_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42"}}}`)
		return
	case "/v1/appStoreVersions/VER1/appStoreReviewDetail":
		_, _ = io.WriteString(w, `{"data":{"type":"appStoreReviewDetails","id":"RD1","attributes":{}}}`)
		return
	case "/v1/appStoreVersions/VER1/appStoreVersionLocalizations":
		f.mu.Lock()
		body := jsonObj(map[string]any{
			"data": []any{
				map[string]any{
					"type":       "appStoreVersionLocalizations",
					"id":         "VL1",
					"attributes": f.versionLocaleEnUSAttrs,
				},
			},
			"links": map[string]any{},
		})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	case "/v1/appInfos/AINFO1/appInfoLocalizations":
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return
	case "/v1/apps/APP1/appPriceSchedule":
		_, _ = io.WriteString(w, `{"data":null,"included":[]}`)
		return
	case "/v1/apps/APP1/inAppPurchasesV2":
		f.mu.Lock()
		out := []any{}
		for _, e := range f.iaps {
			out = append(out, map[string]any{
				"type":       "inAppPurchases",
				"id":         e.id,
				"attributes": e.attrs,
			})
		}
		body := jsonObj(map[string]any{"data": out, "links": map[string]any{}})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	case "/v1/apps/APP1/betaGroups":
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return
	case "/v1/apps/APP1/customProductPages":
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return
	}

	// --- per-IAP localization list ----------------------------------
	if strings.HasPrefix(r.URL.Path, "/v2/inAppPurchases/") &&
		strings.HasSuffix(r.URL.Path, "/inAppPurchaseLocalizations") {
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return
	}

	// --- per-version-localization screenshot sets -------------------
	if strings.HasPrefix(r.URL.Path, "/v1/appStoreVersionLocalizations/") &&
		strings.HasSuffix(r.URL.Path, "/appScreenshotSets") {
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return
	}

	// --- categories relationships ------------------------------------
	if strings.HasPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/") {
		rel := strings.TrimPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/")
		switch r.Method {
		case http.MethodGet:
			f.mu.Lock()
			id := ""
			switch rel {
			case "primaryCategory":
				id = f.primaryCategory
			case "secondaryCategory":
				id = f.secondaryCategory
			}
			f.mu.Unlock()
			if id == "" {
				_, _ = io.WriteString(w, `{"data":null}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":{"type":"appCategories","id":%q}}`, id)
			return
		case http.MethodPatch:
			id := extractRelationshipID(f.t, r)
			f.mu.Lock()
			switch rel {
			case "primaryCategory":
				f.primaryCategory = id
			case "secondaryCategory":
				f.secondaryCategory = id
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// --- version PATCH -----------------------------------------------
	if r.URL.Path == "/v1/appStoreVersions/VER1" && r.Method == http.MethodPatch {
		attrs := extractAttributes(f.t, r)
		f.mu.Lock()
		for k, v := range attrs {
			f.versionAttrs[k] = v
		}
		body := jsonObj(map[string]any{
			"data": map[string]any{
				"type":       "appStoreVersions",
				"id":         "VER1",
				"attributes": f.versionAttrs,
			},
		})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	}

	// --- ageRating PATCH ---------------------------------------------
	if r.URL.Path == "/v1/ageRatingDeclarations/AR1" && r.Method == http.MethodPatch {
		attrs := extractAttributes(f.t, r)
		f.mu.Lock()
		for k, v := range attrs {
			f.ageRatingAttrs[k] = v
		}
		body := jsonObj(map[string]any{
			"data": map[string]any{
				"type":       "ageRatingDeclarations",
				"id":         "AR1",
				"attributes": f.ageRatingAttrs,
			},
		})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	}

	// --- version-localization PATCH ----------------------------------
	if r.URL.Path == "/v1/appStoreVersionLocalizations/VL1" && r.Method == http.MethodPatch {
		attrs := extractAttributes(f.t, r)
		f.mu.Lock()
		for k, v := range attrs {
			f.versionLocaleEnUSAttrs[k] = v
		}
		body := jsonObj(map[string]any{
			"data": map[string]any{
				"type":       "appStoreVersionLocalizations",
				"id":         "VL1",
				"attributes": f.versionLocaleEnUSAttrs,
			},
		})
		f.mu.Unlock()
		_, _ = w.Write(body)
		return
	}

	// --- IAP create (POST /v2/inAppPurchases) ------------------------
	if r.URL.Path == "/v2/inAppPurchases" && r.Method == http.MethodPost {
		attrs := extractAttributes(f.t, r)
		f.mu.Lock()
		f.nextIAPID++
		id := fmt.Sprintf("%d", f.nextIAPID)
		f.iaps = append(f.iaps, iapEntry{id: id, attrs: attrs})
		body := jsonObj(map[string]any{
			"data": map[string]any{
				"type":       "inAppPurchases",
				"id":         id,
				"attributes": attrs,
			},
		})
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
		return
	}

	http.Error(w, "fixture has no route for "+r.Method+" "+r.URL.Path, http.StatusNotFound)
}

// jsonObj marshals m or t.Fatal — fixture-internal helper.
func jsonObj(m any) []byte {
	buf, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return buf
}

func extractAttributes(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("fixture: read body: %v", err)
	}
	var env struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf, &env); err != nil {
		t.Fatalf("fixture: parse PATCH body: %v\n%s", err, buf)
	}
	if env.Data.Attributes == nil {
		env.Data.Attributes = map[string]any{}
	}
	return env.Data.Attributes
}

func extractRelationshipID(t *testing.T, r *http.Request) string {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("fixture: read body: %v", err)
	}
	var env struct {
		Data struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(buf, &env)
	return env.Data.ID
}

// makeDesired constructs the desired state.yaml-equivalent State the
// idempotency test applies. Mirrors the canonical full-coverage fixture
// shape but with deliberately-different starting attrs in the fixture
// (so round 1 has work to do).
func makeDesired() *config.State {
	releaseType := "MANUAL"
	copyright := "© NEW COPYRIGHT"
	gambling := true
	cartoon := "INFREQUENT_OR_MILD"
	primary := "GAMES"
	descNew := "new description"
	return &config.State{
		APIVersion: "skipper.corelift.io/v1alpha1",
		Kind:       "AppState",
		Metadata: config.StateMetadata{
			BundleID: "com.example.app",
			Version:  "1.0",
			Platform: "IOS",
		},
		Spec: config.StateSpec{
			Version: &config.VersionSpec{
				Copyright:   &copyright,
				ReleaseType: &releaseType,
			},
			AgeRating: &config.AgeRatingSpec{
				CartoonOrFantasyViolence: &cartoon,
				Gambling:                 &gambling,
			},
			Categories: &config.CategoriesSpec{
				Primary: &primary,
			},
			Metadata: &config.MetadataSpec{
				Locales: map[string]config.MetadataLocale{
					"en-US": {Description: &descNew},
				},
			},
			IAP: &config.IAPSpec{
				Products: map[string]config.IAPProduct{
					"com.example.iap.lifetime": {
						Type: "NON_CONSUMABLE",
					},
				},
			},
		},
	}
}

// TestApply_Idempotent_FullSurfaceLoop is the keystone L2 contract test:
// apply a fresh state.yaml, refetch, rediff, reapply. The second apply
// MUST issue zero mutating calls and the diff must be empty.
func TestApply_Idempotent_FullSurfaceLoop(t *testing.T) {
	withTempCacheDir(t)
	f := newStatefulApplyFixture(t)
	c := fixtureClient(t, f.srv)
	ctx := context.Background()

	desired := makeDesired()

	// --- Round 1 -----------------------------------------------------
	live1, err := Fetch(ctx, c, "com.example.app", FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("round1 Fetch: %v", err)
	}
	changes1 := plan.Diff(desired, live1)
	if len(changes1) == 0 {
		t.Fatal("round1 Diff is empty — fixture is misconfigured (round 1 should have work)")
	}
	res1, err := Apply(ctx, c, changes1, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("round1 Apply: %v\nresult: %+v", err, res1)
	}
	if len(res1.Errors) != 0 {
		for _, e := range res1.Errors {
			t.Errorf("round1 unexpected error: %s", e.Error())
		}
		t.FailNow()
	}
	if len(res1.Applied) == 0 {
		t.Fatal("round1: Apply.Applied is empty; expected at least one change")
	}
	t.Logf("round1: applied %d change(s) across %d wire requests (%v)",
		len(res1.Applied), f.totalRequests, f.byMethod)
	round1Mutating := f.mutatingCount()
	if round1Mutating == 0 {
		t.Fatalf("round1: expected mutating calls, got 0 (routes hit: %v)", f.mutatingRoutes())
	}

	// --- Round 2 — refetch must show post-apply state ---------------
	live2, err := Fetch(ctx, c, "com.example.app", FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("round2 Fetch: %v", err)
	}
	changes2 := plan.Diff(desired, live2)
	if len(changes2) != 0 {
		for _, ch := range changes2 {
			t.Errorf("round2 diff still shows: %s %s: %v -> %v", ch.Op, ch.Path, ch.From, ch.To)
		}
		t.Fatal("round2: expected empty diff after round1 apply")
	}

	// --- Round 2 apply must be a no-op ------------------------------
	f.resetCounters()
	res2, err := Apply(ctx, c, changes2, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("round2 Apply: %v", err)
	}
	if len(res2.Applied) != 0 {
		t.Errorf("round2 Apply.Applied = %d, want 0", len(res2.Applied))
	}
	if got := f.mutatingCount(); got != 0 {
		t.Errorf("round2: idempotency violation — %d mutating call(s); routes hit: %v", got, f.mutatingRoutes())
	}
}

// TestApply_Idempotent_NoChangesEverProducesZeroMutations is a tighter
// guard: when desired == live from the start, both rounds issue zero
// mutating calls. Catches regressions where a dispatcher would PATCH
// even with no Change to dispatch.
func TestApply_Idempotent_NoChangesEverProducesZeroMutations(t *testing.T) {
	withTempCacheDir(t)
	f := newStatefulApplyFixture(t)
	c := fixtureClient(t, f.srv)
	ctx := context.Background()

	// Fetch once — capture exactly what the fixture serves as live.
	live, err := Fetch(ctx, c, "com.example.app", FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// Use that as the desired state. By construction, Diff(desired, live)
	// MUST be empty.
	changes := plan.Diff(live, live)
	if len(changes) != 0 {
		for _, ch := range changes {
			t.Errorf("Diff(live, live) returned: %+v", ch)
		}
		t.Fatal("Diff(live, live) is non-empty — diff engine is non-idempotent on identical inputs")
	}

	f.resetCounters()
	res, err := Apply(ctx, c, changes, ApplyOpts{
		Context: ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Applied) != 0 {
		t.Errorf("Applied = %d, want 0", len(res.Applied))
	}
	if got := f.mutatingCount(); got != 0 {
		t.Errorf("idempotency violation — %d mutating call(s); routes: %v", got, f.mutatingRoutes())
	}
}
