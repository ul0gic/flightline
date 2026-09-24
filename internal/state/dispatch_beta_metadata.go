package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func ValidateBetaMetadataChange(ch plan.Change) error {
	if !strings.HasPrefix(ch.Path, "/spec/testflight/metadata/") {
		return fmt.Errorf("invalid beta metadata path %q", ch.Path)
	}
	rest := strings.TrimPrefix(ch.Path, "/spec/testflight/metadata/")
	switch {
	case strings.HasPrefix(rest, "appLocalizations/"):
		return validateBetaAppLocalizationChange(ch, strings.TrimPrefix(rest, "appLocalizations/"))
	case strings.HasPrefix(rest, "reviewDetails/"):
		return validateBetaReviewDetailChange(ch, strings.TrimPrefix(rest, "reviewDetails/"))
	case strings.HasPrefix(rest, "builds/"):
		return validateBetaBuildLocalizationChange(ch, strings.TrimPrefix(rest, "builds/"))
	default:
		return fmt.Errorf("unsupported beta metadata path %q", ch.Path)
	}
}

func validateBetaAppLocalizationChange(ch plan.Change, rest string) error {
	parts := strings.SplitN(rest, "/", 2)
	locale, err := unescapeTestFlightPathSegment(parts[0])
	if err != nil || locale == "" {
		return fmt.Errorf("invalid beta app localization path %q", ch.Path)
	}
	if len(parts) == 1 {
		if ch.Op != plan.OpCreate {
			return errors.New("beta app localization parent requires create")
		}
		var value config.BetaAppLocalizationSpec
		return decodeBetaChange(ch.To, &value)
	}
	_, stringValue := ch.To.(string)
	if (ch.Op != plan.OpUpdate && ch.Op != plan.OpCreate) || !betaAppLocalizationField(parts[1]) || !stringValue {
		return fmt.Errorf("invalid beta app localization field change %q", ch.Path)
	}
	return nil
}

func validateBetaReviewDetailChange(ch plan.Change, field string) error {
	if ch.Op != plan.OpUpdate && ch.Op != plan.OpCreate {
		return fmt.Errorf("invalid beta review detail operation %s", ch.Op)
	}
	switch field {
	case "contactFirstName", "contactLastName", "contactPhone", "contactEmail", "demoAccountName", "notes":
		if _, ok := ch.To.(string); ok {
			return nil
		}
	case "demoAccountRequired":
		if _, ok := ch.To.(bool); ok {
			return nil
		}
	}
	return fmt.Errorf("invalid or secret beta review detail field change %q", ch.Path)
}

func validateBetaBuildLocalizationChange(ch plan.Change, rest string) error {
	_, _, field, err := parseBetaBuildLocalizationPath(rest)
	if err != nil {
		return err
	}
	if field == "" {
		if ch.Op != plan.OpCreate {
			return errors.New("beta build localization parent requires create")
		}
		var value config.BetaBuildLocalizationSpec
		return decodeBetaChange(ch.To, &value)
	}
	if (ch.Op != plan.OpUpdate && ch.Op != plan.OpCreate) || field != "whatsNew" {
		return fmt.Errorf("invalid beta build localization field %q", ch.Path)
	}
	if _, ok := ch.To.(string); !ok {
		return errors.New("beta build localization whatsNew must be a string")
	}
	return nil
}

func applyBetaMetadataChange(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidateBetaMetadataChange(ch); err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(ch.Path, "/spec/testflight/metadata/")
	switch {
	case strings.HasPrefix(rest, "appLocalizations/"):
		return applyBetaAppLocalizationChange(ctx, c, appID, strings.TrimPrefix(rest, "appLocalizations/"), ch)
	case strings.HasPrefix(rest, "reviewDetails/"):
		return applyBetaReviewDetailChange(ctx, c, appID, strings.TrimPrefix(rest, "reviewDetails/"), ch)
	case strings.HasPrefix(rest, "builds/"):
		return applyBetaBuildLocalizationChange(ctx, c, appID, strings.TrimPrefix(rest, "builds/"), ch)
	default:
		return fmt.Errorf("unsupported beta metadata path %q", ch.Path)
	}
}

