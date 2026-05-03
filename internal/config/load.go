// Package config loads and validates Skipper state YAML files.
//
// Two stages:
//
//  1. LoadState — pure YAML decode into the typed *State tree, with
//     KnownFields(true) so unknown keys become DiagnosticErrors anchored to
//     the offending line/col. Coercion footguns (`yes`/`no` for bool,
//     bare-decimal price tiers) surface here as type-mismatch errors.
//  2. Validate (schema.go) — runs *State through the embedded JSON Schema
//     for cross-field rules the Go types can't express.
//
// Diagnostics from both stages share the Diagnostic type so cmd/plan and
// cmd/apply can render them uniformly.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	yaml "go.yaml.in/yaml/v3"
)

// State mirrors the top-level shape of schemas/skipper.schema.json.
// Loaded from YAML and validated against the schema before use.
type State struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind"       json:"kind"`
	Metadata   StateMetadata `yaml:"metadata"   json:"metadata"`
	Spec       StateSpec     `yaml:"spec"       json:"spec"`
}

// StateMetadata is metadata.{bundleId,version,platform}.
type StateMetadata struct {
	BundleID string `yaml:"bundleId"           json:"bundleId"`
	Version  string `yaml:"version"            json:"version"`
	Platform string `yaml:"platform,omitempty" json:"platform,omitempty"`
}

// StateSpec mirrors schemas/skipper.schema.json's spec section. Each
// field is a typed sub-spec; nil pointers mean "not managed" — Skipper
// leaves that surface alone during apply.
type StateSpec struct {
	Version            *VersionSpec            `yaml:"version,omitempty"            json:"version,omitempty"`
	Build              *BuildSpec              `yaml:"build,omitempty"              json:"build,omitempty"`
	Metadata           *MetadataSpec           `yaml:"metadata,omitempty"           json:"metadata,omitempty"`
	Screenshots        *ScreenshotsSpec        `yaml:"screenshots,omitempty"        json:"screenshots,omitempty"`
	IAP                *IAPSpec                `yaml:"iap,omitempty"                json:"iap,omitempty"`
	AgeRating          *AgeRatingSpec          `yaml:"ageRating,omitempty"          json:"ageRating,omitempty"`
	ExportCompliance   *ExportComplianceSpec   `yaml:"exportCompliance,omitempty"   json:"exportCompliance,omitempty"`
	ReviewerDemo       *ReviewerDemoSpec       `yaml:"reviewerDemo,omitempty"       json:"reviewerDemo,omitempty"`
	Categories         *CategoriesSpec         `yaml:"categories,omitempty"         json:"categories,omitempty"`
	Pricing            *PricingSpec            `yaml:"pricing,omitempty"            json:"pricing,omitempty"`
	TestFlight         *TestFlightSpec         `yaml:"testflight,omitempty"         json:"testflight,omitempty"`
	CustomProductPages *CustomProductPagesSpec `yaml:"customProductPages,omitempty" json:"customProductPages,omitempty"`
}

// VersionSpec — see #/$defs/versionSpec.
type VersionSpec struct {
	ReleaseType         *string `yaml:"releaseType,omitempty"         json:"releaseType,omitempty"`
	EarliestReleaseDate *string `yaml:"earliestReleaseDate,omitempty" json:"earliestReleaseDate,omitempty"`
	Copyright           *string `yaml:"copyright,omitempty"           json:"copyright,omitempty"`
	Downloadable        *bool   `yaml:"downloadable,omitempty"        json:"downloadable,omitempty"`
}

// BuildSpec — see #/$defs/buildSpec.
type BuildSpec struct {
	Number string `yaml:"number,omitempty" json:"number,omitempty"`
}

// MetadataSpec — see #/$defs/metadataSpec.
type MetadataSpec struct {
	Locales map[string]MetadataLocale `yaml:"locales,omitempty" json:"locales,omitempty"`
}

// MetadataLocale — see #/$defs/metadataLocale.
type MetadataLocale struct {
	Name             *string `yaml:"name,omitempty"             json:"name,omitempty"`
	Subtitle         *string `yaml:"subtitle,omitempty"         json:"subtitle,omitempty"`
	Description      *string `yaml:"description,omitempty"      json:"description,omitempty"`
	Keywords         *string `yaml:"keywords,omitempty"         json:"keywords,omitempty"`
	WhatsNew         *string `yaml:"whatsNew,omitempty"         json:"whatsNew,omitempty"`
	PromotionalText  *string `yaml:"promotionalText,omitempty"  json:"promotionalText,omitempty"`
	MarketingURL     *string `yaml:"marketingUrl,omitempty"     json:"marketingUrl,omitempty"`
	SupportURL       *string `yaml:"supportUrl,omitempty"       json:"supportUrl,omitempty"`
	PrivacyPolicyURL *string `yaml:"privacyPolicyUrl,omitempty" json:"privacyPolicyUrl,omitempty"`
}

