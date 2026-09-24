package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF4A_FetchAccessibilitySelectsDraftPreservesFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"type":"accessibilityDeclarations","id":"old","attributes":{"deviceFamily":"IPHONE","state":"REPLACED","supportsVoiceover":true}},
			{"type":"accessibilityDeclarations","id":"published","attributes":{"deviceFamily":"IPHONE","state":"PUBLISHED","supportsVoiceover":true}},
			{"type":"accessibilityDeclarations","id":"draft","attributes":{"deviceFamily":"IPHONE","state":"DRAFT","supportsVoiceover":false}},
			{"type":"accessibilityDeclarations","id":"ipad","attributes":{"deviceFamily":"IPAD","state":"PUBLISHED","supportsCaptions":false}}
		],"links":{}}`))
	}))
	defer srv.Close()
	got, err := FetchAccessibilityDeclarations(context.Background(), fixtureClient(t, srv), "APP1")
	if err != nil {
		t.Fatal(err)
	}
	phone := got.Families["IPHONE"]
	pad := got.Families["IPAD"]
	if phone.State == nil || *phone.State != "DRAFT" || phone.SupportsVoiceover == nil || *phone.SupportsVoiceover || pad.SupportsCaptions == nil || *pad.SupportsCaptions {
		t.Fatalf("families=%+v", got.Families)
	}
}

func TestF4A_FetchAccessibilityRejectsDuplicateDraft(t *testing.T) {
	declarations := []asc.AccessibilityDeclaration{
		{ID: "D1", AccessibilityDeclarationAttributes: asc.AccessibilityDeclarationAttributes{DeviceFamily: "IPHONE", State: "DRAFT"}},
		{ID: "D2", AccessibilityDeclarationAttributes: asc.AccessibilityDeclarationAttributes{DeviceFamily: "IPHONE", State: "DRAFT"}},
	}
	if _, err := selectAccessibilityDeclarations(declarations); err == nil {
		t.Fatal("duplicate active declarations accepted")
	}
}
