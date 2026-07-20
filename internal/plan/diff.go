// Package plan computes the leaf-level change set between desired and live *config.State.
// Nil sub-specs in desired mean "not managed": Flightline leaves that surface alone.
package plan

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ul0gic/flightline/internal/config"
)

// Op is one of create / update / delete.
type Op string

// Op values.
const (
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
)

// Change is a single leaf-level mutation keyed by Resource (dispatch table) and Path (JSON-Pointer).
type Change struct {
	Op       Op     `json:"op"`
	Resource string `json:"resource"`
	Path     string `json:"path"`
	From     any    `json:"from,omitempty"`
	To       any    `json:"to,omitempty"`
	Hint     string `json:"hint,omitempty"`
}

// Diff computes the change set from live to desired state.
func Diff(desired, live *config.State) []Change {
	var out []Change
	if desired == nil {
		return out
	}

	if live == nil {
		live = &config.State{}
	}

	diffVersion(desired.Spec.Version, live.Spec.Version, &out)
	diffBuild(desired.Spec.Build, live.Spec.Build, &out)
	diffMetadata(desired.Spec.Metadata, live.Spec.Metadata, &out)
	diffScreenshots(desired.Spec.Screenshots, live.Spec.Screenshots, &out)
	diffIAP(desired.Spec.IAP, live.Spec.IAP, &out)
	diffAgeRating(desired.Spec.AgeRating, live.Spec.AgeRating, &out)
	diffExportCompliance(desired.Spec.ExportCompliance, live.Spec.ExportCompliance, &out)
	diffReviewerDemo(desired.Spec.ReviewerDemo, live.Spec.ReviewerDemo, &out)
	diffCategories(desired.Spec.Categories, live.Spec.Categories, &out)
	diffPricing(desired.Spec.Pricing, live.Spec.Pricing, &out)
	diffTestFlight(desired.Spec.TestFlight, live.Spec.TestFlight, &out)
	diffCustomProductPages(derefCPP(desired.Spec.CustomProductPages), derefCPP(live.Spec.CustomProductPages), &out)

	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// emitIfDiff appends a change when desired and live differ.
// A nil desired means "not managed": no change is emitted.
func emitIfDiff(out *[]Change, resource, path string, desired, live any) {
	// Deref before the nil test: a typed nil pointer is not interface-nil, and an
	// absent scalar means "not managed" — it must never delete the live answer.
	d := derefAny(desired)
	if d == nil {
		return
	}
	l := derefAny(live)
	if reflect.DeepEqual(d, l) {
		return
	}

	op := OpUpdate
	if l == nil {
		op = OpCreate
	}
	*out = append(*out, Change{
		Op:       op,
		Resource: resource,
		Path:     path,
		From:     l,
		To:       d,
		Hint:     formatHint(path, l, d),
	})
}

// derefAny reflects through pointer chains so *string("foo") compares equal to *string("foo").
func derefAny(v any) any {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	return rv.Interface()
}

func formatHint(path string, from, to any) string {
	switch {
	case from == nil:
		return fmt.Sprintf("%s: <unset> -> %v", path, to)
	case to == nil:
		return fmt.Sprintf("%s: %v -> <unset>", path, from)
	default:
		return fmt.Sprintf("%s: %v -> %v", path, from, to)
	}
}

func diffVersion(d, l *config.VersionSpec, out *[]Change) {
	if d == nil {
		return
	}
	if l == nil {
		l = &config.VersionSpec{}
	}
	emitIfDiff(out, "version", "/spec/version/releaseType", d.ReleaseType, l.ReleaseType)
	emitIfDiff(out, "version", "/spec/version/earliestReleaseDate", d.EarliestReleaseDate, l.EarliestReleaseDate)
	emitIfDiff(out, "version", "/spec/version/copyright", d.Copyright, l.Copyright)
	emitIfDiff(out, "version", "/spec/version/downloadable", d.Downloadable, l.Downloadable)
}

func diffBuild(d, l *config.BuildSpec, out *[]Change) {
	if d == nil {
		return
	}
	var live string
	if l != nil {
		live = l.Number
	}
	if d.Number != live {
		op := OpUpdate
		if live == "" {
			op = OpCreate
		}
		*out = append(*out, Change{
			Op:       op,
			Resource: "build",
			Path:     "/spec/build/number",
			From:     emptyToNil(live),
			To:       d.Number,
			Hint:     "attach build " + d.Number,
		})
	}
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func diffMetadata(d, l *config.MetadataSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := map[string]config.MetadataLocale{}
	if l != nil && l.Locales != nil {
		live = l.Locales
	}
	for _, loc := range sortedKeys(d.Locales) {
		dl := d.Locales[loc]
		ll := live[loc]
		base := "/spec/metadata/locales/" + loc
		emitIfDiff(out, "metadata."+loc, base+"/name", dl.Name, ll.Name)
		emitIfDiff(out, "metadata."+loc, base+"/subtitle", dl.Subtitle, ll.Subtitle)
		emitIfDiff(out, "metadata."+loc, base+"/description", dl.Description, ll.Description)
		emitIfDiff(out, "metadata."+loc, base+"/keywords", dl.Keywords, ll.Keywords)
		emitIfDiff(out, "metadata."+loc, base+"/whatsNew", dl.WhatsNew, ll.WhatsNew)
		emitIfDiff(out, "metadata."+loc, base+"/promotionalText", dl.PromotionalText, ll.PromotionalText)
		emitIfDiff(out, "metadata."+loc, base+"/marketingUrl", dl.MarketingURL, ll.MarketingURL)
		emitIfDiff(out, "metadata."+loc, base+"/supportUrl", dl.SupportURL, ll.SupportURL)
		emitIfDiff(out, "metadata."+loc, base+"/privacyPolicyUrl", dl.PrivacyPolicyURL, ll.PrivacyPolicyURL)
	}
}

func diffScreenshots(d, l *config.ScreenshotsSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := map[string]map[string][]config.ScreenshotFile{}
	if l != nil && l.Locales != nil {
		live = l.Locales
	}
	for _, loc := range sortedKeys(d.Locales) {
		dDevices := d.Locales[loc]
		lDevices := live[loc]
		for _, dev := range sortedKeys(dDevices) {
			path := "/spec/screenshots/locales/" + loc + "/" + dev
			d := dDevices[dev]
			ll := lDevices[dev]
			if !equalScreenshotFiles(d, ll) {
				op := OpUpdate
				if len(ll) == 0 {
					op = OpCreate
				}
				*out = append(*out, Change{
					Op:       op,
					Resource: "screenshots." + loc + "." + dev,
					Path:     path,
					From:     ll,
					To:       d,
					Hint:     fmt.Sprintf("upload %d screenshot(s) for %s/%s", len(d), loc, dev),
				})
			}
		}
	}
}

func diffIAP(d, l *config.IAPSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := map[string]config.IAPProduct{}
	if l != nil && l.Products != nil {
		live = l.Products
	}
	for _, pid := range sortedKeys(d.Products) {
		dp := d.Products[pid]
		lp, exists := live[pid]
		base := "/spec/iap/products/" + pid
		if !exists {
			*out = append(*out, Change{
				Op: OpCreate, Resource: "iap." + pid,
				Path: base, From: nil, To: dp,
				Hint: fmt.Sprintf("create IAP %s (%s)", pid, dp.Type),
			})
			continue
		}
		if dp.Type != lp.Type {
			emitIfDiff(out, "iap."+pid, base+"/type", &dp.Type, &lp.Type)
		}
		emitIfDiff(out, "iap."+pid, base+"/name", dp.Name, lp.Name)
		emitIfDiff(out, "iap."+pid, base+"/familySharable", dp.FamilySharable, lp.FamilySharable)
		emitIfDiff(out, "iap."+pid, base+"/contentHosting", dp.ContentHosting, lp.ContentHosting)
		emitIfDiff(out, "iap."+pid, base+"/reviewNote", dp.ReviewNote, lp.ReviewNote)
		if !equalIAPReviewScreenshot(dp.ReviewScreenshot, lp.ReviewScreenshot) && dp.ReviewScreenshot != nil {
			op := OpUpdate
			if lp.ReviewScreenshot == nil {
				op = OpCreate
			}
			*out = append(*out, Change{
				Op: op, Resource: "iap." + pid + ".reviewScreenshot",
				Path: base + "/reviewScreenshot", From: lp.ReviewScreenshot, To: dp.ReviewScreenshot,
				Hint: "upload IAP review screenshot for " + pid,
			})
		}
		// localizations
		for _, loc := range sortedKeys(dp.Localizations) {
			dloc := dp.Localizations[loc]
			lloc := lp.Localizations[loc]
			lpath := base + "/localizations/" + loc
			emitIfDiff(out, "iap."+pid+".loc."+loc, lpath+"/name", dloc.Name, lloc.Name)
			emitIfDiff(out, "iap."+pid+".loc."+loc, lpath+"/description", dloc.Description, lloc.Description)
		}
	}
}

func diffAgeRating(d, l *config.AgeRatingSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.AgeRatingSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "ageRating", "/spec/ageRating/cartoonOrFantasyViolence", d.CartoonOrFantasyViolence, live.CartoonOrFantasyViolence)
	emitIfDiff(out, "ageRating", "/spec/ageRating/realisticViolence", d.RealisticViolence, live.RealisticViolence)
	emitIfDiff(out, "ageRating", "/spec/ageRating/prolongedGraphicSadisticRealisticViolence", d.ProlongedGraphicSadisticRealisticViolence, live.ProlongedGraphicSadisticRealisticViolence)
	emitIfDiff(out, "ageRating", "/spec/ageRating/profanityOrCrudeHumor", d.ProfanityOrCrudeHumor, live.ProfanityOrCrudeHumor)
	emitIfDiff(out, "ageRating", "/spec/ageRating/matureSuggestiveThemes", d.MatureSuggestiveThemes, live.MatureSuggestiveThemes)
	emitIfDiff(out, "ageRating", "/spec/ageRating/horrorOrFearThemes", d.HorrorOrFearThemes, live.HorrorOrFearThemes)
	emitIfDiff(out, "ageRating", "/spec/ageRating/medicalOrTreatmentInformation", d.MedicalOrTreatmentInformation, live.MedicalOrTreatmentInformation)
	emitIfDiff(out, "ageRating", "/spec/ageRating/alcoholTobaccoOrDrugUseOrReferences", d.AlcoholTobaccoOrDrugUseOrReferences, live.AlcoholTobaccoOrDrugUseOrReferences)
	emitIfDiff(out, "ageRating", "/spec/ageRating/contestsAndGambling", d.ContestsAndGambling, live.ContestsAndGambling)
	emitIfDiff(out, "ageRating", "/spec/ageRating/sexualContentOrNudity", d.SexualContentOrNudity, live.SexualContentOrNudity)
	emitIfDiff(out, "ageRating", "/spec/ageRating/sexualContentGraphicAndNudity", d.SexualContentGraphicAndNudity, live.SexualContentGraphicAndNudity)
	emitIfDiff(out, "ageRating", "/spec/ageRating/gambling", d.Gambling, live.Gambling)
	emitIfDiff(out, "ageRating", "/spec/ageRating/gamblingSimulated", d.GamblingSimulated, live.GamblingSimulated)
	emitIfDiff(out, "ageRating", "/spec/ageRating/gunsOrOtherWeapons", d.GunsOrOtherWeapons, live.GunsOrOtherWeapons)
	emitIfDiff(out, "ageRating", "/spec/ageRating/advertising", d.Advertising, live.Advertising)
	emitIfDiff(out, "ageRating", "/spec/ageRating/ageAssurance", d.AgeAssurance, live.AgeAssurance)
	emitIfDiff(out, "ageRating", "/spec/ageRating/healthOrWellnessTopics", d.HealthOrWellnessTopics, live.HealthOrWellnessTopics)
	emitIfDiff(out, "ageRating", "/spec/ageRating/lootBox", d.LootBox, live.LootBox)
	emitIfDiff(out, "ageRating", "/spec/ageRating/messagingAndChat", d.MessagingAndChat, live.MessagingAndChat)
	emitIfDiff(out, "ageRating", "/spec/ageRating/parentalControls", d.ParentalControls, live.ParentalControls)
	emitIfDiff(out, "ageRating", "/spec/ageRating/userGeneratedContent", d.UserGeneratedContent, live.UserGeneratedContent)
	emitIfDiff(out, "ageRating", "/spec/ageRating/socialMedia", d.SocialMedia, live.SocialMedia)
	emitIfDiff(out, "ageRating", "/spec/ageRating/socialMediaAgeRestricted", d.SocialMediaAgeRestricted, live.SocialMediaAgeRestricted)
	emitIfDiff(out, "ageRating", "/spec/ageRating/unrestrictedWebAccess", d.UnrestrictedWebAccess, live.UnrestrictedWebAccess)
	emitIfDiff(out, "ageRating", "/spec/ageRating/kidsAgeBand", d.KidsAgeBand, live.KidsAgeBand)
	emitIfDiff(out, "ageRating", "/spec/ageRating/seventeenPlus", d.SeventeenPlus, live.SeventeenPlus)
}

func diffExportCompliance(d, l *config.ExportComplianceSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.ExportComplianceSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/usesNonExemptEncryption", d.UsesNonExemptEncryption, live.UsesNonExemptEncryption)
	if d.Declaration != nil {
		liveDecl := config.ExportComplianceDeclaration{}
		if live.Declaration != nil {
			liveDecl = *live.Declaration
		}
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/containsProprietaryCryptography", d.Declaration.ContainsProprietaryCryptography, liveDecl.ContainsProprietaryCryptography)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/containsThirdPartyCryptography", d.Declaration.ContainsThirdPartyCryptography, liveDecl.ContainsThirdPartyCryptography)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/availableOnFrenchStore", d.Declaration.AvailableOnFrenchStore, liveDecl.AvailableOnFrenchStore)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/usesEncryption", d.Declaration.UsesEncryption, liveDecl.UsesEncryption)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/exempt", d.Declaration.Exempt, liveDecl.Exempt)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/eccn", d.Declaration.ECCN, liveDecl.ECCN)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/documentName", d.Declaration.DocumentName, liveDecl.DocumentName)
		emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/declaration/documentUrl", d.Declaration.DocumentURL, liveDecl.DocumentURL)
	}
}

