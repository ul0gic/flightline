package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type ExperimentTreatment struct {
	ID           string `json:"id"`
	ExperimentID string `json:"experimentId"`
	Name         string `json:"name"`
	AppIconName  string `json:"appIconName,omitempty"`
}

type ExperimentLocalization struct {
	ID          string `json:"id"`
	TreatmentID string `json:"treatmentId"`
	Locale      string `json:"locale"`
}

type experimentTreatmentAttributes struct {
	Name        string `json:"name"`
	AppIconName string `json:"appIconName"`
}
type experimentLocalizationAttributes struct {
	Locale string `json:"locale"`
}

func projectExperimentTreatment(row Resource[experimentTreatmentAttributes], experimentID string) (ExperimentTreatment, error) {
	if row.Type != "appStoreVersionExperimentTreatments" || row.ID == "" || row.Attributes.Name == "" {
		return ExperimentTreatment{}, errors.New("treatment response has incomplete identity or name")
	}
	actual, err := experimentRelationshipID(row.Relationships, "appStoreVersionExperimentV2", "appStoreVersionExperiments")
	if err != nil {
		return ExperimentTreatment{}, err
	}
	if actual != experimentID {
		return ExperimentTreatment{}, errors.New("treatment belongs to another experiment")
	}
	return ExperimentTreatment{ID: row.ID, ExperimentID: actual, Name: row.Attributes.Name, AppIconName: row.Attributes.AppIconName}, nil
}

