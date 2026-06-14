package lint

import (
	"fmt"
	"sort"
)

// localizationsCompletenessRule fires when a locale appears in one surface (metadata, screenshots, IAP) but not another.
// Warning (not Error): missing a locale doesn't always block submission, but Apple may reject. Offline-only.
type localizationsCompletenessRule struct{}

func init() { Register(localizationsCompletenessRule{}) }

func (localizationsCompletenessRule) ID() string         { return "localizations.completeness" }
func (localizationsCompletenessRule) Severity() Severity { return SeverityWarning }
func (localizationsCompletenessRule) Mode() Mode         { return ModeOffline }

func (r localizationsCompletenessRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.State == nil {
		return nil
	}
	surfaces := r.collectLocalizedSurfaces(ctx)
	if len(surfaces) < 2 {
		return nil // need at least two surfaces to compare
	}
	union := map[string]struct{}{}
	for _, locales := range surfaces {
		for loc := range locales {
			union[loc] = struct{}{}
		}
	}

	out := make([]Diagnostic, 0)
	for surface, locales := range surfaces {
		missing := make([]string, 0)
		for loc := range union {
			if _, ok := locales[loc]; !ok {
				missing = append(missing, loc)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)
		for _, loc := range missing {
			out = append(out, Diagnostic{
				RuleID:   r.ID(),
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"locale %q is declared in another surface but missing from %s",
					loc, surface,
				),
				Path: surfacePath(surface, loc),
				FixHint: "either add the locale to every localizable surface (metadata, " +
					"screenshots, iap.localizations) or remove it everywhere.",
				Reference: "PRD §L3: localizations.completeness",
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// collectLocalizedSurfaces returns a map[surfaceName]set[locale] for every surface with localizations declared.
func (localizationsCompletenessRule) collectLocalizedSurfaces(ctx CheckContext) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	if ctx.State == nil {
		return out
	}
	if md := ctx.State.Spec.Metadata; md != nil && len(md.Locales) > 0 {
		s := map[string]struct{}{}
		for k := range md.Locales {
			s[k] = struct{}{}
		}
		out["metadata"] = s
	}
	if sc := ctx.State.Spec.Screenshots; sc != nil && len(sc.Locales) > 0 {
		s := map[string]struct{}{}
		for k := range sc.Locales {
			s[k] = struct{}{}
		}
		out["screenshots"] = s
	}
	if iap := ctx.State.Spec.IAP; iap != nil && len(iap.Products) > 0 {
		s := map[string]struct{}{}
		for _, prod := range iap.Products {
			for k := range prod.Localizations {
				s[k] = struct{}{}
			}
		}
		if len(s) > 0 {
			out["iap.localizations"] = s
		}
	}
	return out
}

// surfacePath returns a JSON-Pointer-style path for a surface and locale.
func surfacePath(surface, locale string) string {
	switch surface {
	case "metadata":
		return "/spec/metadata/locales/" + locale
	case "screenshots":
		return "/spec/screenshots/locales/" + locale
	case "iap.localizations":
		return "/spec/iap/products/*/localizations/" + locale
	default:
		return "/spec/" + surface + "/" + locale
	}
}
