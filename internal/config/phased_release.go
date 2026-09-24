package config

// PhasedReleaseSpec separates explicit rollout intent from observed lifecycle fields.
type PhasedReleaseSpec struct {
	Enabled            *bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	State              *string `yaml:"state,omitempty" json:"state,omitempty"`
	StartDate          *string `yaml:"startDate,omitempty" json:"startDate,omitempty"`
	TotalPauseDuration *int    `yaml:"totalPauseDuration,omitempty" json:"totalPauseDuration,omitempty"`
	CurrentDayNumber   *int    `yaml:"currentDayNumber,omitempty" json:"currentDayNumber,omitempty"`
}
