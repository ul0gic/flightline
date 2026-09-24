package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type AppEventLocalizationAttributes struct {
	Locale           string `json:"locale"`
	Name             string `json:"name,omitempty"`
	ShortDescription string `json:"shortDescription,omitempty"`
	LongDescription  string `json:"longDescription,omitempty"`
}

type AppEventLocalizationView struct {
	ID         string                         `json:"id"`
	EventID    string                         `json:"eventId"`
	Attributes AppEventLocalizationAttributes `json:"attributes"`
}

func ListAppEventLocalizations(ctx context.Context, c *Client, eventID string) ([]AppEventLocalizationView, error) {
	if eventID == "" {
		return nil, errors.New("event ID is required")
	}
	out := make([]AppEventLocalizationView, 0)
	for page, err := range Pages[AppEventLocalizationAttributes](ctx, c, "/v1/appEvents/"+url.PathEscape(eventID)+"/localizations", url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list event localizations: %w", err)
		}
		for _, row := range page.Data {
			v, err := projectAppEventLocalization(row, eventID)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func GetAppEventLocalization(ctx context.Context, c *Client, localizationID string) (AppEventLocalizationView, error) {
	if localizationID == "" {
		return AppEventLocalizationView{}, errors.New("localization ID is required")
	}
	resp, err := Get[Single[AppEventLocalizationAttributes]](ctx, c, "/v1/appEventLocalizations/"+url.PathEscape(localizationID), nil)
	if err != nil {
		return AppEventLocalizationView{}, err
	}
	v, err := projectAppEventLocalization(resp.Data, "")
	if err != nil {
		return AppEventLocalizationView{}, err
	}
	if v.ID != localizationID {
		return AppEventLocalizationView{}, errors.New("localization response ID mismatch")
	}
	return v, nil
}

func projectAppEventLocalization(row Resource[AppEventLocalizationAttributes], eventID string) (AppEventLocalizationView, error) {
	if row.Type != "appEventLocalizations" || row.ID == "" || row.Attributes.Locale == "" {
		return AppEventLocalizationView{}, errors.New("event localization missing type, ID, or locale")
	}
	if rel, ok := row.Relationships["appEvent"]; ok && len(rel.Data) > 0 {
		id, err := iapToOneID(rel, "appEvents")
		if err != nil {
			return AppEventLocalizationView{}, err
		}
		if eventID != "" && eventID != id {
			return AppEventLocalizationView{}, fmt.Errorf("event localization %s belongs to another event", row.ID)
		}
		eventID = id
	}
	return AppEventLocalizationView{ID: row.ID, EventID: eventID, Attributes: row.Attributes}, nil
}

func FindAppEventLocalization(ctx context.Context, c *Client, eventID, localizationID string) (AppEventLocalizationView, error) {
	localizations, err := ListAppEventLocalizations(ctx, c, eventID)
	if err != nil {
		return AppEventLocalizationView{}, err
	}
	for _, loc := range localizations {
		if loc.ID == localizationID {
			return loc, nil
		}
	}
	return AppEventLocalizationView{}, fmt.Errorf("localization %s does not belong to event %s", localizationID, eventID)
}

func ValidateAppEventLocalization(a AppEventLocalizationAttributes) error {
	if strings.TrimSpace(a.Locale) == "" {
		return errors.New("localization locale is required")
	}
	if len([]rune(a.Name)) > 30 || len([]rune(a.ShortDescription)) > 50 || len([]rune(a.LongDescription)) > 120 {
		return errors.New("event localization exceeds Apple's 30/50/120 character limits")
	}
	return nil
}

func CreateAppEventLocalization(ctx context.Context, c *Client, eventID string, attrs AppEventLocalizationAttributes) (AppEventLocalizationView, bool, error) {
	if err := ValidateAppEventLocalization(attrs); err != nil {
		return AppEventLocalizationView{}, false, err
	}
	locs, err := ListAppEventLocalizations(ctx, c, eventID)
	if err != nil {
		return AppEventLocalizationView{}, false, err
	}
	for _, loc := range locs {
		if loc.Attributes.Locale == attrs.Locale {
			return loc, false, fmt.Errorf("event locale %s already exists as %s", attrs.Locale, loc.ID)
		}
	}
	body := map[string]any{"data": map[string]any{"type": "appEventLocalizations", "attributes": attrs, "relationships": map[string]any{"appEvent": map[string]any{"data": map[string]string{"type": "appEvents", "id": eventID}}}}}
	resp, err := Post[Single[AppEventLocalizationAttributes]](ctx, c, "/v1/appEventLocalizations", nil, body)
	if err != nil {
		return AppEventLocalizationView{}, false, fmt.Errorf("create event localization outcome may be uncertain; inspect before retrying: %w", err)
	}
	v, err := projectAppEventLocalization(resp.Data, eventID)
	if err != nil {
		return AppEventLocalizationView{}, false, err
	}
	if v.Attributes.Locale != attrs.Locale {
		return AppEventLocalizationView{}, false, errors.New("created localization locale mismatch")
	}
	return v, true, nil
}

func UpdateAppEventLocalization(ctx context.Context, c *Client, eventID, localizationID string, patch map[string]any) (AppEventLocalizationView, bool, error) {
	current, err := FindAppEventLocalization(ctx, c, eventID, localizationID)
	if err != nil {
		return AppEventLocalizationView{}, false, err
	}
	if _, ok := patch["locale"]; ok {
		return AppEventLocalizationView{}, false, errors.New("localization locale cannot be patched")
	}
	if len(patch) == 0 {
		return current, false, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appEventLocalizations", "id": localizationID, "attributes": patch}}
	resp, err := Patch[Single[AppEventLocalizationAttributes]](ctx, c, "/v1/appEventLocalizations/"+url.PathEscape(localizationID), nil, body)
	if err != nil {
		return AppEventLocalizationView{}, false, fmt.Errorf("update event localization outcome may be uncertain; inspect before retrying: %w", err)
	}
	v, err := projectAppEventLocalization(resp.Data, eventID)
	if err != nil {
		return AppEventLocalizationView{}, false, err
	}
	if v.ID != localizationID || v.Attributes.Locale != current.Attributes.Locale {
		return AppEventLocalizationView{}, false, errors.New("updated localization identity mismatch")
	}
	return v, true, nil
}

func DeleteAppEventLocalization(ctx context.Context, c *Client, eventID, localizationID string) error {
	if _, err := FindAppEventLocalization(ctx, c, eventID, localizationID); err != nil {
		return err
	}
	return c.Delete(ctx, "/v1/appEventLocalizations/"+url.PathEscape(localizationID), nil)
}
