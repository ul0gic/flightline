package plan

import (
	"encoding/json"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/ul0gic/flightline/internal/config"
)

func TestC1B_CollectionIntentRoundTripControlsDiff(t *testing.T) {
	trueValue := true
	falseValue := false
	live := &config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{
			Primary:              stringC1B("GAMES"),
			PrimarySubcategories: []string{"ADVENTURE"},
		},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"external": {
				PublicLink: &falseValue,
				Testers: []config.TestFlightTester{
					{Email: "one@example.com"},
				},
			},
		}},
	}}
	desired := config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{Primary: stringC1B("BOOKS")},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"external": {PublicLink: &trueValue},
		}},
	}}

	for _, format := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{"json", json.Marshal, json.Unmarshal},
		{"yaml", yaml.Marshal, yaml.Unmarshal},
	} {
		t.Run(format.name, func(t *testing.T) {
			encoded, err := format.marshal(desired)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded config.State
			if err := format.unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			changes := Diff(&decoded, live)
			assertC1B_Paths(t, changes,
				"/spec/categories/primary",
				"/spec/testflight/groups/external/publicLink",
			)
		})
	}
}

func TestC1B_ExplicitEmptyCollectionsClear(t *testing.T) {
	live := &config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{
			PrimarySubcategories:   []string{"ADVENTURE"},
			SecondarySubcategories: []string{"FOOD"},
		},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"external": {Testers: []config.TestFlightTester{{Email: "one@example.com"}, {Email: "two@example.com"}}},
		}},
	}}
	desired := &config.State{Spec: config.StateSpec{
		Categories: &config.CategoriesSpec{PrimarySubcategories: []string{}},
		TestFlight: &config.TestFlightSpec{Groups: map[string]config.TestFlightGroup{
			"external": {Testers: []config.TestFlightTester{}},
		}},
	}}

	changes := Diff(desired, live)
	assertC1B_Paths(t, changes,
		"/spec/categories/primarySubcategories",
		"/spec/testflight/groups/external/testers/one@example.com",
		"/spec/testflight/groups/external/testers/two@example.com",
	)
	for _, change := range changes {
		if change.Path == "/spec/categories/primarySubcategories" {
			values, ok := change.To.([]string)
			if !ok || values == nil || len(values) != 0 {
				t.Fatalf("explicit empty categories intent lost: %#v", change.To)
			}
		}
	}
}

func assertC1B_Paths(t *testing.T, changes []Change, want ...string) {
	t.Helper()
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.Path)
	}
	if len(got) != len(want) {
		t.Fatalf("change paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("change paths = %v, want %v", got, want)
		}
	}
}

func stringC1B(value string) *string { return &value }