func diffReviewerDemo(d, l *config.ReviewerDemoSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.ReviewerDemoSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/username", d.Username, live.Username)
	// Diff the reference, not the resolved secret: apply resolves at write time so plan is stable.
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/passwordRef", d.PasswordRef, live.PasswordRef)
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/passwordFile", d.PasswordFile, live.PasswordFile)
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/notes", d.Notes, live.Notes)
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/contactName", d.ContactName, live.ContactName)
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/contactEmail", d.ContactEmail, live.ContactEmail)
	emitIfDiff(out, "reviewerDemo", "/spec/reviewerDemo/contactPhone", d.ContactPhone, live.ContactPhone)
}

func diffCategories(d, l *config.CategoriesSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.CategoriesSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "categories", "/spec/categories/primary", d.Primary, live.Primary)
	emitIfDiff(out, "categories", "/spec/categories/secondary", d.Secondary, live.Secondary)
	if !equalStringSlices(d.PrimarySubcategories, live.PrimarySubcategories) {
		op := OpUpdate
		if len(live.PrimarySubcategories) == 0 {
			op = OpCreate
		}
		*out = append(*out, Change{
			Op: op, Resource: "categories", Path: "/spec/categories/primarySubcategories",
			From: live.PrimarySubcategories, To: d.PrimarySubcategories,
			Hint: fmt.Sprintf("primarySubcategories: %v -> %v", live.PrimarySubcategories, d.PrimarySubcategories),
		})
	}
	if !equalStringSlices(d.SecondarySubcategories, live.SecondarySubcategories) {
		op := OpUpdate
		if len(live.SecondarySubcategories) == 0 {
			op = OpCreate
		}
		*out = append(*out, Change{
			Op: op, Resource: "categories", Path: "/spec/categories/secondarySubcategories",
			From: live.SecondarySubcategories, To: d.SecondarySubcategories,
			Hint: fmt.Sprintf("secondarySubcategories: %v -> %v", live.SecondarySubcategories, d.SecondarySubcategories),
		})
	}
}