func applyBetaAppLocalizationChange(ctx context.Context, c *asc.Client, appID, rest string, ch plan.Change) error {
	parts := strings.SplitN(rest, "/", 2)
	locale, err := unescapeTestFlightPathSegment(parts[0])
	if err != nil || locale == "" {
		return fmt.Errorf("invalid beta app locale in %q", ch.Path)
	}
	rows, err := asc.ListBetaAppLocalizations(ctx, c, appID)
	if err != nil {
		return err
	}
	current, err := selectBetaAppLocalization(rows, locale)
	if err != nil {
		return err
	}
	if len(parts) == 1 {
		if ch.Op != plan.OpCreate {
			return fmt.Errorf("beta app localization %s requires create operation", locale)
		}
		var spec config.BetaAppLocalizationSpec
		if err := decodeBetaChange(ch.To, &spec); err != nil {
			return err
		}
		if current != nil {
			if betaAppLocalizationMatches(spec, current.Attributes) {
				return nil
			}
			return fmt.Errorf("beta app localization %s already exists with different fields; refresh the plan", locale)
		}
		_, err = asc.CreateBetaAppLocalization(ctx, c, appID, locale, betaAppLocalizationAttributes(spec))
		return err
	}
	if current == nil {
		return fmt.Errorf("beta app localization %s is absent; refresh the plan", locale)
	}
	field := parts[1]
	if !betaAppLocalizationField(field) {
		return fmt.Errorf("unsupported beta app localization field %q", field)
	}
	observed := betaAppLocalizationFieldValue(current.Attributes, field)
	noop, err := betaMetadataStaleGuard(observed, ch)
	if err != nil || noop {
		return err
	}
	_, err = asc.UpdateBetaAppLocalization(ctx, c, current.ID, map[string]any{field: ch.To})
	return err
}

func selectBetaAppLocalization(rows []asc.Resource[asc.BetaAppLocalizationAttributes], locale string) (*asc.Resource[asc.BetaAppLocalizationAttributes], error) {
	var selected *asc.Resource[asc.BetaAppLocalizationAttributes]
	for i := range rows {
		if rows[i].Attributes.Locale != locale {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("multiple beta app localizations for %s", locale)
		}
		selected = &rows[i]
	}
	return selected, nil
}

func betaAppLocalizationMatches(spec config.BetaAppLocalizationSpec, actual asc.BetaAppLocalizationAttributes) bool {
	for field, value := range betaAppLocalizationAttributes(spec) {
		if betaAppLocalizationFieldValue(actual, field) != value {
			return false
		}
	}
	return true
}

func betaAppLocalizationFieldValue(a asc.BetaAppLocalizationAttributes, field string) string {
	switch field {
	case "description":
		return a.Description
	case "feedbackEmail":
		return a.FeedbackEmail
	case "marketingUrl":
		return a.MarketingURL
	case "privacyPolicyUrl":
		return a.PrivacyPolicyURL
	case "tvOsPrivacyPolicy":
		return a.TVOSPrivacyPolicy
	default:
		return ""
	}
}

func betaAppLocalizationAttributes(spec config.BetaAppLocalizationSpec) map[string]any {
	attributes := make(map[string]any)
	for key, value := range map[string]*string{
		"description": spec.Description, "feedbackEmail": spec.FeedbackEmail, "marketingUrl": spec.MarketingURL,
		"privacyPolicyUrl": spec.PrivacyPolicyURL, "tvOsPrivacyPolicy": spec.TVOSPrivacyPolicy,
	} {
		if value != nil {
			attributes[key] = *value
		}
	}
	return attributes
}

func betaAppLocalizationField(field string) bool {
	switch field {
	case "description", "feedbackEmail", "marketingUrl", "privacyPolicyUrl", "tvOsPrivacyPolicy":
		return true
	default:
		return false
	}
}

func applyBetaReviewDetailChange(ctx context.Context, c *asc.Client, appID, field string, ch plan.Change) error {
	detail, err := asc.GetBetaAppReviewDetail(ctx, c, appID)
	if err != nil {
		return err
	}
	if detail == nil {
		return errors.New("beta app review detail absent; Apple exposes PATCH but no create endpoint")
	}
	switch field {
	case "contactFirstName", "contactLastName", "contactPhone", "contactEmail", "demoAccountName", "demoAccountRequired", "notes":
	default:
		return fmt.Errorf("unsupported beta app review field %q", field)
	}
	observed := betaReviewDetailFieldValue(detail.Attributes, field)
	noop, err := betaMetadataStaleGuard(observed, ch)
	if err != nil || noop {
		return err
	}
	_, err = asc.UpdateBetaAppReviewDetail(ctx, c, detail.ID, map[string]any{field: ch.To})
	return err
}

func betaReviewDetailFieldValue(a asc.BetaAppReviewDetailAttributes, field string) any {
	switch field {
	case "contactFirstName":
		return a.ContactFirstName
	case "contactLastName":
		return a.ContactLastName
	case "contactPhone":
		return a.ContactPhone
	case "contactEmail":
		return a.ContactEmail
	case "demoAccountName":
		return a.DemoAccountName
	case "demoAccountRequired":
		if a.DemoAccountRequired == nil {
			return nil
		}
		return *a.DemoAccountRequired
	case "notes":
		return a.Notes
	default:
		return nil
	}
}

