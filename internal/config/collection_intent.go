package config

import "encoding/json"

type categoriesWire struct {
	Primary                *string   `yaml:"primary,omitempty" json:"primary,omitempty"`
	Secondary              *string   `yaml:"secondary,omitempty" json:"secondary,omitempty"`
	PrimarySubcategories   *[]string `yaml:"primarySubcategories,omitempty" json:"primarySubcategories,omitempty"`
	SecondarySubcategories *[]string `yaml:"secondarySubcategories,omitempty" json:"secondarySubcategories,omitempty"`
}

func (s CategoriesSpec) wire() categoriesWire {
	return categoriesWire{
		Primary: s.Primary, Secondary: s.Secondary,
		PrimarySubcategories:   collectionPointer(s.PrimarySubcategories),
		SecondarySubcategories: collectionPointer(s.SecondarySubcategories),
	}
}

// MarshalYAML preserves explicit empty collections as managed intent.
func (s CategoriesSpec) MarshalYAML() (any, error) { return s.wire(), nil }

// MarshalJSON preserves explicit empty collections as managed intent.
func (s CategoriesSpec) MarshalJSON() ([]byte, error) { return json.Marshal(s.wire()) }

type testFlightGroupWire struct {
	IsInternal      *bool                `yaml:"isInternal,omitempty" json:"isInternal,omitempty"`
	PublicLink      *bool                `yaml:"publicLink,omitempty" json:"publicLink,omitempty"`
	PublicLinkLimit *int                 `yaml:"publicLinkLimit,omitempty" json:"publicLinkLimit,omitempty"`
	Testers         *[]TestFlightTester  `yaml:"testers,omitempty" json:"testers,omitempty"`
	Builds          *[]BetaBuildSelector `yaml:"builds,omitempty" json:"builds,omitempty"`
}

func (s TestFlightGroup) wire() testFlightGroupWire {
	return testFlightGroupWire{
		IsInternal: s.IsInternal, PublicLink: s.PublicLink,
		PublicLinkLimit: s.PublicLinkLimit, Testers: collectionPointer(s.Testers),
		Builds: s.Builds,
	}
}

// MarshalYAML preserves explicit empty tester rosters as managed intent.
func (s TestFlightGroup) MarshalYAML() (any, error) { return s.wire(), nil }

// MarshalJSON preserves explicit empty tester rosters as managed intent.
func (s TestFlightGroup) MarshalJSON() ([]byte, error) { return json.Marshal(s.wire()) }

func collectionPointer[T any](values []T) *[]T {
	if values == nil {
		return nil
	}
	return &values
}