func ListExperimentTreatments(ctx context.Context, c *Client, experimentID string) ([]ExperimentTreatment, error) {
	if experimentID == "" {
		return nil, errors.New("experiment ID is required")
	}
	out := make([]ExperimentTreatment, 0)
	for page, err := range Pages[experimentTreatmentAttributes](ctx, c, "/v2/appStoreVersionExperiments/"+url.PathEscape(experimentID)+"/appStoreVersionExperimentTreatments", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list experiment treatments: %w", err)
		}
		for _, row := range page.Data {
			item, err := projectExperimentTreatment(row, experimentID)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func GetExperimentTreatment(ctx context.Context, c *Client, experimentID, treatmentID string) (ExperimentTreatment, error) {
	if experimentID == "" || treatmentID == "" {
		return ExperimentTreatment{}, errors.New("experiment and treatment IDs are required")
	}
	resp, err := Get[Single[experimentTreatmentAttributes]](ctx, c, "/v1/appStoreVersionExperimentTreatments/"+url.PathEscape(treatmentID), nil)
	if err != nil {
		return ExperimentTreatment{}, fmt.Errorf("read treatment: %w", err)
	}
	item, err := projectExperimentTreatment(resp.Data, experimentID)
	if err != nil {
		return ExperimentTreatment{}, err
	}
	if item.ID != treatmentID {
		return ExperimentTreatment{}, errors.New("treatment response ID mismatch")
	}
	return item, nil
}

func CreateExperimentTreatment(ctx context.Context, c *Client, experimentID, name, icon string) (ExperimentTreatment, error) {
	if experimentID == "" || name == "" {
		return ExperimentTreatment{}, errors.New("experiment ID and treatment name are required")
	}
	attrs := map[string]any{"name": name}
	if icon != "" {
		attrs["appIconName"] = icon
	}
	body := map[string]any{"data": map[string]any{"type": "appStoreVersionExperimentTreatments", "attributes": attrs, "relationships": map[string]any{"appStoreVersionExperimentV2": map[string]any{"data": map[string]any{"type": "appStoreVersionExperiments", "id": experimentID}}}}}
	resp, err := Post[Single[experimentTreatmentAttributes]](ctx, c, "/v1/appStoreVersionExperimentTreatments", nil, body)
	if err != nil {
		return ExperimentTreatment{}, fmt.Errorf("create treatment (inspect live before retry): %w", err)
	}
	item, err := projectExperimentTreatment(resp.Data, experimentID)
	if err != nil {
		return ExperimentTreatment{}, err
	}
	if item.Name != name || item.AppIconName != icon {
		return ExperimentTreatment{}, errors.New("created treatment differs; inspect live before retry")
	}
	return item, nil
}

func UpdateExperimentTreatment(ctx context.Context, c *Client, experimentID, treatmentID string, name, icon *string) (ExperimentTreatment, bool, error) {
	if name == nil && icon == nil {
		return ExperimentTreatment{}, false, errors.New("name or icon is required")
	}
	current, err := GetExperimentTreatment(ctx, c, experimentID, treatmentID)
	if err != nil {
		return ExperimentTreatment{}, false, err
	}
	attrs := map[string]any{}
	if name != nil {
		if *name == "" {
			return ExperimentTreatment{}, false, errors.New("name cannot be empty")
		}
		if *name != current.Name {
			attrs["name"] = *name
		}
	}
	if icon != nil && *icon != current.AppIconName {
		attrs["appIconName"] = *icon
	}
	if len(attrs) == 0 {
		return current, false, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appStoreVersionExperimentTreatments", "id": treatmentID, "attributes": attrs}}
	resp, err := Patch[Single[experimentTreatmentAttributes]](ctx, c, "/v1/appStoreVersionExperimentTreatments/"+url.PathEscape(treatmentID), nil, body)
	if err != nil {
		return ExperimentTreatment{}, false, fmt.Errorf("update treatment (inspect live before retry): %w", err)
	}
	item, err := projectExperimentTreatment(resp.Data, experimentID)
	if err != nil {
		return ExperimentTreatment{}, false, err
	}
	if item.ID != treatmentID {
		return ExperimentTreatment{}, false, errors.New("updated treatment ID mismatch; inspect live")
	}
	return item, true, nil
}

func DeleteExperimentTreatment(ctx context.Context, c *Client, experimentID, treatmentID string) error {
	if _, err := GetExperimentTreatment(ctx, c, experimentID, treatmentID); err != nil {
		return err
	}
	if err := c.Delete(ctx, "/v1/appStoreVersionExperimentTreatments/"+url.PathEscape(treatmentID), nil); err != nil {
		return fmt.Errorf("delete treatment (inspect live before retry): %w", err)
	}
	return nil
}

func projectExperimentLocalization(row Resource[experimentLocalizationAttributes], treatmentID string) (ExperimentLocalization, error) {
	if row.Type != "appStoreVersionExperimentTreatmentLocalizations" || row.ID == "" || row.Attributes.Locale == "" {
		return ExperimentLocalization{}, errors.New("localization response has incomplete identity or locale")
	}
	actual, err := experimentRelationshipID(row.Relationships, "appStoreVersionExperimentTreatment", "appStoreVersionExperimentTreatments")
	if err != nil {
		return ExperimentLocalization{}, err
	}
	if actual != treatmentID {
		return ExperimentLocalization{}, errors.New("localization belongs to another treatment")
	}
	return ExperimentLocalization{ID: row.ID, TreatmentID: actual, Locale: row.Attributes.Locale}, nil
}

func ListExperimentLocalizations(ctx context.Context, c *Client, treatmentID string) ([]ExperimentLocalization, error) {
	if treatmentID == "" {
		return nil, errors.New("treatment ID is required")
	}
	out := make([]ExperimentLocalization, 0)
	for page, err := range Pages[experimentLocalizationAttributes](ctx, c, "/v1/appStoreVersionExperimentTreatments/"+url.PathEscape(treatmentID)+"/appStoreVersionExperimentTreatmentLocalizations", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list treatment localizations: %w", err)
		}
		for _, row := range page.Data {
			item, err := projectExperimentLocalization(row, treatmentID)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func GetExperimentLocalization(ctx context.Context, c *Client, treatmentID, localizationID string) (ExperimentLocalization, error) {
	if treatmentID == "" || localizationID == "" {
		return ExperimentLocalization{}, errors.New("treatment and localization IDs are required")
	}
	resp, err := Get[Single[experimentLocalizationAttributes]](ctx, c, "/v1/appStoreVersionExperimentTreatmentLocalizations/"+url.PathEscape(localizationID), nil)
	if err != nil {
		return ExperimentLocalization{}, fmt.Errorf("read treatment localization: %w", err)
	}
	item, err := projectExperimentLocalization(resp.Data, treatmentID)
	if err != nil {
		return ExperimentLocalization{}, err
	}
	if item.ID != localizationID {
		return ExperimentLocalization{}, errors.New("localization response ID mismatch")
	}
	return item, nil
}

func CreateExperimentLocalization(ctx context.Context, c *Client, treatmentID, locale string) (ExperimentLocalization, error) {
	if treatmentID == "" || locale == "" {
		return ExperimentLocalization{}, errors.New("treatment ID and locale are required")
	}
	body := map[string]any{"data": map[string]any{"type": "appStoreVersionExperimentTreatmentLocalizations", "attributes": map[string]any{"locale": locale}, "relationships": map[string]any{"appStoreVersionExperimentTreatment": map[string]any{"data": map[string]any{"type": "appStoreVersionExperimentTreatments", "id": treatmentID}}}}}
	resp, err := Post[Single[experimentLocalizationAttributes]](ctx, c, "/v1/appStoreVersionExperimentTreatmentLocalizations", nil, body)
	if err != nil {
		return ExperimentLocalization{}, fmt.Errorf("create treatment localization (inspect live before retry): %w", err)
	}
	item, err := projectExperimentLocalization(resp.Data, treatmentID)
	if err != nil {
		return ExperimentLocalization{}, err
	}
	if item.Locale != locale {
		return ExperimentLocalization{}, errors.New("created localization differs; inspect live before retry")
	}
	return item, nil
}

func DeleteExperimentLocalization(ctx context.Context, c *Client, treatmentID, localizationID string) error {
	if _, err := GetExperimentLocalization(ctx, c, treatmentID, localizationID); err != nil {
		return err
	}
	if err := c.Delete(ctx, "/v1/appStoreVersionExperimentTreatmentLocalizations/"+url.PathEscape(localizationID), nil); err != nil {
		return fmt.Errorf("delete localization (inspect live before retry): %w", err)
	}
	return nil
}
