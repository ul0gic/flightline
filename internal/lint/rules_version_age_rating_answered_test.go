package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestVersionAgeRatingAnswered_OfflineMissingBlockFires(t *testing.T) {
	got := versionAgeRatingAnsweredRule{}.Check(CheckContext{
		Ctx:   context.Background(),
		State: &config.State{Spec: config.StateSpec{}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d diags, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "version.age-rating-answered" {
		t.Errorf("rule = %q", got[0].RuleID)
	}
}

func TestVersionAgeRatingAnswered_OfflinePartialFires(t *testing.T) {
	none := "NONE"
	f := false
	// Most fields nil; only a subset answered. Expect ~10 unanswered.
	got := versionAgeRatingAnsweredRule{}.Check(CheckContext{
		Ctx: context.Background(),
		State: &config.State{Spec: config.StateSpec{AgeRating: &config.AgeRatingSpec{
			CartoonOrFantasyViolence: &none,
			Gambling:                 &f,
		}}},
	})
	if len(got) == 0 {
		t.Fatal("got 0 diags, want >=1 for partial answers")
	}
	for _, d := range got {
		if d.RuleID != "version.age-rating-answered" || d.Severity != SeverityError {
			t.Errorf("unexpected diag: %+v", d)
		}
	}
}

func TestVersionAgeRatingAnswered_OfflineFullyAnsweredNoOp(t *testing.T) {
	none := "NONE"
	f := false
	// Set every field the rule scans for.
	full := &config.AgeRatingSpec{
		CartoonOrFantasyViolence:                  &none,
		RealisticViolence:                         &none,
		ProlongedGraphicSadisticRealisticViolence: &f,
		ProfanityOrCrudeHumor:                     &none,
		MatureSuggestiveThemes:                    &none,
		HorrorOrFearThemes:                        &none,
		MedicalOrTreatmentInformation:             &none,
		AlcoholTobaccoOrDrugUseOrReferences:       &none,
		ContestsAndGambling:                       &none,
		SexualContentOrNudity:                     &none,
		SexualContentGraphicAndNudity:             &none,
		Gambling:                                  &f,
		UnrestrictedWebAccess:                     &f,
		KidsAgeBand:                               &none,
		SeventeenPlus:                             &f,
	}
	got := versionAgeRatingAnsweredRule{}.Check(CheckContext{
		Ctx:   context.Background(),
		State: &config.State{Spec: config.StateSpec{AgeRating: full}},
	})
	if len(got) != 0 {
		t.Errorf("fully-answered returned %d diags, want 0: %+v", len(got), got)
	}
}

func TestVersionAgeRatingAnswered_LivePartialFires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"app-1","type":"apps","attributes":{"bundleId":"com.example.x"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/appInfos"):
			_, _ = w.Write([]byte(`{"data":[{"id":"info-1","type":"appInfos","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/ageRatingDeclaration"):
			_, _ = w.Write([]byte(`{"data":{"id":"ar-1","type":"ageRatingDeclarations","attributes":{"violenceCartoonOrFantasy":"NONE","gambling":false}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	got := versionAgeRatingAnsweredRule{}.Check(CheckContext{
		Ctx: context.Background(), Client: c, BundleID: "com.example.x", Live: true,
	})
	// Only 2 fields answered; expect at least 8 missing.
	if len(got) < 5 {
		t.Errorf("got %d diags, want many missing: %+v", len(got), got)
	}
}