func diffPricing(d, l *config.PricingSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.PricingSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "pricing", "/spec/pricing/baseTerritory", d.BaseTerritory, live.BaseTerritory)
	emitIfDiff(out, "pricing", "/spec/pricing/appPricePointId", d.AppPricePointID, live.AppPricePointID)
}

func diffTestFlight(d, l *config.TestFlightSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := map[string]config.TestFlightGroup{}
	if l != nil && l.Groups != nil {
		live = l.Groups
	}
	for _, g := range sortedKeys(d.Groups) {
		dg := d.Groups[g]
		lg, exists := live[g]
		base := "/spec/testflight/groups/" + g
		if !exists {
			*out = append(*out, Change{
				Op: OpCreate, Resource: "testflight." + g,
				Path: base, To: dg,
				Hint: "create TestFlight group " + g,
			})
			continue
		}
		emitIfDiff(out, "testflight."+g, base+"/isInternal", dg.IsInternal, lg.IsInternal)
		emitIfDiff(out, "testflight."+g, base+"/publicLink", dg.PublicLink, lg.PublicLink)
		emitIfDiff(out, "testflight."+g, base+"/publicLinkLimit", dg.PublicLinkLimit, lg.PublicLinkLimit)
		dEmails := testerEmails(dg.Testers)
		lEmails := testerEmails(lg.Testers)
		for _, e := range dEmails {
			if !contains(lEmails, e) {
				*out = append(*out, Change{
					Op: OpCreate, Resource: "testflight." + g + ".testers",
					Path: base + "/testers/" + e, To: e,
					Hint: fmt.Sprintf("add tester %s to %s", e, g),
				})
			}
		}
		for _, e := range lEmails {
			if !contains(dEmails, e) {
				*out = append(*out, Change{
					Op: OpDelete, Resource: "testflight." + g + ".testers",
					Path: base + "/testers/" + e, From: e,
					Hint: fmt.Sprintf("remove tester %s from %s", e, g),
				})
			}
		}
	}
}