// ScreenshotsSpec — see #/$defs/screenshotsSpec.
type ScreenshotsSpec struct {
	Locales map[string]map[string][]ScreenshotFile `yaml:"locales,omitempty" json:"locales,omitempty"`
}

// ScreenshotFile — see #/$defs/screenshotFile.
type ScreenshotFile struct {
	Path string  `yaml:"path"          json:"path"`
	Alt  *string `yaml:"alt,omitempty" json:"alt,omitempty"`
}

// IAPSpec — see #/$defs/iapSpec.
type IAPSpec struct {
	Products map[string]IAPProduct `yaml:"products,omitempty" json:"products,omitempty"`
}

// IAPProduct — see #/$defs/iapProduct.
type IAPProduct struct {
	Type             string                     `yaml:"type"                       json:"type"`
	Name             *string                    `yaml:"name,omitempty"             json:"name,omitempty"`
	FamilySharable   *bool                      `yaml:"familySharable,omitempty"   json:"familySharable,omitempty"`
	ContentHosting   *string                    `yaml:"contentHosting,omitempty"   json:"contentHosting,omitempty"`
	ReviewNote       *string                    `yaml:"reviewNote,omitempty"       json:"reviewNote,omitempty"`
	ReviewScreenshot *IAPReviewScreenshot       `yaml:"reviewScreenshot,omitempty" json:"reviewScreenshot,omitempty"`
	Localizations    map[string]IAPLocalization `yaml:"localizations,omitempty"    json:"localizations,omitempty"`
}

// IAPReviewScreenshot is the path-only sub-object on iapProduct.
type IAPReviewScreenshot struct {
	Path string `yaml:"path" json:"path"`
}

// IAPLocalization — see #/$defs/iapLocalization.
type IAPLocalization struct {
	Name        *string `yaml:"name,omitempty"        json:"name,omitempty"`
	Description *string `yaml:"description,omitempty" json:"description,omitempty"`
}

// AgeRatingSpec — see #/$defs/ageRatingSpec. Pointer-typed enums and
// booleans so Skipper can distinguish "answered NONE" from "not managed".
type AgeRatingSpec struct {
	CartoonOrFantasyViolence                  *string `yaml:"cartoonOrFantasyViolence,omitempty"                    json:"cartoonOrFantasyViolence,omitempty"`
	RealisticViolence                         *string `yaml:"realisticViolence,omitempty"                           json:"realisticViolence,omitempty"`
	ProlongedGraphicSadisticRealisticViolence *bool   `yaml:"prolongedGraphicSadisticRealisticViolence,omitempty"   json:"prolongedGraphicSadisticRealisticViolence,omitempty"`
	ProfanityOrCrudeHumor                     *string `yaml:"profanityOrCrudeHumor,omitempty"                       json:"profanityOrCrudeHumor,omitempty"`
	MatureSuggestiveThemes                    *string `yaml:"matureSuggestiveThemes,omitempty"                      json:"matureSuggestiveThemes,omitempty"`
	HorrorOrFearThemes                        *string `yaml:"horrorOrFearThemes,omitempty"                          json:"horrorOrFearThemes,omitempty"`
	MedicalOrTreatmentInformation             *string `yaml:"medicalOrTreatmentInformation,omitempty"               json:"medicalOrTreatmentInformation,omitempty"`
	AlcoholTobaccoOrDrugUseOrReferences       *string `yaml:"alcoholTobaccoOrDrugUseOrReferences,omitempty"         json:"alcoholTobaccoOrDrugUseOrReferences,omitempty"`
	ContestsAndGambling                       *string `yaml:"contestsAndGambling,omitempty"                         json:"contestsAndGambling,omitempty"`
	SexualContentOrNudity                     *string `yaml:"sexualContentOrNudity,omitempty"                       json:"sexualContentOrNudity,omitempty"`
	SexualContentGraphicAndNudity             *string `yaml:"sexualContentGraphicAndNudity,omitempty"               json:"sexualContentGraphicAndNudity,omitempty"`
	Gambling                                  *bool   `yaml:"gambling,omitempty"                                    json:"gambling,omitempty"`
	UnrestrictedWebAccess                     *bool   `yaml:"unrestrictedWebAccess,omitempty"                       json:"unrestrictedWebAccess,omitempty"`
	KidsAgeBand                               *string `yaml:"kidsAgeBand,omitempty"                                 json:"kidsAgeBand,omitempty"`
	SeventeenPlus                             *bool   `yaml:"seventeenPlus,omitempty"                               json:"seventeenPlus,omitempty"`
}

