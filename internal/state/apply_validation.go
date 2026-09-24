package state

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// ValidateChanges checks the entire plan without network or filesystem access.
// Callers must reject all errors before dispatching any change.
func ValidateChanges(changes []plan.Change) []ChangeError {
	var failures []ChangeError
	for _, change := range changes {
		if err := validateChange(change); err != nil {
			failures = append(failures, newChangeError(change, err))
		}
	}
	return failures
}

func validateChange(ch plan.Change) error {
	for _, entry := range dispatchTable {
		if !entry.match(ch.Path) {
			continue
		}
		if err := validateChangeOp(ch); err != nil {
			return err
		}
		if entry.validate != nil {
			return entry.validate(ch)
		}
		return validateChangeValue(ch)
	}
	return errUnmapped(ch)
}

func unmappedLeaf(ch plan.Change, reason string) error {
	return fmt.Errorf("%s at %s: %w", reason, ch.Path, ErrUnmappedChange)
}

func validateChangeOp(ch plan.Change) error {
	if strings.Contains(ch.Path, "/testers/") && strings.HasPrefix(ch.Path, "/spec/testflight/groups/") {
		if ch.Op == plan.OpCreate || ch.Op == plan.OpDelete {
			return nil
		}
		return fmt.Errorf("testflight tester change requires create or delete, got %s", ch.Op)
	}
	if ch.Op == plan.OpCreate || ch.Op == plan.OpUpdate {
		return nil
	}
	return fmt.Errorf("unsupported %s operation at %s", ch.Op, ch.Path)
}

func validateChangeValue(ch plan.Change) error {
	switch {
	case strings.HasPrefix(ch.Path, "/spec/version/"):
		return validateVersionChange(ch)
	case ch.Path == "/spec/build/number":
		return requireNonemptyString(ch.To, "build number")
	case strings.HasPrefix(ch.Path, "/spec/metadata/locales/"):
		return validateMetadataChange(ch)
	case strings.HasPrefix(ch.Path, "/spec/screenshots/locales/"):
		return validateScreenshotChange(ch)
	case strings.HasPrefix(ch.Path, "/spec/iap/products/"):
		return validateIAPChange(ch)
	case strings.HasPrefix(ch.Path, "/spec/ageRating/"):
		return validateAgeRatingChange(ch)
	case ch.Path == "/spec/exportCompliance/usesNonExemptEncryption":
		return requireBool(ch.To)
	case ch.Path == "/spec/exportCompliance/declaration":
		if ch.Op == plan.OpDelete {
			return errors.New("declaration deletion is unsupported")
		}
		_, err := declarationCreateAttributes(ch.To)
		return err
	case strings.HasPrefix(ch.Path, "/spec/reviewerDemo/"):
		return validateReviewerDemoChange(ch)
	case strings.HasPrefix(ch.Path, "/spec/categories/"):
		return validateCategoryChange(ch)
	case ch.Path == "/spec/pricing":
		return validatePricingChange(ch)
	case strings.HasPrefix(ch.Path, "/spec/testflight/groups/"):
		return validateTestFlightChange(ch)
	case strings.HasPrefix(ch.Path, "/spec/customProductPages/"):
		return validateCPPChange(ch)
	default:
		return unmappedLeaf(ch, "unsupported version leaf")
	}
}

func requireString(value any) error {
	if _, ok := value.(string); !ok {
		return fmt.Errorf("expected string, got %T", value)
	}
	return nil
}

func requireNonemptyString(value any, label string) error {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s requires a nonempty string", label)
	}
	return nil
}

func requireBool(value any) error {
	if _, ok := value.(bool); !ok {
		return fmt.Errorf("expected bool, got %T", value)
	}
	return nil
}

func validateVersionChange(ch plan.Change) error {
	switch ch.Path {
	case "/spec/version/copyright", "/spec/version/earliestReleaseDate":
		return requireString(ch.To)
	case "/spec/version/releaseType":
		value, ok := ch.To.(string)
		if !ok || (value != "MANUAL" && value != "AFTER_APPROVAL" && value != "SCHEDULED") {
			return errors.New("invalid releaseType")
		}
		return nil
	case "/spec/version/downloadable":
		return requireBool(ch.To)
	default:
		return errUnmapped(ch)
	}
}

func validateMetadataChange(ch plan.Change) error {
	parts := strings.Split(strings.TrimPrefix(ch.Path, "/spec/metadata/locales/"), "/")
	if len(parts) != 2 || parts[0] == "" || (!isVersionLocalizationField(parts[1]) && !isAppInfoLocalizationField(parts[1])) {
		return unmappedLeaf(ch, "unsupported metadata localization leaf")
	}
	return requireString(ch.To)
}