func diffCustomProductPages(d, l config.CustomProductPagesSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.CustomProductPagesSpec{}
	if l != nil {
		live = l
	}
	for _, name := range sortedKeys(d) {
		dp := d[name]
		lp, exists := live[name]
		base := "/spec/customProductPages/" + name
		if !exists {
			*out = append(*out, Change{
				Op: OpCreate, Resource: "customProductPages." + name,
				Path: base, To: dp,
				Hint: "create custom product page " + name,
			})
			continue
		}
		emitIfDiff(out, "customProductPages."+name, base+"/visible", dp.Visible, lp.Visible)
		for _, loc := range sortedKeys(dp.Localizations) {
			dl := dp.Localizations[loc]
			ll := lp.Localizations[loc]
			lpath := base + "/localizations/" + loc
			emitIfDiff(out, "customProductPages."+name+".loc."+loc, lpath+"/promotionalText", dl.PromotionalText, ll.PromotionalText)
			for _, dev := range sortedKeys(dl.Screenshots) {
				dShots := dl.Screenshots[dev]
				lShots := ll.Screenshots[dev]
				if !equalScreenshotFiles(dShots, lShots) {
					op := OpUpdate
					if len(lShots) == 0 {
						op = OpCreate
					}
					*out = append(*out, Change{
						Op: op, Resource: "customProductPages." + name + ".loc." + loc + ".screenshots",
						Path: lpath + "/screenshots/" + dev,
						From: lShots, To: dShots,
						Hint: fmt.Sprintf("upload %d screenshot(s) for CPP %s %s/%s", len(dShots), name, loc, dev),
					})
				}
			}
		}
	}
}

