package lint

import (
	"fmt"
	"strings"
)

// iapLocalizationNameLanguageRule flags IAP display names that are language names — a locale-picker paste bug.
// A reviewer seeing "English" as a product name reports the IAP itself as unlocatable (PassDMV 2.1(b), 2026-04).
type iapLocalizationNameLanguageRule struct{}

func init() { Register(iapLocalizationNameLanguageRule{}) }

func (iapLocalizationNameLanguageRule) ID() string         { return "iap.localization-name-is-language-name" }
func (iapLocalizationNameLanguageRule) Severity() Severity { return SeverityWarning }
func (iapLocalizationNameLanguageRule) Mode() Mode         { return ModeOffline }
func (iapLocalizationNameLanguageRule) Doc() string {
	return "Warns when an in-app purchase localization's display name is a language name like \"English\" or \"Español\" — the classic paste bug where the locale label lands in the product-name field. " +
		"Customers see this name on the purchase sheet, and Apple's reviewer will describe the IAP by it, turning the mistake into a confusing rejection. " +
		"Fix it by naming what the customer buys (\"Lifetime Access\", \"Acceso de por Vida\"), never the language it is written in."
}

// languageNames covers the App Store's common locale labels in English and native forms.
var languageNames = map[string]struct{}{
	"english": {}, "spanish": {}, "español": {}, "espanol": {}, "french": {}, "français": {}, "francais": {},
	"german": {}, "deutsch": {}, "italian": {}, "italiano": {}, "portuguese": {}, "português": {},
	"portugues": {}, //nolint:misspell // accent-stripped native form users actually type, not a typo
	"japanese":  {}, "日本語": {}, "chinese": {}, "中文": {}, "korean": {}, "한국어": {}, "arabic": {}, "العربية": {},
	"hindi": {}, "russian": {}, "русский": {}, "dutch": {}, "nederlands": {}, "swedish": {}, "svenska": {},
	"turkish": {}, "türkçe": {}, "vietnamese": {}, "thai": {}, "polish": {}, "polski": {}, "indonesian": {},
	"greek": {}, "hebrew": {}, "danish": {}, "dansk": {}, "norwegian": {}, "norsk": {}, "finnish": {}, "suomi": {},
}

func (r iapLocalizationNameLanguageRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.State == nil || ctx.State.Spec.IAP == nil {
		return nil
	}
	out := make([]Diagnostic, 0)
	for _, productID := range sortedKeys(ctx.State.Spec.IAP.Products) {
		product := ctx.State.Spec.IAP.Products[productID]
		for _, locale := range sortedKeys(product.Localizations) {
			loc := product.Localizations[locale]
			if loc.Name == nil {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(*loc.Name))
			if _, isLanguage := languageNames[name]; !isLanguage {
				continue
			}
			out = append(out, Diagnostic{
				RuleID:   r.ID(),
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"IAP %q localization %s is named %q — a language name, not a product name",
					productID, locale, *loc.Name,
				),
				Path: fmt.Sprintf("/spec/iap/products/%s/localizations/%s/name", productID, locale),
				FixHint: "name what the customer buys (e.g. \"Lifetime Access\"); the locale field already says " +
					"what language the entry is for.",
				Reference: "Rejection corpus: PassDMV 2.1(b) 2026-04-14 (\"cannot locate the In-App Purchases, such as English\")",
			})
		}
	}
	return out
}
