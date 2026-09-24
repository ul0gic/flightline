package config

import (
	"encoding/json"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func TestC1B_CollectionIntentMarshalRoundTrip(t *testing.T) {
	for _, format := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{"json", json.Marshal, json.Unmarshal},
		{"yaml", yaml.Marshal, yaml.Unmarshal},
	} {
		t.Run(format.name, func(t *testing.T) {
			state := State{Spec: StateSpec{
				Categories: &CategoriesSpec{PrimarySubcategories: []string{}},
				TestFlight: &TestFlightSpec{Groups: map[string]TestFlightGroup{
					"empty":    {Testers: []TestFlightTester{}, Builds: &[]BetaBuildSelector{}},
					"omit":     {},
					"assigned": {Builds: &[]BetaBuildSelector{{Number: "42", Version: "1.0", Platform: "IOS"}}},
				}},
			}}
			encoded, err := format.marshal(state)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded State
			if err := format.unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.Spec.Categories.PrimarySubcategories == nil {
				t.Fatal("explicit empty primarySubcategories decoded as unmanaged")
			}
			if decoded.Spec.TestFlight.Groups["empty"].Testers == nil {
				t.Fatal("explicit empty testers decoded as unmanaged")
			}
			if decoded.Spec.TestFlight.Groups["omit"].Testers != nil {
				t.Fatal("omitted testers decoded as managed empty collection")
			}
			assertG5BuildCollectionIntent(t, decoded)
		})
	}
}

func assertG5BuildCollectionIntent(t *testing.T, decoded State) {
	t.Helper()
	if builds := decoded.Spec.TestFlight.Groups["empty"].Builds; builds == nil || len(*builds) != 0 {
		t.Fatal("explicit empty builds did not round-trip")
	}
	if decoded.Spec.TestFlight.Groups["omit"].Builds != nil {
		t.Fatal("omitted builds decoded as managed collection")
	}
	if builds := decoded.Spec.TestFlight.Groups["assigned"].Builds; builds == nil || len(*builds) != 1 || (*builds)[0].Number != "42" {
		t.Fatal("assigned builds lost during serialization")
	}
}