func equalScreenshotFiles(desired, live []config.ScreenshotFile) bool {
	if len(desired) != len(live) {
		return false
	}
	desiredIDs := make([]string, len(desired))
	liveIDs := make([]string, len(live))
	for i := range desired {
		desiredIDs[i] = screenshotIdentity(desired[i])
		liveIDs[i] = screenshotIdentity(live[i])
	}
	sort.Strings(desiredIDs)
	sort.Strings(liveIDs)
	return equalStringSlices(desiredIDs, liveIDs)
}

func screenshotIdentity(file config.ScreenshotFile) string {
	if checksum := screenshotChecksum(file); checksum != "" {
		return "checksum:" + checksum
	}
	alt := ""
	if file.Alt != nil {
		alt = *file.Alt
	}
	return "path:" + file.Path + "\x00alt:" + alt
}

func screenshotChecksum(file config.ScreenshotFile) string {
	if file.SourceFileChecksum != "" {
		return file.SourceFileChecksum
	}
	if file.Alt != nil && strings.HasPrefix(*file.Alt, "checksum:") {
		return strings.TrimPrefix(*file.Alt, "checksum:")
	}
	return ""
}

func equalIAPReviewScreenshot(desired, live *config.IAPReviewScreenshot) bool {
	if desired == nil || live == nil {
		return desired == live
	}
	if desired.SourceFileChecksum != "" && live.SourceFileChecksum != "" {
		return desired.SourceFileChecksum == live.SourceFileChecksum
	}
	return desired.Path == live.Path
}

func derefCPP(p *config.CustomProductPagesSpec) config.CustomProductPagesSpec {
	if p == nil {
		return nil
	}
	return *p
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func testerEmails(t []config.TestFlightTester) []string {
	out := make([]string, 0, len(t))
	for _, te := range t {
		out = append(out, strings.ToLower(strings.TrimSpace(te.Email)))
	}
	sort.Strings(out)
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
