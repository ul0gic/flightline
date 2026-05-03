// dispatchers_test.go — per-surface apply dispatcher tests.
//
// One subtest per surface that: stages a Change, runs Apply against
// an httptest server, asserts the expected method+path was hit. The
// fixture handler asserts on body shape only when correctness depends
// on it (PATCH attribute name, relationship type, etc.).

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

func ctxApply() ApplyContext {
	return ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}
}

// runOneChange wires Apply against an httptest handler and a single
// change. Returns the request count + any error so the test can
// assert "exactly one PATCH was issued at expected path".
func runOneChange(t *testing.T, handler http.Handler, ch plan.Change) (int32, error) {
	t.Helper()
	withTempCacheDir(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := fixtureClient(t, srv)
	_, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	})
	return 0, err
}

// TestDispatch_Categories — one categories Change → one PATCH on the
// appInfo's primaryCategory relationship.
func TestDispatch_Categories(t *testing.T) {
	var patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1","attributes":{}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = w.Write([]byte(`{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`))
		case r.URL.Path == "/v1/appInfos/AINFO1/relationships/primaryCategory" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "EDUCATION") {
				t.Errorf("PATCH body missing EDUCATION: %s", body)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	withTempCacheDir(t)
	c := fixtureClient(t, srv)
	ch := plan.Change{Op: plan.OpUpdate, Resource: "categories", Path: "/spec/categories/primary", To: "EDUCATION"}
	if _, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if atomic.LoadInt32(&patches) != 1 {
		t.Errorf("PATCHes = %d, want 1", patches)
	}
}

// TestDispatch_Metadata — version-localization field PATCHes the right
// resource (description lives on appStoreVersionLocalizations).
func TestDispatch_Metadata(t *testing.T) {
	var versionPatches, appInfoPatches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1/appStoreVersionLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersionLocalizations","id":"VL1","attributes":{"locale":"en-US"}}],"links":{}}`))
		case r.URL.Path == "/v1/appStoreVersionLocalizations/VL1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&versionPatches, 1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "description") || !strings.Contains(string(body), "new desc") {
				t.Errorf("PATCH body wrong: %s", body)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreVersionLocalizations","id":"VL1"}}`))
		case r.URL.Path == "/v1/appInfoLocalizations/AL1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&appInfoPatches, 1)
			_, _ = w.Write([]byte(`{"data":{"type":"appInfoLocalizations","id":"AL1"}}`))
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	withTempCacheDir(t)
	c := fixtureClient(t, srv)
	ch := plan.Change{Op: plan.OpUpdate, Resource: "metadata.en-US", Path: "/spec/metadata/locales/en-US/description", To: "new desc"}
	if _, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if atomic.LoadInt32(&versionPatches) != 1 {
		t.Errorf("version-localization PATCHes = %d, want 1", versionPatches)
	}
	if atomic.LoadInt32(&appInfoPatches) != 0 {
		t.Errorf("appInfo-localization PATCHes = %d, want 0 (description doesn't live there)", appInfoPatches)
	}
}

// TestDispatch_TestFlightTesterAdd — OpCreate on testers/<email>
// should look up or create the tester then POST to the group's
// betaTesters relationship.
func TestDispatch_TestFlightTesterAdd(t *testing.T) {
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"type":"betaGroups","id":"BG1","attributes":{"name":"family"}}],"links":{}}`))
		case r.URL.Path == "/v1/betaTesters" && r.Method == http.MethodGet:
			// Tester not found → empty list, will be created.
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		case r.URL.Path == "/v1/betaTesters" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"data":{"type":"betaTesters","id":"BT1","attributes":{"email":"new@x.com"}}}`))
		case r.URL.Path == "/v1/betaGroups/BG1/relationships/betaTesters" && r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	withTempCacheDir(t)
	c := fixtureClient(t, srv)
	ch := plan.Change{
		Op: plan.OpCreate, Resource: "testflight.family.testers",
		Path: "/spec/testflight/groups/family/testers/new@x.com", To: "new@x.com",
	}
	if _, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if atomic.LoadInt32(&posts) != 1 {
		t.Errorf("relationship POSTs = %d, want 1", posts)
	}
}

