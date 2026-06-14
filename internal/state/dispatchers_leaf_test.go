package state

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/plan"
)

// applyOneChange runs Apply against handler for a single change with a
// temp checkpoint dir, returning any apply error.
func applyOneChange(t *testing.T, handler http.HandlerFunc, ch plan.Change) error {
	t.Helper()
	withTempCacheDir(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)
	_, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	})
	return err
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// --- applyBuildAttach -------------------------------------------------------

func TestDispatch_BuildAttach(t *testing.T) {
	var relPatches int32
	var body string
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case r.URL.Path == "/v1/builds" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[{"type":"builds","id":"BUILD9","attributes":{"version":"42"}}],"links":{}}`)
		case r.URL.Path == "/v1/appStoreVersions/VER1/relationships/build" && r.Method == http.MethodPatch:
			atomic.AddInt32(&relPatches, 1)
			body = readBody(t, r)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{Op: plan.OpUpdate, Resource: "build", Path: "/spec/build/number", To: "42"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if relPatches != 1 {
		t.Errorf("build relationship PATCHes = %d, want 1", relPatches)
	}
	if !strings.Contains(body, "BUILD9") || !strings.Contains(body, "builds") {
		t.Errorf("PATCH body should link resolved build BUILD9: %s", body)
	}
}

func TestDispatch_BuildAttach_NoBuildErrors(t *testing.T) {
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case "/v1/builds":
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		default:
			http.Error(w, "unhandled "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{Op: plan.OpUpdate, Resource: "build", Path: "/spec/build/number", To: "99"})
	if err == nil || !strings.Contains(err.Error(), "no build") {
		t.Fatalf("expected no-build error, got %v", err)
	}
}

// --- applyEncryptionFlag ----------------------------------------------------

func TestDispatch_EncryptionFlag(t *testing.T) {
	var patches int32
	var body string
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case r.URL.Path == "/v1/appStoreVersions/VER1/build":
			_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1","attributes":{"usesNonExemptEncryption":false}}}`)
		case r.URL.Path == "/v1/builds/BUILD1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			body = readBody(t, r)
			_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{Op: plan.OpUpdate, Resource: "exportCompliance", Path: "/spec/exportCompliance/usesNonExemptEncryption", To: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if patches != 1 {
		t.Errorf("build PATCHes = %d, want 1", patches)
	}
	if !strings.Contains(body, "usesNonExemptEncryption") || !strings.Contains(body, "true") {
		t.Errorf("PATCH body should set usesNonExemptEncryption=true: %s", body)
	}
}

// --- applyEncryptionDeclaration ---------------------------------------------