func validateScreenshotChange(ch plan.Change) error {
	parts := strings.Split(strings.TrimPrefix(ch.Path, "/spec/screenshots/locales/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return unmappedLeaf(ch, "malformed screenshot path")
	}
	return validateScreenshotFiles(ch.To)
}

func validateScreenshotFiles(value any) error {
	files, ok := value.([]config.ScreenshotFile)
	if !ok {
		return fmt.Errorf("expected screenshot file list, got %T", value)
	}
	for _, file := range files {
		if file.Path == "" {
			return errors.New("screenshot path is required")
		}
	}
	return nil
}

func validateAgeRatingChange(ch plan.Change) error {
	leaf := strings.TrimPrefix(ch.Path, "/spec/ageRating/")
	if leaf == "seventeenPlus" {
		return errors.New("ageRating.seventeenPlus is derived by Apple and cannot be written")
	}
	if _, ok := ageRatingSchemaToWire[leaf]; !ok {
		return unmappedLeaf(ch, "unsupported ageRating leaf")
	}
	if leaf == "gracRatingClassificationNumber" {
		return requireString(ch.To)
	}
	if values, ok := ageRatingOverrideValues[leaf]; ok {
		return validateAgeRatingOverride(ch.To, leaf, values)
	}

	if leaf == "kidsAgeBand" {
		value, ok := ch.To.(string)
		if !ok || (value != "FIVE_AND_UNDER" && value != "SIX_TO_EIGHT" && value != "NINE_TO_ELEVEN") {
			return errors.New("invalid kidsAgeBand")
		}
		return nil
	}
	if _, frequency := ageRatingFrequencyFields[leaf]; frequency {
		value, ok := ch.To.(string)
		if !ok || !validAgeFrequency(value) {
			return fmt.Errorf("invalid age rating frequency for %s", leaf)
		}
		return nil
	}
	return requireBool(ch.To)
}

var ageRatingFrequencyFields = map[string]struct{}{
	"cartoonOrFantasyViolence": {}, "realisticViolence": {}, "prolongedGraphicSadisticRealisticViolence": {},
	"profanityOrCrudeHumor": {}, "matureSuggestiveThemes": {}, "horrorOrFearThemes": {},
	"medicalOrTreatmentInformation": {}, "alcoholTobaccoOrDrugUseOrReferences": {}, "contestsAndGambling": {},
	"sexualContentOrNudity": {}, "sexualContentGraphicAndNudity": {}, "gamblingSimulated": {}, "gunsOrOtherWeapons": {},
}

func validAgeFrequency(value string) bool {
	switch value {
	case "NONE", "INFREQUENT_OR_MILD", "FREQUENT_OR_INTENSE", "INFREQUENT", "FREQUENT":
		return true
	default:
		return false
	}
}

func validateReviewerDemoChange(ch plan.Change) error {
	leaf := strings.TrimPrefix(ch.Path, "/spec/reviewerDemo/")
	switch leaf {
	case "username", "passwordRef", "passwordFile", "notes", "contactName", "contactEmail", "contactPhone":
		return requireString(ch.To)
	default:
		return unmappedLeaf(ch, "unsupported reviewerDemo leaf")
	}
}

func validateCategoryChange(ch plan.Change) error {
	leaf := strings.TrimPrefix(ch.Path, "/spec/categories/")
	switch leaf {
	case "primary", "secondary":
		if ch.To == nil {
			return nil
		}
		return requireString(ch.To)
	case "primarySubcategories", "secondarySubcategories":
		values, err := asStringSlice(ch.To)
		if err != nil {
			return err
		}
		if len(values) > 2 {
			return errors.New("at most two subcategories are supported")
		}
		return nil
	default:
		return unmappedLeaf(ch, "unsupported category leaf")
	}
}

func validatePricingChange(ch plan.Change) error {
	pair, err := pricingChangePair(ch.To)
	if err != nil {
		return err
	}
	if pair.BaseTerritory == nil || *pair.BaseTerritory == "" || pair.AppPricePointID == nil || *pair.AppPricePointID == "" {
		return errors.New("pricing requires a complete baseTerritory and appPricePointId pair")
	}
	return nil
}

func validateIAPChange(ch plan.Change) error {
	parts := strings.Split(strings.TrimPrefix(ch.Path, "/spec/iap/products/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return unmappedLeaf(ch, "malformed IAP path")
	}
	if len(parts) == 1 {
		if ch.Op != plan.OpCreate {
			return errors.New("IAP parent requires create operation")
		}
		product, ok := ch.To.(config.IAPProduct)
		if !ok {
			return fmt.Errorf("IAP create requires IAPProduct, got %T", ch.To)
		}
		if err := validateIAPParentCreate(product); err != nil {
			return err
		}
		if product.Type != "CONSUMABLE" && product.Type != "NON_CONSUMABLE" && product.Type != "NON_RENEWING_SUBSCRIPTION" {
			return fmt.Errorf("unsupported IAP type %q", product.Type)
		}
		return nil
	}
	return validateIAPChildChange(ch, parts[1:])
}

func validateIAPChildChange(ch plan.Change, parts []string) error {
	switch {
	case len(parts) == 1 && (parts[0] == "type" || parts[0] == "contentHosting"):
		return fmt.Errorf("IAP %s is not writable", parts[0])
	case len(parts) == 1 && writableIAPField(parts[0]):
		if parts[0] == "familySharable" {
			return requireBool(ch.To)
		}
		return requireString(ch.To)
	case len(parts) == 1 && parts[0] == "reviewScreenshot":
		_, err := decodeIAPReviewScreenshot(ch.To)
		return err
	case len(parts) >= 2 && parts[0] == "localizations":
		return validateIAPLocalizationChange(ch, parts[1:])
	default:
		return unmappedLeaf(ch, "unsupported IAP leaf")
	}
}

func validateIAPLocalizationChange(ch plan.Change, parts []string) error {
	switch {
	case len(parts) == 1 && parts[0] != "":
		if ch.Op != plan.OpCreate {
			return errors.New("whole IAP localization requires create operation")
		}
		localization, ok := ch.To.(config.IAPLocalization)
		if !ok || localization.Name == nil {
			return errors.New("IAP localization create requires complete name")
		}
		if localization.Description != nil {
			return validateIAPDescriptionWrite(*localization.Description)
		}
		return nil
	case len(parts) == 2 && parts[0] != "" && writableIAPLocalizationField(parts[1]):
		if parts[1] == "description" {
			return validateIAPDescriptionWrite(ch.To)
		}
		return requireString(ch.To)
	default:
		return unmappedLeaf(ch, "unsupported IAP leaf")
	}
}

func validateTestFlightChange(ch plan.Change) error {
	parts := strings.Split(strings.TrimPrefix(ch.Path, "/spec/testflight/groups/"), "/")
	if len(parts) == 0 {
		return unmappedLeaf(ch, "malformed TestFlight path")
	}
	name, err := unescapeTestFlightPathSegment(parts[0])
	if err != nil || name == "" {
		return unmappedLeaf(ch, "invalid TestFlight group segment")
	}
	if len(parts) == 1 {
		if ch.Op != plan.OpCreate {
			return errors.New("TestFlight group parent requires create operation")
		}
		group, ok := ch.To.(config.TestFlightGroup)
		if !ok || group.IsInternal == nil {
			return errors.New("TestFlight group create requires isInternal")
		}
		return nil
	}
	return validateTestFlightChild(ch, parts[1:])
}

func validateTestFlightChild(ch plan.Change, parts []string) error {
	if len(parts) == 1 {
		switch parts[0] {
		case "isInternal":
			return errors.New("TestFlight group kind is immutable")
		case "publicLink":
			return requireBool(ch.To)
		case "publicLinkLimit":
			limit, ok := ch.To.(int)
			if !ok || limit < 0 {
				return errors.New("publicLinkLimit requires a nonnegative integer")
			}
			return nil
		}
	}
	if len(parts) == 2 && parts[0] == "testers" {
		email, err := unescapeTestFlightPathSegment(parts[1])
		if err != nil || email == "" || !strings.Contains(email, "@") {
			return unmappedLeaf(ch, "invalid TestFlight tester segment")
		}
		return nil
	}
	return unmappedLeaf(ch, "unsupported TestFlight leaf")
}

func validateCPPChange(ch plan.Change) error {
	parts := strings.Split(strings.TrimPrefix(ch.Path, "/spec/customProductPages/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return unmappedLeaf(ch, "malformed custom product page path")
	}
	if pageName, err := unescapeJSONPointerToken(parts[0]); err != nil || pageName == "" {
		return unmappedLeaf(ch, "malformed custom product page pointer")
	}
	if len(parts) == 1 {
		if ch.Op != plan.OpCreate {
			return errors.New("custom product page parent requires create operation")
		}
		if _, ok := ch.To.(config.CustomProductPage); !ok {
			return fmt.Errorf("custom product page create requires CustomProductPage, got %T", ch.To)
		}
		return nil
	}
	return validateCPPChild(ch, parts[1:])
}

func validateCPPChild(ch plan.Change, parts []string) error {
	if len(parts) == 1 && parts[0] == "visible" {
		return requireBool(ch.To)
	}
	if len(parts) == 3 && parts[0] == "localizations" && parts[1] != "" && parts[2] == "promotionalText" {
		return requireString(ch.To)
	}
	if len(parts) == 4 && parts[0] == "localizations" && parts[1] != "" && parts[2] == "screenshots" && parts[3] != "" {
		return validateScreenshotFiles(ch.To)
	}
	return unmappedLeaf(ch, "unsupported custom product page leaf")
}

var ageRatingOverrideValues = map[string]string{
	"ageRatingOverrideV2":    "NONE|NINE_PLUS|THIRTEEN_PLUS|SIXTEEN_PLUS|EIGHTEEN_PLUS|UNRATED",
	"koreaAgeRatingOverride": "NONE|ALL|TWELVE_PLUS|FIFTEEN_PLUS|NINETEEN_PLUS",
}

func validateAgeRatingOverride(target any, leaf, values string) error {
	value, ok := target.(string)
	if !ok || value == "" || !slices.Contains(strings.Split(values, "|"), value) {
		return fmt.Errorf("invalid %s", leaf)
	}
	return nil
}
