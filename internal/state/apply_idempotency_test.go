package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// statefulApplyFixture is a stateful httptest server; PATCHes/POSTs are reflected into
// its in-memory model so the second fetch sees updated state. mu guards all model access.
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

func (f *statefulApplyFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.totalRequests++
	f.byMethod[r.Method]++
	if r.Method != http.MethodGet {
		f.mutatingByRoute[r.Method+" "+r.URL.Path]++
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if f.serveLookup(w, r) || f.serveMutation(w, r) {
		return
	}
	http.Error(w, "fixture has no route for "+r.Method+" "+r.URL.Path, http.StatusNotFound)
}

func (f *statefulApplyFixture) serveLookup(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/v1/apps":
		_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`)
	case "/v1/apps/APP1/appStoreVersions":
		f.writeLocked(w, map[string]any{
			"data":  []any{map[string]any{"type": "appStoreVersions", "id": "VER1", "attributes": f.versionAttrs}},
			"links": map[string]any{},
		})
	case "/v1/apps/APP1/appInfos":
		_, _ = io.WriteString(w, `{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`)
	case "/v1/appInfos/AINFO1/ageRatingDeclaration":
		f.writeLocked(w, map[string]any{
			"data": map[string]any{"type": "ageRatingDeclarations", "id": "AR1", "attributes": f.ageRatingAttrs},
		})
	case "/v1/appStoreVersions/VER1/build":
		_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42","usesNonExemptEncryption":false}}}`)
	case "/v1/builds/BUILD1":
		_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42"}}}`)
	case "/v1/appStoreVersions/VER1/appStoreReviewDetail":
		_, _ = io.WriteString(w, `{"data":{"type":"appStoreReviewDetails","id":"RD1","attributes":{}}}`)
	case "/v1/appStoreVersions/VER1/appStoreVersionLocalizations":
		f.writeLocked(w, map[string]any{
			"data":  []any{map[string]any{"type": "appStoreVersionLocalizations", "id": "VL1", "attributes": f.versionLocaleEnUSAttrs}},
			"links": map[string]any{},
		})
	case "/v1/appInfos/AINFO1/appInfoLocalizations":
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
	case "/v1/apps/APP1/appPriceSchedule":
		_, _ = io.WriteString(w, `{"data":null,"included":[]}`)
	case "/v1/apps/APP1/inAppPurchasesV2":
		f.mu.Lock()
		out := make([]any, 0, len(f.iaps))
		for _, e := range f.iaps {
			out = append(out, map[string]any{"type": "inAppPurchases", "id": e.id, "attributes": e.attrs})
		}
		body := jsonObj(map[string]any{"data": out, "links": map[string]any{}})
		f.mu.Unlock()
		_, _ = w.Write(body)
	case "/v1/apps/APP1/betaGroups":
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
	case "/v1/apps/APP1/customProductPages":
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
	default:
		return f.serveLookupPrefix(w, r)
	}
	return true
}

func (f *statefulApplyFixture) serveLookupPrefix(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case strings.HasPrefix(r.URL.Path, "/v2/inAppPurchases/") && strings.HasSuffix(r.URL.Path, "/inAppPurchaseLocalizations"):
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return true
	case strings.HasPrefix(r.URL.Path, "/v1/appStoreVersionLocalizations/") && strings.HasSuffix(r.URL.Path, "/appScreenshotSets"):
		_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		return true
	case strings.HasPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/") && r.Method == http.MethodGet:
		f.serveCategoryGet(w, r)
		return true
	}
	return false
}

func (f *statefulApplyFixture) serveCategoryGet(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/")
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
}