func TestDispatch_EncryptionDeclaration(t *testing.T) {
	var posts int32
	var body string
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/appEncryptionDeclarations" && r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
			body = readBody(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"appEncryptionDeclarations","id":"ED1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "exportCompliance",
		Path: "/spec/exportCompliance/declaration/usesEncryption", To: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if posts != 1 {
		t.Errorf("declaration POSTs = %d, want 1", posts)
	}
	if !strings.Contains(body, "usesEncryption") || !strings.Contains(body, `"apps"`) {
		t.Errorf("POST body should carry attr + app relationship: %s", body)
	}
}

func TestDispatch_EncryptionDeclaration_NestedPathErrors(t *testing.T) {
	err := applyOneChange(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "exportCompliance",
		Path: "/spec/exportCompliance/declaration/a/b", To: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected path") {
		t.Fatalf("expected unexpected-path error, got %v", err)
	}
}

// --- patchAppInfoSubcategories (via applyCategoriesField) --------------------

func TestDispatch_Subcategories(t *testing.T) {
	patchedRels := map[string]string{}
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = io.WriteString(w, `{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`)
		case strings.HasPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/") && r.Method == http.MethodPatch:
			rel := strings.TrimPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/")
			patchedRels[rel] = readBody(t, r)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "categories",
		Path: "/spec/categories/primarySubcategories", To: []any{"PUZZLE", "WORD"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(patchedRels) != 2 {
		t.Fatalf("expected 2 subcategory relationship PATCHes, got %d: %v", len(patchedRels), patchedRels)
	}
	if !strings.Contains(patchedRels["primarySubcategoryOne"], "PUZZLE") {
		t.Errorf("subcategoryOne should be PUZZLE: %s", patchedRels["primarySubcategoryOne"])
	}
	if !strings.Contains(patchedRels["primarySubcategoryTwo"], "WORD") {
		t.Errorf("subcategoryTwo should be WORD: %s", patchedRels["primarySubcategoryTwo"])
	}
}

func TestDispatch_Subcategories_ClearsSecondSlot(t *testing.T) {
	patchedRels := map[string]string{}
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = io.WriteString(w, `{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`)
		case strings.HasPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/") && r.Method == http.MethodPatch:
			rel := strings.TrimPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/")
			patchedRels[rel] = readBody(t, r)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "categories",
		Path: "/spec/categories/secondarySubcategories", To: []any{"PUZZLE"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(patchedRels["secondarySubcategoryTwo"], `"data":null`) {
		t.Errorf("empty second slot should clear with null data: %s", patchedRels["secondarySubcategoryTwo"])
	}
}

// --- applyPricingField ------------------------------------------------------

func TestDispatch_Pricing_CoalescesMissingField(t *testing.T) {
	var posts int32
	var body string
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appPriceSchedule":
			// Live schedule already has a price point; only territory changes.
			_, _ = io.WriteString(w, `{"data":{"type":"appPriceSchedules","id":"PS1"},"included":[{"type":"appPricePoints","id":"PP_LIVE"}]}`)
		case r.URL.Path == "/v1/appPriceSchedules" && r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
			body = readBody(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"appPriceSchedules","id":"PS2"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "pricing",
		Path: "/spec/pricing/baseTerritory", To: "USA",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if posts != 1 {
		t.Errorf("schedule POSTs = %d, want 1", posts)
	}
	if !strings.Contains(body, "USA") || !strings.Contains(body, "PP_LIVE") {
		t.Errorf("POST body should coalesce new territory + live price point: %s", body)
	}
}

func TestDispatch_Pricing_MissingBothErrors(t *testing.T) {
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case "/v1/apps/APP1/appPriceSchedule":
			_, _ = io.WriteString(w, `{"data":null,"included":[]}`)
		default:
			http.Error(w, "unhandled "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "pricing",
		Path: "/spec/pricing/baseTerritory", To: "USA",
	})
	if err == nil || !strings.Contains(err.Error(), "both baseTerritory") {
		t.Fatalf("expected both-required error, got %v", err)
	}
}

// --- getOrCreateAppInfoLocalization (via applyMetadataField name field) ------

func TestDispatch_AppInfoLocalization_CreatesWhenMissing(t *testing.T) {
	var creates, patches int32
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = io.WriteString(w, `{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`)
		case r.URL.Path == "/v1/appInfos/AINFO1/appInfoLocalizations" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		case r.URL.Path == "/v1/appInfoLocalizations" && r.Method == http.MethodPost:
			atomic.AddInt32(&creates, 1)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"appInfoLocalizations","id":"AL_NEW"}}`)
		case r.URL.Path == "/v1/appInfoLocalizations/AL_NEW" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			_, _ = io.WriteString(w, `{"data":{"type":"appInfoLocalizations","id":"AL_NEW"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "metadata.en-US",
		Path: "/spec/metadata/locales/en-US/name", To: "My App",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if creates != 1 {
		t.Errorf("appInfoLocalization POSTs = %d, want 1", creates)
	}
	if patches != 1 {
		t.Errorf("appInfoLocalization PATCHes = %d, want 1", patches)
	}
}

func TestDispatch_AppInfoLocalization_ReusesExisting(t *testing.T) {
	var creates, patches int32
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = io.WriteString(w, `{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`)
		case r.URL.Path == "/v1/appInfos/AINFO1/appInfoLocalizations" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[{"type":"appInfoLocalizations","id":"AL1","attributes":{"locale":"en-US"}}],"links":{}}`)
		case r.URL.Path == "/v1/appInfoLocalizations" && r.Method == http.MethodPost:
			atomic.AddInt32(&creates, 1)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"appInfoLocalizations","id":"X"}}`)
		case r.URL.Path == "/v1/appInfoLocalizations/AL1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			_, _ = io.WriteString(w, `{"data":{"type":"appInfoLocalizations","id":"AL1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "metadata.en-US",
		Path: "/spec/metadata/locales/en-US/name", To: "My App",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if creates != 0 {
		t.Errorf("should reuse existing localization, got %d POSTs", creates)
	}
	if patches != 1 {
		t.Errorf("PATCHes on existing AL1 = %d, want 1", patches)
	}
}

// --- getOrCreateIAPLocalization (via applyIAPField localization path) --------

func TestDispatch_IAPLocalization_CreatesWhenMissing(t *testing.T) {
	var creates, patches int32
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/inAppPurchasesV2":
			_, _ = io.WriteString(w, `{"data":[{"type":"inAppPurchases","id":"IAP1","attributes":{"productId":"com.x.lifetime"}}],"links":{}}`)
		case r.URL.Path == "/v2/inAppPurchases/IAP1/inAppPurchaseLocalizations" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		case r.URL.Path == "/v1/inAppPurchaseLocalizations" && r.Method == http.MethodPost:
			atomic.AddInt32(&creates, 1)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"inAppPurchaseLocalizations","id":"IL_NEW"}}`)
		case r.URL.Path == "/v1/inAppPurchaseLocalizations/IL_NEW" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			_, _ = io.WriteString(w, `{"data":{"type":"inAppPurchaseLocalizations","id":"IL_NEW"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "iap.com.x.lifetime",
		Path: "/spec/iap/products/com.x.lifetime/localizations/en-US/name", To: "Lifetime",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if creates != 1 {
		t.Errorf("IAP localization POSTs = %d, want 1", creates)
	}
	if patches != 1 {
		t.Errorf("IAP localization PATCHes = %d, want 1", patches)
	}
}

// --- applyScreenshotSet -----------------------------------------------------

func TestDispatch_ScreenshotSet_DefersToL1(t *testing.T) {
	err := applyOneChange(t, func(_ http.ResponseWriter, _ *http.Request) {
		// No route should be hit: the dispatcher returns before any HTTP.
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "screenshots.en-US.APP_IPHONE_69",
		Path: "/spec/screenshots/locales/en-US/APP_IPHONE_69", To: []any{"a.png"},
	})
	if err == nil || !strings.Contains(err.Error(), "QA-010") {
		t.Fatalf("expected QA-010 deferral error, got %v", err)
	}
}

// --- removeTester (via applyTestFlightField OpDelete) -----------------------

func TestDispatch_RemoveTester(t *testing.T) {
	var deletes int32
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = io.WriteString(w, `{"data":[{"type":"betaGroups","id":"BG1","attributes":{"name":"family"}}],"links":{}}`)
		case r.URL.Path == "/v1/betaTesters" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[{"type":"betaTesters","id":"BT9","attributes":{"email":"old@x.com"}}],"links":{}}`)
		case r.URL.Path == "/v1/betaGroups/BG1/relationships/betaTesters" && r.Method == http.MethodDelete:
			atomic.AddInt32(&deletes, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpDelete, Resource: "testflight.family.testers",
		Path: "/spec/testflight/groups/family/testers/old@x.com", To: "old@x.com",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if deletes != 1 {
		t.Errorf("relationship DELETEs = %d, want 1", deletes)
	}
}

func TestDispatch_RemoveTester_AbsentIsNoOp(t *testing.T) {
	var mutations int32
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			atomic.AddInt32(&mutations, 1)
		}
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = io.WriteString(w, `{"data":[{"type":"betaGroups","id":"BG1","attributes":{"name":"family"}}],"links":{}}`)
		case r.URL.Path == "/v1/betaTesters" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpDelete, Resource: "testflight.family.testers",
		Path: "/spec/testflight/groups/family/testers/ghost@x.com", To: "ghost@x.com",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if mutations != 0 {
		t.Errorf("removing an absent tester must issue no mutating call, got %d", mutations)
	}
}

// --- applyCustomProductPageField + resolveCustomProductPage -----------------

func TestDispatch_CustomProductPage_Create(t *testing.T) {
	var posts int32
	var body string
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/customProductPages" && r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
			body = readBody(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"customProductPages","id":"CPP1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpCreate, Resource: "customProductPages.summer-2026",
		Path: "/spec/customProductPages/summer-2026", To: map[string]any{"visible": true},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if posts != 1 {
		t.Errorf("page POSTs = %d, want 1", posts)
	}
	if !strings.Contains(body, "summer-2026") {
		t.Errorf("POST body should carry page name: %s", body)
	}
}

func TestDispatch_CustomProductPage_VisiblePatch(t *testing.T) {
	var patches int32
	var body string
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.URL.Path == "/v1/apps/APP1/customProductPages" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[{"type":"customProductPages","id":"CPP1","attributes":{"name":"summer-2026"}}],"links":{}}`)
		case r.URL.Path == "/v1/customProductPages/CPP1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			body = readBody(t, r)
			_, _ = io.WriteString(w, `{"data":{"type":"customProductPages","id":"CPP1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "customProductPages.summer-2026",
		Path: "/spec/customProductPages/summer-2026/visible", To: false,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if patches != 1 {
		t.Errorf("visible PATCHes = %d, want 1", patches)
	}
	if !strings.Contains(body, "visible") {
		t.Errorf("PATCH body should set visible: %s", body)
	}
}

func TestDispatch_CustomProductPage_NotFoundErrors(t *testing.T) {
	err := applyOneChange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case "/v1/apps/APP1/customProductPages":
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		default:
			http.Error(w, "unhandled "+r.URL.Path, http.StatusNotFound)
		}
	}, plan.Change{
		Op: plan.OpUpdate, Resource: "customProductPages.ghost",
		Path: "/spec/customProductPages/ghost/visible", To: true,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error from resolveCustomProductPage, got %v", err)
	}
}
