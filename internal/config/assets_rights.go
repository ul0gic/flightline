package config

// PreviewsSpec manages preview files by locale and Apple's preview type.
type PreviewsSpec struct {
	Locales map[string]map[string][]PreviewFile `yaml:"locales,omitempty" json:"locales,omitempty"`
}

// PreviewFile uses its source checksum for observed/local identity; frame intent is optional.
type PreviewFile struct {
	Path                 string  `yaml:"path" json:"path"`
	PreviewFrameTimeCode *string `yaml:"previewFrameTimeCode,omitempty" json:"previewFrameTimeCode,omitempty"`
	SourceFileChecksum   string  `yaml:"sourceFileChecksum,omitempty" json:"sourceFileChecksum,omitempty"`
}

// AppEULASpec preserves omitted text or territory settings on an existing custom agreement.
type AppEULASpec struct {
	AgreementText *string   `yaml:"agreementText,omitempty" json:"agreementText,omitempty"`
	Territories   *[]string `yaml:"territories,omitempty" json:"territories,omitempty"`
}
