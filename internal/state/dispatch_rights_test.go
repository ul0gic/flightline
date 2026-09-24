package state

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestF6B_ValidateRightsChangeRejectsUnsupportedAndIncomplete(t *testing.T) {
	empty := ""
	ids := []string{"USA"}
	for _, change := range []plan.Change{
		{Op: plan.OpDelete, Path: "/spec/contentRights", To: asc.ContentRightsUsesThirdParty},
		{Op: plan.OpUpdate, Path: "/spec/contentRights", To: "UNKNOWN"},
		{Op: plan.OpCreate, Path: "/spec/appEula", To: config.AppEULASpec{}},
		{Op: plan.OpUpdate, Path: "/spec/appEula", From: config.AppEULASpec{AgreementText: ptrRightsTest("Terms"), Territories: &ids}, To: config.AppEULASpec{AgreementText: &empty, Territories: &ids}},
		{Op: plan.OpUpdate, Path: "/spec/appEula/agreementText", To: "text"},
	} {
		if err := ValidateRightsChange(change); err == nil {
			t.Fatalf("accepted %+v", change)
		}
	}
}

func ptrRightsTest(value string) *string { return &value }

func TestF6B_ApplyContentRightsRejectsStalePriorWithoutPatch(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1":
			if r.Method == http.MethodPatch {
				patches++
			}
			_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1","attributes":{"contentRightsDeclaration":"DOES_NOT_USE_THIRD_PARTY_CONTENT"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	change := plan.Change{Op: plan.OpUpdate, Path: "/spec/contentRights", From: nil, To: asc.ContentRightsUsesThirdParty}
	err := applyRightsChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app"}, change)
	if err == nil || !strings.Contains(err.Error(), "changed since planning") || patches != 0 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}

func TestF6B_ApplyEULARejectsStalePriorWithoutPatch(t *testing.T) {
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1/endUserLicenseAgreement":
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"Third-party edit"}}}`)
		case "/v1/endUserLicenseAgreements/E1/territories":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"territories","id":"USA"}]}`)
		case "/v1/endUserLicenseAgreements/E1":
			patches++
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	prior := "Original"
	want := "Updated"
	ids := []string{"USA"}
	change := plan.Change{Op: plan.OpUpdate, Path: "/spec/appEula", From: config.AppEULASpec{AgreementText: &prior, Territories: &ids}, To: config.AppEULASpec{AgreementText: &want, Territories: &ids}}
	err := applyRightsChange(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app"}, change)
	if err == nil || !strings.Contains(err.Error(), "changed since planning") || patches != 0 {
		t.Fatalf("err=%v patches=%d", err, patches)
	}
}