func applyBetaBuildLocalizationChange(ctx context.Context, c *asc.Client, appID, rest string, ch plan.Change) error {
	selector, locale, field, err := parseBetaBuildLocalizationPath(rest)
	if err != nil {
		return err
	}
	buildID, err := resolveBetaBuildSelector(ctx, c, appID, selector)
	if err != nil {
		return err
	}
	rows, err := asc.ListBetaBuildLocalizations(ctx, c, buildID)
	if err != nil {
		return err
	}
	current, err := selectBetaBuildLocalization(rows, locale)
	if err != nil {
		return err
	}
	if field == "" {
		return createBetaBuildLocalizationChange(ctx, c, buildID, locale, current, ch)
	}
	if field != "whatsNew" || current == nil {
		return fmt.Errorf("unsupported or missing beta build localization field in %q", ch.Path)
	}
	noop, err := betaMetadataStaleGuard(current.Attributes.WhatsNew, ch)
	if err != nil || noop {
		return err
	}
	_, err = asc.UpdateBetaBuildLocalization(ctx, c, current.ID, map[string]any{"whatsNew": ch.To})
	return err
}

func createBetaBuildLocalizationChange(ctx context.Context, c *asc.Client, buildID, locale string, current *asc.Resource[asc.BetaBuildLocalizationAttributes], ch plan.Change) error {
	if ch.Op != plan.OpCreate {
		return fmt.Errorf("beta build localization %s requires create operation", locale)
	}
	var spec config.BetaBuildLocalizationSpec
	if err := decodeBetaChange(ch.To, &spec); err != nil {
		return err
	}
	if current != nil {
		if spec.WhatsNew == nil || *spec.WhatsNew == current.Attributes.WhatsNew {
			return nil
		}
		return fmt.Errorf("beta build localization %s already exists with different text; refresh the plan", locale)
	}
	attributes := map[string]any{}
	if spec.WhatsNew != nil {
		attributes["whatsNew"] = *spec.WhatsNew
	}
	_, err := asc.CreateBetaBuildLocalization(ctx, c, buildID, locale, attributes)
	return err
}

func selectBetaBuildLocalization(rows []asc.Resource[asc.BetaBuildLocalizationAttributes], locale string) (*asc.Resource[asc.BetaBuildLocalizationAttributes], error) {
	var selected *asc.Resource[asc.BetaBuildLocalizationAttributes]
	for i := range rows {
		if rows[i].Attributes.Locale != locale {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("multiple beta build localizations for %s", locale)
		}
		selected = &rows[i]
	}
	return selected, nil
}

func betaMetadataStaleGuard(observed any, ch plan.Change) (bool, error) {
	if reflect.DeepEqual(observed, ch.To) {
		return true, nil
	}
	if !reflect.DeepEqual(observed, ch.From) {
		return false, fmt.Errorf("beta metadata changed since planning at %s; refresh the plan", ch.Path)
	}
	return false, nil
}

func parseBetaBuildLocalizationPath(rest string) (selector config.BetaBuildSelector, locale, field string, err error) {
	parts := strings.SplitN(rest, "/localizations/", 2)
	if len(parts) != 2 {
		return config.BetaBuildSelector{}, "", "", fmt.Errorf("invalid beta build localization path %q", rest)
	}
	key, err := unescapeTestFlightPathSegment(parts[0])
	if err != nil {
		return config.BetaBuildSelector{}, "", "", err
	}
	selector, err = parseBetaBuildKey(key)
	if err != nil {
		return config.BetaBuildSelector{}, "", "", err
	}
	leaf := strings.SplitN(parts[1], "/", 2)
	locale, err = unescapeTestFlightPathSegment(leaf[0])
	if err != nil || locale == "" {
		return config.BetaBuildSelector{}, "", "", fmt.Errorf("invalid beta build locale in %q", rest)
	}
	if len(leaf) == 1 {
		return selector, locale, "", nil
	}
	return selector, locale, leaf[1], nil
}

func decodeBetaChange(value, target any) error {
	if value == nil {
		return errors.New("beta change value is null")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode beta change: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode beta change: %w", err)
	}
	return nil
}

func parseBetaBuildKey(key string) (config.BetaBuildSelector, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return config.BetaBuildSelector{}, fmt.Errorf("invalid beta build key %q", key)
	}
	platform, err := url.QueryUnescape(parts[0])
	if err != nil {
		return config.BetaBuildSelector{}, fmt.Errorf("decode beta platform: %w", err)
	}
	version, err := url.QueryUnescape(parts[1])
	if err != nil {
		return config.BetaBuildSelector{}, fmt.Errorf("decode beta version: %w", err)
	}
	number, err := url.QueryUnescape(parts[2])
	if err != nil {
		return config.BetaBuildSelector{}, fmt.Errorf("decode beta build number: %w", err)
	}
	if platform == "" || version == "" || number == "" {
		return config.BetaBuildSelector{}, errors.New("beta build key requires platform, version, number")
	}
	return config.BetaBuildSelector{Platform: platform, Version: version, Number: number}, nil
}