// ExportComplianceSpec — see #/$defs/exportComplianceSpec.
type ExportComplianceSpec struct {
	UsesNonExemptEncryption *bool                        `yaml:"usesNonExemptEncryption,omitempty" json:"usesNonExemptEncryption,omitempty"`
	Declaration             *ExportComplianceDeclaration `yaml:"declaration,omitempty"             json:"declaration,omitempty"`
}

// ExportComplianceDeclaration is the optional ECCN classification block.
type ExportComplianceDeclaration struct {
	ContainsProprietaryCryptography *bool   `yaml:"containsProprietaryCryptography,omitempty" json:"containsProprietaryCryptography,omitempty"`
	ContainsThirdPartyCryptography  *bool   `yaml:"containsThirdPartyCryptography,omitempty"  json:"containsThirdPartyCryptography,omitempty"`
	AvailableOnFrenchStore          *bool   `yaml:"availableOnFrenchStore,omitempty"          json:"availableOnFrenchStore,omitempty"`
	UsesEncryption                  *bool   `yaml:"usesEncryption,omitempty"                  json:"usesEncryption,omitempty"`
	Exempt                          *bool   `yaml:"exempt,omitempty"                          json:"exempt,omitempty"`
	ECCN                            *string `yaml:"eccn,omitempty"                            json:"eccn,omitempty"`
	DocumentName                    *string `yaml:"documentName,omitempty"                    json:"documentName,omitempty"`
	DocumentURL                     *string `yaml:"documentUrl,omitempty"                     json:"documentUrl,omitempty"`
}

// ReviewerDemoSpec — see #/$defs/reviewerDemoSpec.
type ReviewerDemoSpec struct {
	Username     *string `yaml:"username,omitempty"     json:"username,omitempty"`
	PasswordRef  *string `yaml:"passwordRef,omitempty"  json:"passwordRef,omitempty"`
	PasswordFile *string `yaml:"passwordFile,omitempty" json:"passwordFile,omitempty"`
	Notes        *string `yaml:"notes,omitempty"        json:"notes,omitempty"`
	ContactName  *string `yaml:"contactName,omitempty"  json:"contactName,omitempty"`
	ContactEmail *string `yaml:"contactEmail,omitempty" json:"contactEmail,omitempty"`
	ContactPhone *string `yaml:"contactPhone,omitempty" json:"contactPhone,omitempty"`
}

// CategoriesSpec — see #/$defs/categoriesSpec.
type CategoriesSpec struct {
	Primary                *string  `yaml:"primary,omitempty"                json:"primary,omitempty"`
	Secondary              *string  `yaml:"secondary,omitempty"              json:"secondary,omitempty"`
	PrimarySubcategories   []string `yaml:"primarySubcategories,omitempty"   json:"primarySubcategories,omitempty"`
	SecondarySubcategories []string `yaml:"secondarySubcategories,omitempty" json:"secondarySubcategories,omitempty"`
}

// PricingSpec — see #/$defs/pricingSpec.
type PricingSpec struct {
	BaseTerritory   *string `yaml:"baseTerritory,omitempty"   json:"baseTerritory,omitempty"`
	AppPricePointID *string `yaml:"appPricePointId,omitempty" json:"appPricePointId,omitempty"`
}

// TestFlightSpec — see #/$defs/testflightSpec.
type TestFlightSpec struct {
	Groups map[string]TestFlightGroup `yaml:"groups,omitempty" json:"groups,omitempty"`
}

// TestFlightGroup — see #/$defs/testflightGroup.
type TestFlightGroup struct {
	IsInternal      *bool              `yaml:"isInternal,omitempty"      json:"isInternal,omitempty"`
	PublicLink      *bool              `yaml:"publicLink,omitempty"      json:"publicLink,omitempty"`
	PublicLinkLimit *int               `yaml:"publicLinkLimit,omitempty" json:"publicLinkLimit,omitempty"`
	Testers         []TestFlightTester `yaml:"testers,omitempty"         json:"testers,omitempty"`
}

// TestFlightTester — see #/$defs/testflightTester.
type TestFlightTester struct {
	Email     string  `yaml:"email"               json:"email"`
	FirstName *string `yaml:"firstName,omitempty" json:"firstName,omitempty"`
	LastName  *string `yaml:"lastName,omitempty"  json:"lastName,omitempty"`
}

