package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF5C_BetaMetadataCommandsAndSecretSafeOutput(t *testing.T) {
	root := newBetaMetadataCommand()
	for _, name := range []string{"app-localizations", "build-localizations", "review-details"} {
		if found, _, err := root.Find([]string{name}); err != nil || found.Name() != name {
			t.Fatalf("missing command %q: found=%v err=%v", name, found, err)
		}
	}
	detail := BetaReviewDetailsResult{BundleID: "com.example.app", Detail: &asc.Resource[asc.BetaAppReviewDetailAttributes]{
		Type: "betaAppReviewDetails", ID: "R1", Attributes: asc.BetaAppReviewDetailAttributes{ContactEmail: "beta@example.com"},
	}}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"contactEmail":"beta@example.com"`) || strings.Contains(string(encoded), "Password") {
		t.Fatalf("review detail JSON=%s", encoded)
	}
}
