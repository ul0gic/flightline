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

func TestF5C_ValidateBetaDistributionChangePreservesExplicitEmpty(t *testing.T) {
	base := plan.Change{Op: plan.OpUpdate, Path: "/spec/testflight/groups/Beta~1QA/builds", To: []config.BetaBuildSelector{}}
	if err := ValidateBetaDistributionChange(base); err != nil {
		t.Fatalf("explicit empty: %v", err)
	}
	base.To = nil
	if err := ValidateBetaDistributionChange(base); err == nil {
		t.Fatal("nil intent accepted")
	}
	base.To = []config.BetaBuildSelector{{Number: "42", Version: "1.2", Platform: "IOS"}, {Number: "42", Version: "1.2", Platform: "IOS"}}
	if err := ValidateBetaDistributionChange(base); err == nil {
		t.Fatal("duplicate build accepted")
	}
}

func TestF5C_NotificationGuardRequiresKnownFalse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		wantError bool
	}{
		{"disabled", `{"data":{"type":"buildBetaDetails","id":"D1","attributes":{"autoNotifyEnabled":false}}}`, false},
		{"enabled", `{"data":{"type":"buildBetaDetails","id":"D1","attributes":{"autoNotifyEnabled":true}}}`, true},
		{"unknown", `{"data":{"type":"buildBetaDetails","id":"D1","attributes":{}}}`, true},
		{"wrong type despite false", `{"data":{"type":"builds","id":"D1","attributes":{"autoNotifyEnabled":false}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/builds/B1/buildBetaDetail" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			err := requireBetaNotificationDisabled(context.Background(), fixtureClient(t, srv), "B1")
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v wantError=%v", err, tc.wantError)
			}
			if err != nil && !strings.Contains(err.Error(), "auto-notify") {
				t.Fatalf("non-actionable error: %v", err)
			}
		})
	}
}
