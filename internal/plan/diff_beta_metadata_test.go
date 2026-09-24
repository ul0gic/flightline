package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF5C_BetaMetadataDiffPreservesOmittedFieldsAndSeparatesReview(t *testing.T) {
	before := &config.BetaMetadataSpec{
		AppLocalizations: map[string]config.BetaAppLocalizationSpec{"en-US": {Description: betaString("Old"), FeedbackEmail: betaString("keep@example.com")}},
		ReviewDetails:    &config.BetaReviewDetailsSpec{ContactEmail: betaString("old@example.com")},
	}
	after := &config.BetaMetadataSpec{
		AppLocalizations: map[string]config.BetaAppLocalizationSpec{"en-US": {Description: betaString("New")}},
		ReviewDetails:    &config.BetaReviewDetailsSpec{ContactEmail: betaString("new@example.com")},
	}
	var changes []Change
	diffBetaMetadata(after, before, &changes)
	if len(changes) != 2 {
		t.Fatalf("changes=%+v", changes)
	}
	for _, change := range changes {
		if change.Path == "/spec/testflight/metadata/appLocalizations/en-US/feedbackEmail" || change.Path == "/spec/reviewerDemo" {
			t.Fatalf("unmanaged or App Store reviewer field changed: %+v", change)
		}
	}
}

func TestF5C_BetaMetadataDiffBuildLocalizationCreate(t *testing.T) {
	build := config.BetaBuildSelector{Number: "42", Version: "1.2", Platform: "IOS"}
	desired := &config.BetaMetadataSpec{Builds: []config.BetaBuildMetadataSpec{{
		Build: build, Localizations: map[string]config.BetaBuildLocalizationSpec{"en-US": {WhatsNew: betaString("Try sync")}},
	}}}
	var changes []Change
	diffBetaMetadata(desired, nil, &changes)
	if len(changes) != 1 || changes[0].Op != OpCreate || changes[0].Path != "/spec/testflight/metadata/builds/IOS:1.2:42/localizations/en-US" {
		t.Fatalf("changes=%+v", changes)
	}
}

func betaString(s string) *string { return &s }
