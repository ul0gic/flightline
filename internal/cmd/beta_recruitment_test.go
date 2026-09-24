package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF5C_RecruitmentCommandsRequireExplicitActions(t *testing.T) {
	root := newBetaRecruitmentCommand()
	for _, name := range []string{"policy", "notify", "criteria", "criteria-options"} {
		found, _, err := root.Find([]string{name})
		if err != nil || found.Name() != name {
			t.Fatalf("missing %s: found=%v err=%v", name, found, err)
		}
	}
	notify := newBetaNotifyCommand()
	notify.SetArgs([]string{"com.example.app", "--build", "42", "--version", "1.2", "--platform", "IOS"})
	if err := notify.Execute(); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("notify without confirm err=%v", err)
	}
	policy := newBetaPolicySetCommand()
	policy.SetArgs([]string{"com.example.app", "--build", "42", "--version", "1.2", "--platform", "IOS", "--enabled=true"})
	if err := policy.Execute(); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("enable without confirm err=%v", err)
	}
}

func TestF5C_RecruitmentBuildIdentityMismatchPreventsActions(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/builds":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"builds","id":"B1","attributes":{"version":"42"}}]}`)
		case "/v1/builds/B1/app":
			_, _ = fmt.Fprint(w, `{"data":{"type":"apps","id":"A1"}}`)
		case "/v1/builds/B1/preReleaseVersion":
			_, _ = fmt.Fprint(w, `{"data":{"type":"preReleaseVersions","id":"P1","attributes":{"version":"1.1","platform":"IOS"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cmd := newBetaPolicySetCommand()
	cmd.SetContext(context.Background())
	for name, value := range map[string]string{"build": "42", "version": "1.2", "platform": "IOS"} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	_, err := betaActionBuildID(cmd, fixtureASCClient(t, srv), "A1", "com.example.app")
	if err == nil || !strings.Contains(err.Error(), "identity does not match") || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestF5C_RecruitmentEmptyOptionsIsArray(t *testing.T) {
	empty := []asc.Resource[asc.BetaRecruitmentOptionAttributes]{}
	encoded, err := json.Marshal(BetaRecruitmentResult{Options: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"options":[]`) {
		t.Fatalf("options JSON=%s", encoded)
	}
}
