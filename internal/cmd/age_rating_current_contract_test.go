package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestDBT059_GRACClassificationNumberPayloadAndDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "age-rating.json")
	if err := os.WriteFile(path, []byte(`{"gracRatingClassificationNumber":"GRAC-2026-001"}`), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	payload, err := loadAgeRatingPayload(path)
	if err != nil {
		t.Fatalf("loadAgeRatingPayload: %v", err)
	}
	desired, err := payload.toAttributes()
	if err != nil {
		t.Fatalf("toAttributes: %v", err)
	}
	if desired.GracRatingClassificationNumber != "GRAC-2026-001" {
		t.Fatalf("GRAC number = %q", desired.GracRatingClassificationNumber)
	}
	changes := diffAgeRating(asc.AgeRatingDeclarationAttributes{}, desired, payload.providedKeys())
	if changes["gracRatingClassificationNumber"] != "GRAC-2026-001" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDBT059_AgeRatingOverrideEnums(t *testing.T) {
	cases := []struct {
		name       string
		valid      []string
		invalid    string
		attributes func(string) asc.AgeRatingDeclarationAttributes
	}{
		{
			name:    "legacy override",
			valid:   []string{"NONE", "NINE_PLUS", "THIRTEEN_PLUS", "SIXTEEN_PLUS", "SEVENTEEN_PLUS", "UNRATED"},
			invalid: "EIGHTEEN_PLUS",
			attributes: func(value string) asc.AgeRatingDeclarationAttributes {
				return asc.AgeRatingDeclarationAttributes{AgeRatingOverride: value}
			},
		},
		{
			name:    "override v2",
			valid:   []string{"NONE", "NINE_PLUS", "THIRTEEN_PLUS", "SIXTEEN_PLUS", "EIGHTEEN_PLUS", "UNRATED"},
			invalid: "SEVENTEEN_PLUS",
			attributes: func(value string) asc.AgeRatingDeclarationAttributes {
				return asc.AgeRatingDeclarationAttributes{AgeRatingOverrideV2: value}
			},
		},
		{
			name:    "Korea override",
			valid:   []string{"NONE", "ALL", "TWELVE_PLUS", "FIFTEEN_PLUS", "NINETEEN_PLUS"},
			invalid: "EIGHTEEN_PLUS",
			attributes: func(value string) asc.AgeRatingDeclarationAttributes {
				return asc.AgeRatingDeclarationAttributes{KoreaAgeRatingOverride: value}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, value := range tc.valid {
				if err := validateAgeRatingAttributes(tc.attributes(value)); err != nil {
					t.Errorf("%q: unexpected validation error: %v", value, err)
				}
			}
			if err := validateAgeRatingAttributes(tc.attributes(tc.invalid)); err == nil {
				t.Errorf("%q: expected validation error", tc.invalid)
			}
		})
	}
}
