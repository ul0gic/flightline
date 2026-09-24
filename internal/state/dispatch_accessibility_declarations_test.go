package state

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestF4A_CreateDraftAndRefetchConverges(t *testing.T) {
	var created atomic.Bool
	posted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"APP1"}],"links":{}}`))
		case "/v1/apps/APP1/accessibilityDeclarations":
			if !created.Load() {
				_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsVoiceover":false}}],"links":{}}`))
		case "/v1/accessibilityDeclarations":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			posted <- body
			created.Store(true)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"type":"accessibilityDeclarations","id":"D1","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsVoiceover":false}}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	falseValue := false
	change := plan.Change{Op: plan.OpCreate, Path: "/spec/accessibilityDeclarations/families/IPHONE", To: config.AccessibilityDeclarationSpec{SupportsVoiceover: &falseValue}}
	client := fixtureClient(t, srv)
	if err := applyAccessibilityDeclaration(context.Background(), client, ApplyContext{BundleID: "com.example.app"}, change); err != nil {
		t.Fatal(err)
	}
	buf, _ := json.Marshal(<-posted)
	if len(buf) == 0 || !created.Load() {
		t.Fatalf("missing create request: %s", buf)
	}
	fetched, err := FetchAccessibilityDeclarations(context.Background(), client, "APP1")
	if err != nil || fetched.Families["IPHONE"].SupportsVoiceover == nil || *fetched.Families["IPHONE"].SupportsVoiceover {
		t.Fatalf("fetched=%+v err=%v", fetched, err)
	}
}

func TestF4A_ValidateAccessibilityChangeRejectsImplicitAttestationAndState(t *testing.T) {
	for _, change := range []plan.Change{
		{Op: plan.OpCreate, Path: "/spec/accessibilityDeclarations/families/IPHONE", To: config.AccessibilityDeclarationSpec{}},
		{Op: plan.OpCreate, Path: "/spec/accessibilityDeclarations/families/IPHONE", To: config.AccessibilityDeclarationSpec{State: f4aString("DRAFT")}},
		{Op: plan.OpDelete, Path: "/spec/accessibilityDeclarations/families/IPHONE", To: config.AccessibilityDeclarationSpec{SupportsVoiceover: f4aBool(false)}},
	} {
		if err := ValidateAccessibilityChange(change); err == nil {
			t.Fatalf("invalid change accepted: %+v", change)
		}
	}
}

func f4aString(value string) *string { return &value }
func f4aBool(value bool) *bool       { return &value }
