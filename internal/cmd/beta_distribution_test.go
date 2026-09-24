package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5C_DistributionCommandTreeAndNotificationOverride(t *testing.T) {
	root := newBetaDistributionCommand()
	for _, name := range []string{"list", "add", "remove"} {
		found, _, err := root.Find([]string{name})
		if err != nil || found.Name() != name {
			t.Fatalf("missing %s: %v %v", name, found, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":{"type":"buildBetaDetails","id":"D1","attributes":{"autoNotifyEnabled":true}}}`)
	}))
	defer srv.Close()
	c := fixtureASCClient(t, srv)
	for _, tc := range []struct {
		allow     bool
		confirm   bool
		wantError bool
	}{{false, false, true}, {true, false, true}, {false, true, true}, {true, true, false}} {
		err := betaDistributionNotificationGuard(context.Background(), c, "B1", tc.allow, tc.confirm)
		if (err != nil) != tc.wantError {
			t.Fatalf("allow=%v confirm=%v err=%v", tc.allow, tc.confirm, err)
		}
		if err != nil && !strings.Contains(err.Error(), "auto-notify") {
			t.Fatalf("error lacks action context: %v", err)
		}
	}
}
