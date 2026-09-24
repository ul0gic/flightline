package state

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestC2A_IAPParentThenLocalizationCreate(t *testing.T) {
	name := "Lifetime"
	var parentBody, localizationBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}]}`)
		case "/v2/inAppPurchases":
			parentBody = readBody(t, r)
			_, _ = io.WriteString(w, `{"data":{"type":"inAppPurchases","id":"IAP1"}}`)
		case "/v1/apps/APP1/inAppPurchasesV2":
			_, _ = io.WriteString(w, `{"data":[{"type":"inAppPurchases","id":"IAP1","attributes":{"productId":"com.example.lifetime"}}]}`)
		case "/v2/inAppPurchases/IAP1/inAppPurchaseLocalizations":
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		case "/v1/inAppPurchaseLocalizations":
			localizationBody = readBody(t, r)
			_, _ = io.WriteString(w, `{"data":{"type":"inAppPurchaseLocalizations","id":"LOC1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	parent := plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/com.example.lifetime", To: config.IAPProduct{Type: "NON_CONSUMABLE", Name: &name}}
	child := plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/com.example.lifetime/localizations/en-US", To: config.IAPLocalization{Name: &name}}
	if err := applyIAPField(context.Background(), c, ctxApply(), parent); err != nil {
		t.Fatalf("parent create: %v", err)
	}
	if err := applyIAPField(context.Background(), c, ctxApply(), child); err != nil {
		t.Fatalf("localization create: %v", err)
	}
	if !strings.Contains(parentBody, `"inAppPurchaseType":"NON_CONSUMABLE"`) || strings.Contains(parentBody, "contentHosting") {
		t.Fatalf("unexpected parent body: %s", parentBody)
	}
	if !strings.Contains(localizationBody, `"name":"Lifetime"`) || !strings.Contains(localizationBody, `"locale":"en-US"`) {
		t.Fatalf("incomplete localization create: %s", localizationBody)
	}
}

func TestC2A_LocalizationRetryDoesNotRepeatParent(t *testing.T) {
	name := "Lifetime"
	parentCreates, localizationCreates := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}]}`)
		case "/v2/inAppPurchases":
			parentCreates++
			_, _ = io.WriteString(w, `{"data":{"type":"inAppPurchases","id":"IAP1"}}`)
		case "/v1/apps/APP1/inAppPurchasesV2":
			_, _ = io.WriteString(w, `{"data":[{"type":"inAppPurchases","id":"IAP1","attributes":{"productId":"com.example.lifetime"}}]}`)
		case "/v2/inAppPurchases/IAP1/inAppPurchaseLocalizations":
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
		case "/v1/inAppPurchaseLocalizations":
			localizationCreates++
			if localizationCreates == 1 {
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"type":"inAppPurchaseLocalizations","id":"LOC1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	parent := plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/com.example.lifetime", To: config.IAPProduct{Type: "NON_CONSUMABLE", Name: &name}}
	child := plan.Change{Op: plan.OpCreate, Path: "/spec/iap/products/com.example.lifetime/localizations/en-US", To: config.IAPLocalization{Name: &name}}
	if err := applyIAPField(context.Background(), c, ctxApply(), parent); err != nil {
		t.Fatalf("parent create: %v", err)
	}
	if err := applyIAPField(context.Background(), c, ctxApply(), child); err == nil {
		t.Fatal("first localization create unexpectedly succeeded")
	}
	if err := applyIAPField(context.Background(), c, ctxApply(), child); err != nil {
		t.Fatalf("retry localization create: %v", err)
	}
	if parentCreates != 1 {
		t.Fatalf("parent creates = %d, want 1", parentCreates)
	}
	if localizationCreates != 2 {
		t.Fatalf("localization creates = %d, want 2", localizationCreates)
	}
}

func TestC2A_TestFlightParentHonorsKind(t *testing.T) {
	internal := true
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}]}`)
		case "/v1/betaGroups":
			body = readBody(t, r)
			_, _ = io.WriteString(w, `{"data":{"type":"betaGroups","id":"GROUP1"}}`)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	err := applyTestFlightField(context.Background(), fixtureClient(t, srv), ctxApply(), plan.Change{
		Op: plan.OpCreate, Path: "/spec/testflight/groups/internal", To: config.TestFlightGroup{IsInternal: &internal},
	})
	if err != nil {
		t.Fatalf("group create: %v", err)
	}
	if !strings.Contains(body, `"isInternalGroup":true`) {
		t.Fatalf("group kind missing from create body: %s", body)
	}
}

func TestC2A_TestFlightEscapedGroupAndTesterPathsPreserveValues(t *testing.T) {
	internal := true
	groupName := "release/qa~canary"
	email := "tester/one~two@example.com"
	var createdGroup, lookedUpGroup, lookedUpTester string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}]}`)
		case "/v1/betaGroups":
			if r.Method == http.MethodPost {
				createdGroup = readBody(t, r)
				_, _ = io.WriteString(w, `{"data":{"type":"betaGroups","id":"GROUP1"}}`)
				return
			}
			http.Error(w, "unexpected beta group method", http.StatusMethodNotAllowed)
		case "/v1/apps/APP1/betaGroups":
			lookedUpGroup = r.URL.Query().Get("filter[name]")
			_, _ = io.WriteString(w, `{"data":[{"type":"betaGroups","id":"GROUP1"}]}`)
		case "/v1/betaTesters":
			lookedUpTester = r.URL.Query().Get("filter[email]")
			_, _ = io.WriteString(w, `{"data":[{"type":"betaTesters","id":"TESTER1"}]}`)
		case "/v1/betaGroups/GROUP1/relationships/betaTesters":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	parent := plan.Change{Op: plan.OpCreate, Path: "/spec/testflight/groups/release~1qa~0canary", To: config.TestFlightGroup{IsInternal: &internal}}
	tester := plan.Change{Op: plan.OpCreate, Path: "/spec/testflight/groups/release~1qa~0canary/testers/tester~1one~0two@example.com", To: email}
	if err := applyTestFlightField(context.Background(), c, ctxApply(), parent); err != nil {
		t.Fatalf("parent create: %v", err)
	}
	if err := applyTestFlightField(context.Background(), c, ctxApply(), tester); err != nil {
		t.Fatalf("tester create: %v", err)
	}
	if !strings.Contains(createdGroup, `"name":"release/qa~canary"`) {
		t.Fatalf("group create did not preserve name: %s", createdGroup)
	}
	if lookedUpGroup != groupName {
		t.Fatalf("group lookup = %q, want %q", lookedUpGroup, groupName)
	}
	if lookedUpTester != email {
		t.Fatalf("tester lookup = %q, want %q", lookedUpTester, email)
	}
}

func TestC2A_UnsupportedIAPWriteDoesNotCallAPI(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "must not call", http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := applyIAPField(context.Background(), fixtureClient(t, srv), ctxApply(), plan.Change{
		Op: plan.OpUpdate, Path: "/spec/iap/products/com.example.lifetime/type", To: "CONSUMABLE",
	})
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable type error = %v", err)
	}
	if called {
		t.Fatal("unsupported IAP write issued an API request")
	}
}