// TestDispatch_ReviewerDemoUsername — username PATCH lands on the
// appStoreReviewDetail's demoAccountName attribute (schema → wire
// rename).
func TestDispatch_ReviewerDemoUsername(t *testing.T) {
	var patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1/appStoreReviewDetail":
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreReviewDetails","id":"RD1","attributes":{}}}`))
		case r.URL.Path == "/v1/appStoreReviewDetails/RD1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "demoAccountName") {
				t.Errorf("PATCH body should set demoAccountName: %s", body)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreReviewDetails","id":"RD1"}}`))
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	withTempCacheDir(t)
	c := fixtureClient(t, srv)
	ch := plan.Change{Op: plan.OpUpdate, Resource: "reviewerDemo", Path: "/spec/reviewerDemo/username", To: "demo@x.com"}
	if _, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if atomic.LoadInt32(&patches) != 1 {
		t.Errorf("PATCHes = %d, want 1", patches)
	}
}

// TestDispatch_PasswordRefResolvesEnv — passwordRef "env:VAR" must
// look up the env var and PATCH demoAccountPassword with the value.
func TestDispatch_PasswordRefResolvesEnv(t *testing.T) {
	var bodyText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1/appStoreReviewDetail":
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreReviewDetails","id":"RD1","attributes":{}}}`))
		case r.URL.Path == "/v1/appStoreReviewDetails/RD1" && r.Method == http.MethodPatch:
			b, _ := io.ReadAll(r.Body)
			bodyText = string(b)
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreReviewDetails","id":"RD1"}}`))
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	withTempCacheDir(t)
	t.Setenv("DEMO_PASSWORD_TEST", "s3cr3t")
	c := fixtureClient(t, srv)
	ch := plan.Change{Op: plan.OpUpdate, Resource: "reviewerDemo", Path: "/spec/reviewerDemo/passwordRef", To: "env:DEMO_PASSWORD_TEST"}
	if _, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(bodyText, `"demoAccountPassword":"s3cr3t"`) {
		t.Errorf("PATCH body should contain resolved password under demoAccountPassword: %s", bodyText)
	}
	if strings.Contains(bodyText, "env:") {
		t.Errorf("PATCH body must not contain the env: ref string: %s", bodyText)
	}
}

// TestDispatch_PasswordRefMissingEnvErrors — env var unset → typed error.
func TestDispatch_PasswordRefMissingEnvErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	withTempCacheDir(t)
	c := fixtureClient(t, srv)
	ch := plan.Change{Op: plan.OpUpdate, Resource: "reviewerDemo", Path: "/spec/reviewerDemo/passwordRef", To: "env:NEVER_SET_PASSWORD_VAR"}
	_, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	})
	if err == nil {
		t.Fatal("expected error for unset env")
	}
	if !strings.Contains(err.Error(), "NEVER_SET_PASSWORD_VAR") {
		t.Errorf("error should name the env var: %v", err)
	}
}

// TestDispatch_IAPField — name update on an existing IAP PATCHes
// /v2/inAppPurchases/{id}.
func TestDispatch_IAPField(t *testing.T) {
	var patches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1"}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/inAppPurchasesV2":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchases","id":"IAP1","attributes":{"productId":"com.x.lifetime","name":"old"}}],"links":{}}`))
		case r.URL.Path == "/v2/inAppPurchases/IAP1" && r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "Lifetime Access") {
				t.Errorf("PATCH body missing new name: %s", body)
			}
			_, _ = w.Write([]byte(`{"data":{"type":"inAppPurchases","id":"IAP1"}}`))
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	withTempCacheDir(t)
	c := fixtureClient(t, srv)
	ch := plan.Change{
		Op: plan.OpUpdate, Resource: "iap.com.x.lifetime",
		Path: "/spec/iap/products/com.x.lifetime/name", To: "Lifetime Access",
	}
	if _, err := Apply(context.Background(), c, []plan.Change{ch}, ApplyOpts{
		Context: ctxApply(), Confirm: true,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if atomic.LoadInt32(&patches) != 1 {
		t.Errorf("PATCHes = %d, want 1", patches)
	}
}

// silence the unused-helper lint while we keep runOneChange around
// as a drop-in for future surface tests.
var _ = runOneChange