func (f *statefulApplyFixture) serveMutation(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/") && r.Method == http.MethodPatch:
		rel := strings.TrimPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/")
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
		return true
	case r.URL.Path == "/v1/appStoreVersions/VER1" && r.Method == http.MethodPatch:
		f.mergeAttrs(w, r, f.versionAttrs, "appStoreVersions", "VER1")
		return true
	case r.URL.Path == "/v1/ageRatingDeclarations/AR1" && r.Method == http.MethodPatch:
		f.mergeAttrs(w, r, f.ageRatingAttrs, "ageRatingDeclarations", "AR1")
		return true
	case r.URL.Path == "/v1/appStoreVersionLocalizations/VL1" && r.Method == http.MethodPatch:
		f.mergeAttrs(w, r, f.versionLocaleEnUSAttrs, "appStoreVersionLocalizations", "VL1")
		return true
	case r.URL.Path == "/v2/inAppPurchases" && r.Method == http.MethodPost:
		attrs := extractAttributes(f.t, r)
		f.mu.Lock()
		f.nextIAPID++
		id := strconv.Itoa(f.nextIAPID)
		f.iaps = append(f.iaps, iapEntry{id: id, attrs: attrs})
		body := jsonObj(map[string]any{
			"data": map[string]any{"type": "inAppPurchases", "id": id, "attributes": attrs},
		})
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
		return true
	}
	return false
}

func (f *statefulApplyFixture) writeLocked(w http.ResponseWriter, body map[string]any) {
	f.mu.Lock()
	buf := jsonObj(body)
	f.mu.Unlock()
	_, _ = w.Write(buf)
}

func (f *statefulApplyFixture) mergeAttrs(w http.ResponseWriter, r *http.Request, model map[string]any, resType, id string) {
	attrs := extractAttributes(f.t, r)
	f.mu.Lock()
	for k, v := range attrs {
		model[k] = v
	}
	body := jsonObj(map[string]any{
		"data": map[string]any{"type": resType, "id": id, "attributes": model},
	})
	f.mu.Unlock()
	_, _ = w.Write(body)
}

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

// makeDesired builds a desired State with attrs that differ from the fixture's initial state,
// so round 1 has work to do.
func makeDesired() *config.State {
	releaseType := "MANUAL"
	copyright := "© NEW COPYRIGHT"
	gambling := true
	cartoon := "INFREQUENT_OR_MILD"
	primary := "GAMES"
	descNew := "new description"
	return &config.State{
		APIVersion: "flightline.dev/v1alpha1",
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

// TestApply_Idempotent_FullSurfaceLoop is the keystone L2 contract: apply → refetch → rediff → reapply.
// The second apply MUST issue zero mutating calls and the diff must be empty.
func TestApply_Idempotent_FullSurfaceLoop(t *testing.T) {
	withTempCacheDir(t)
	f := newStatefulApplyFixture(t)
	c := fixtureClient(t, f.srv)
	ctx := context.Background()

	desired := makeDesired()

	live1, err := Fetch(ctx, c, "com.example.app", FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("round1 Fetch: %v", err)
	}
	changes1 := plan.Diff(desired, live1)
	if len(changes1) == 0 {
		t.Fatal("round1 Diff is empty: fixture is misconfigured (round 1 should have work)")
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
		t.Errorf("round2: idempotency violation: %d mutating call(s); routes hit: %v", got, f.mutatingRoutes())
	}
}

// TestApply_Idempotent_NoChangesEverProducesZeroMutations guards against dispatchers
// issuing PATCHes when Diff(live, live) is empty.
func TestApply_Idempotent_NoChangesEverProducesZeroMutations(t *testing.T) {
	withTempCacheDir(t)
	f := newStatefulApplyFixture(t)
	c := fixtureClient(t, f.srv)
	ctx := context.Background()

	live, err := Fetch(ctx, c, "com.example.app", FetchOpts{Version: "1.0", Platform: "IOS"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	changes := plan.Diff(live, live) // Diff(live, live) must be empty by construction
	if len(changes) != 0 {
		for _, ch := range changes {
			t.Errorf("Diff(live, live) returned: %+v", ch)
		}
		t.Fatal("Diff(live, live) is non-empty: diff engine is non-idempotent on identical inputs")
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
		t.Errorf("idempotency violation: %d mutating call(s); routes: %v", got, f.mutatingRoutes())
	}
}
