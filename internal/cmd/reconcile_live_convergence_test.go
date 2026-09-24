//go:build integration

package cmd

import (
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestC3B_ActualPopulatedRefetchControlsConvergence(t *testing.T) {
	copyright := "Original [flightline live check]"
	releaseType := "MANUAL"
	live := &config.State{
		APIVersion: "flightline.dev/v1alpha1", Kind: "AppState",
		Metadata: config.StateMetadata{BundleID: "com.example.sacrificial", Version: "1.2.3", Platform: "IOS"},
		Spec: config.StateSpec{
			Version:  &config.VersionSpec{Copyright: &copyright, ReleaseType: &releaseType},
			Metadata: &config.MetadataSpec{Locales: map[string]config.MetadataLocale{"en-US": {Name: stringPtrC3B("Example")}}},
		},
	}
	if err := c3bAssertEmptyPlan(copyright, live); err != nil {
		t.Fatalf("unmanaged populated refetch should converge: %v", err)
	}
	if err := c3bAssertEmptyPlan("Different", live); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("different actual refetch copyright escaped plan: %v", err)
	}
}

func stringPtrC3B(value string) *string { return &value }