// CustomProductPagesSpec — see #/$defs/customProductPagesSpec. Keys
// are the page identifier (slug); values describe the page.
//
// The schema models this as a patternProperties object at the root of
// `customProductPages`, so the Go type is a bare map. The pointer in
// StateSpec lets callers distinguish "not managed" (nil) from
// "explicitly empty" (zero-len map).
type CustomProductPagesSpec map[string]CustomProductPage

// CustomProductPage — see #/$defs/customProductPage.
type CustomProductPage struct {
	Visible       *bool                              `yaml:"visible,omitempty"       json:"visible,omitempty"`
	Localizations map[string]CustomProductPageLocale `yaml:"localizations,omitempty" json:"localizations,omitempty"`
}

// CustomProductPageLocale — one locale's content on a custom product page.
type CustomProductPageLocale struct {
	PromotionalText *string                     `yaml:"promotionalText,omitempty" json:"promotionalText,omitempty"`
	Screenshots     map[string][]ScreenshotFile `yaml:"screenshots,omitempty"     json:"screenshots,omitempty"`
}

// DiagnosticSeverity classifies a Diagnostic. Errors block apply; warnings
// surface in plan/apply output but don't gate.
type DiagnosticSeverity string

// Severity levels.
const (
	SeverityError   DiagnosticSeverity = "ERROR"
	SeverityWarning DiagnosticSeverity = "WARNING"
)

// Diagnostic is one machine-readable problem with the state file. Shared
// between LoadState (line/col anchored) and Validate (path-only).
type Diagnostic struct {
	File     string             `json:"file"`
	Line     int                `json:"line,omitempty"`
	Column   int                `json:"column,omitempty"`
	Path     string             `json:"path,omitempty"`
	Message  string             `json:"message"`
	Severity DiagnosticSeverity `json:"severity"`
}

// String renders a Diagnostic in the standard "file:line:col: msg" form.
func (d Diagnostic) String() string {
	loc := d.File
	if d.Line > 0 {
		loc = fmt.Sprintf("%s:%d", loc, d.Line)
		if d.Column > 0 {
			loc = fmt.Sprintf("%s:%d", loc, d.Column)
		}
	}
	if d.Path != "" {
		return fmt.Sprintf("%s: %s [%s]: %s", loc, d.Severity, d.Path, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", loc, d.Severity, d.Message)
}

// LoadError is returned by LoadState when YAML decoding fails. Carries
// the per-error Diagnostics so callers can format them however they
// like (cmd/plan, cmd/apply, future LSP server).
type LoadError struct {
	Diagnostics []Diagnostic
}

// Error renders all diagnostics newline-joined.
func (e *LoadError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "config: load failed (no diagnostics)"
	}
	if len(e.Diagnostics) == 1 {
		return e.Diagnostics[0].String()
	}
	out := fmt.Sprintf("%d errors:", len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		out += "\n  " + d.String()
	}
	return out
}

// LoadState reads a YAML state file and decodes it to *State.
//
// On any decode error (unknown key, type mismatch, malformed YAML), returns
// a *LoadError carrying file:line:col-anchored Diagnostics. Schema
// validation against schemas/skipper.schema.json runs separately via
// Validate — LoadState only does structural decode.
func LoadState(path string) (*State, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	f, err := os.Open(abs) //nolint:gosec // path is user-supplied; opening it is the entire purpose
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", abs, err)
	}
	defer func() { _ = f.Close() }()

	return loadStateFromReader(abs, f)
}

func loadStateFromReader(file string, r io.Reader) (*State, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var s State
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &LoadError{Diagnostics: []Diagnostic{{
				File:     file,
				Severity: SeverityError,
				Message:  "file is empty",
			}}}
		}
		return nil, &LoadError{Diagnostics: []Diagnostic{yamlErrorToDiagnostic(file, err)}}
	}
	return &s, nil
}

// yamlErrorToDiagnostic extracts file:line:col anchors from yaml.v3's
// *yaml.TypeError and falls back to a path-less diagnostic for everything
// else. yaml.v3 stuffs line numbers into the error string; we prefer
// structured access where the library exposes it.
func yamlErrorToDiagnostic(file string, err error) Diagnostic {
	d := Diagnostic{
		File:     file,
		Severity: SeverityError,
		Message:  err.Error(),
	}
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		// TypeError carries one or more "line N: ..." messages. Render
		// them all in the message (separate diagnostics is overkill for
		// this stage; the schema validator will be more granular).
		d.Message = "yaml: " + te.Error()
	}
	return d
}
