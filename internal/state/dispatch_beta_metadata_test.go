package state

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestF5C_ValidateBetaMetadataChangeRejectsSecretsAndUnknownFields(t *testing.T) {
	cases := []struct {
		name   string
		change plan.Change
		valid  bool
	}{
		{"app create", plan.Change{Op: plan.OpCreate, Path: "/spec/testflight/metadata/appLocalizations/en-US", To: config.BetaAppLocalizationSpec{}}, true},
		{"app field", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/appLocalizations/en-US/description", To: "Beta"}, true},
		{"build notes", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/builds/IOS:1.2:42/localizations/en-US/whatsNew", To: "Test sync"}, true},
		{"review password", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/reviewDetails/demoAccountPassword", To: "secret"}, false},
		{"unknown app field", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/appLocalizations/en-US/unknown", To: "x"}, false},
		{"delete", plan.Change{Op: plan.OpDelete, Path: "/spec/testflight/metadata/appLocalizations/en-US", To: nil}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBetaMetadataChange(tc.change)
			if (err == nil) != tc.valid {
				t.Fatalf("err=%v valid=%v", err, tc.valid)
			}
		})
	}
}

func TestF5C_BetaAppMetadataStaleAndCreateConflictNeverPatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change plan.Change
		want   string
	}{
		{"stale field", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/appLocalizations/en-US/description", From: "Old", To: "New"}, "changed since planning"},
		{"create conflict", plan.Change{Op: plan.OpCreate, Path: "/spec/testflight/metadata/appLocalizations/en-US", To: config.BetaAppLocalizationSpec{Description: betaStateString("New")}}, "already exists"},
		{"already converged", plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/metadata/appLocalizations/en-US/description", From: "Old", To: "Concurrent"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"data":[{"type":"betaAppLocalizations","id":"L1","attributes":{"locale":"en-US","description":"Concurrent"}}]}`)
			}))
			defer srv.Close()
			err := applyBetaAppLocalizationChange(context.Background(), fixtureClient(t, srv), "A1", "en-US"+strings.TrimPrefix(tc.change.Path, "/spec/testflight/metadata/appLocalizations/en-US"), tc.change)
			if (err == nil) != (tc.want == "") || err != nil && !strings.Contains(err.Error(), tc.want) || writes != 0 {
				t.Fatalf("err=%v writes=%d", err, writes)
			}
		})
	}
}

func betaStateString(value string) *string { return &value }
