package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"
)

type AppEventTerritorySchedule struct {
	Territories  []string `json:"territories"`
	PublishStart string   `json:"publishStart"`
	EventStart   string   `json:"eventStart"`
	EventEnd     string   `json:"eventEnd"`
}

type AppEventAttributes struct {
	ReferenceName       string                      `json:"referenceName"`
	Badge               string                      `json:"badge,omitempty"`
	EventState          string                      `json:"eventState,omitempty"`
	DeepLink            string                      `json:"deepLink,omitempty"`
	PurchaseRequirement string                      `json:"purchaseRequirement,omitempty"`
	PrimaryLocale       string                      `json:"primaryLocale,omitempty"`
	Priority            string                      `json:"priority,omitempty"`
	Purpose             string                      `json:"purpose,omitempty"`
	TerritorySchedules  []AppEventTerritorySchedule `json:"territorySchedules,omitempty"`
}

type AppEventView struct {
	ID         string             `json:"id"`
	AppID      string             `json:"appId"`
	Attributes AppEventAttributes `json:"attributes"`
}

func ListAppEvents(ctx context.Context, c *Client, appID string) ([]AppEventView, error) {
	if appID == "" {
		return nil, errors.New("app ID is required")
	}
	out := make([]AppEventView, 0)
	path := "/v1/apps/" + url.PathEscape(appID) + "/appEvents"
	for page, err := range Pages[AppEventAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list app events: %w", err)
		}
		for _, row := range page.Data {
			v, err := projectAppEvent(row, appID)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func GetAppEvent(ctx context.Context, c *Client, eventID string) (AppEventView, error) {
	if eventID == "" {
		return AppEventView{}, errors.New("event ID is required")
	}
	resp, err := Get[Single[AppEventAttributes]](ctx, c, "/v1/appEvents/"+url.PathEscape(eventID), nil)
	if err != nil {
		return AppEventView{}, fmt.Errorf("read app event: %w", err)
	}
	v, err := projectAppEvent(resp.Data, "")
	if err != nil {
		return AppEventView{}, err
	}
	if v.ID != eventID {
		return AppEventView{}, errors.New("app event response ID mismatch")
	}
	return v, nil
}

func projectAppEvent(row Resource[AppEventAttributes], appID string) (AppEventView, error) {
	if row.Type != "appEvents" || row.ID == "" || row.Attributes.ReferenceName == "" {
		return AppEventView{}, errors.New("app event missing type, ID, or reference name")
	}
	if rel, ok := row.Relationships["app"]; ok && len(rel.Data) > 0 {
		id, err := iapToOneID(rel, "apps")
		if err != nil {
			return AppEventView{}, err
		}
		if appID != "" && appID != id {
			return AppEventView{}, fmt.Errorf("app event %s belongs to another app", row.ID)
		}
		appID = id
	}
	return AppEventView{ID: row.ID, AppID: appID, Attributes: row.Attributes}, nil
}

func FindAppEvent(ctx context.Context, c *Client, appID, eventID string) (AppEventView, error) {
	events, err := ListAppEvents(ctx, c, appID)
	if err != nil {
		return AppEventView{}, err
	}
	var selected *AppEventView
	for i := range events {
		if events[i].ID != eventID {
			continue
		}
		if selected != nil {
			return AppEventView{}, fmt.Errorf("duplicate app event ID %s", eventID)
		}
		selected = &events[i]
	}
	if selected == nil {
		return AppEventView{}, fmt.Errorf("app event %s does not belong to app %s", eventID, appID)
	}
	return *selected, nil
}

func ValidateAppEventAttributes(a AppEventAttributes, now time.Time) error {
	if err := validateEventMetadata(a); err != nil {
		return err
	}
	return validateEventSchedules(a.TerritorySchedules, now)
}

func validateEventMetadata(a AppEventAttributes) error {
	if strings.TrimSpace(a.ReferenceName) == "" || len([]rune(a.ReferenceName)) > 64 {
		return errors.New("event reference name must contain 1–64 characters")
	}
	if a.Badge != "" && !eventEnums[a.Badge] {
		return fmt.Errorf("invalid event badge %q", a.Badge)
	}
	if a.Priority != "" && a.Priority != "HIGH" && a.Priority != "NORMAL" {
		return fmt.Errorf("invalid event priority %q", a.Priority)
	}
	if a.Purpose != "" && !eventPurposes[a.Purpose] {
		return fmt.Errorf("invalid event purpose %q", a.Purpose)
	}
	if a.DeepLink != "" {
		u, err := url.Parse(a.DeepLink)
		if err != nil || u.Scheme == "" {
			return errors.New("event deep link must be an absolute URI")
		}
	}
	return nil
}

func validateEventSchedules(schedules []AppEventTerritorySchedule, now time.Time) error {
	territories := make(map[string]bool)
	var earliest, latest time.Time
	for _, s := range schedules {
		for _, territory := range s.Territories {
			if territory == "" || territories[territory] {
				return fmt.Errorf("duplicate or empty event territory %q", territory)
			}
			territories[territory] = true
		}
		start, err := validateEventSchedule(s, now)
		if err != nil {
			return err
		}
		if earliest.IsZero() || start.Before(earliest) {
			earliest = start
		}
		if latest.IsZero() || start.After(latest) {
			latest = start
		}
	}
	if !earliest.IsZero() && latest.Sub(earliest) > 48*time.Hour {
		return errors.New("customized territory event starts must fall within 48 hours")
	}
	return nil
}

func validateEventSchedule(s AppEventTerritorySchedule, now time.Time) (time.Time, error) {
	if len(s.Territories) == 0 {
		return time.Time{}, errors.New("event territory schedule requires territories")
	}
	p, e1 := time.Parse(time.RFC3339, s.PublishStart)
	start, e2 := time.Parse(time.RFC3339, s.EventStart)
	end, e3 := time.Parse(time.RFC3339, s.EventEnd)
	if e1 != nil || e2 != nil || e3 != nil {
		return time.Time{}, errors.New("event schedule dates must be RFC3339")
	}
	if p.After(start) || !start.Before(end) {
		return time.Time{}, errors.New("event schedule requires publishStart <= eventStart < eventEnd")
	}
	if end.Sub(start) < 15*time.Minute || end.Sub(start) > 31*24*time.Hour {
		return time.Time{}, errors.New("event duration must be 15 minutes to 31 days")
	}
	if start.Sub(p) > 14*24*time.Hour {
		return time.Time{}, errors.New("event publishStart may precede eventStart by at most 14 days")
	}
	if !start.After(now) {
		return time.Time{}, errors.New("eventStart must be in the future")
	}
	return start, nil
}

var eventEnums = map[string]bool{"LIVE_EVENT": true, "PREMIERE": true, "CHALLENGE": true, "COMPETITION": true, "NEW_SEASON": true, "MAJOR_UPDATE": true, "SPECIAL_EVENT": true}
var eventPurposes = map[string]bool{"APPROPRIATE_FOR_ALL_USERS": true, "ATTRACT_NEW_USERS": true, "KEEP_ACTIVE_USERS_INFORMED": true, "BRING_BACK_LAPSED_USERS": true}

func CreateAppEvent(ctx context.Context, c *Client, appID string, attrs AppEventAttributes, now time.Time) (AppEventView, bool, error) {
	if appID == "" {
		return AppEventView{}, false, errors.New("app ID is required")
	}
	if err := ValidateAppEventAttributes(attrs, now); err != nil {
		return AppEventView{}, false, err
	}
	events, err := ListAppEvents(ctx, c, appID)
	if err != nil {
		return AppEventView{}, false, err
	}
	for i := range events {
		if events[i].Attributes.ReferenceName == attrs.ReferenceName {
			return events[i], false, fmt.Errorf("event reference name %q already exists as %s; inspect before creating", attrs.ReferenceName, events[i].ID)
		}
	}
	attrs.EventState = ""
	body := map[string]any{"data": map[string]any{"type": "appEvents", "attributes": attrs, "relationships": map[string]any{"app": map[string]any{"data": map[string]string{"type": "apps", "id": appID}}}}}
	resp, err := Post[Single[AppEventAttributes]](ctx, c, "/v1/appEvents", nil, body)
	if err != nil {
		return AppEventView{}, false, fmt.Errorf("create app event outcome may be uncertain; inspect by reference name before retrying: %w", err)
	}
	v, err := projectAppEvent(resp.Data, appID)
	if err != nil {
		return AppEventView{}, false, err
	}
	if v.Attributes.ReferenceName != attrs.ReferenceName || v.Attributes.EventState != "DRAFT" {
		return AppEventView{}, false, errors.New("created event identity/state unconfirmed; inspect before retrying")
	}
	return v, true, nil
}

func UpdateAppEvent(ctx context.Context, c *Client, appID, eventID string, patch map[string]any) (AppEventView, bool, error) {
	current, err := FindAppEvent(ctx, c, appID, eventID)
	if err != nil {
		return AppEventView{}, false, err
	}
	if current.Attributes.EventState != "DRAFT" {
		return AppEventView{}, false, fmt.Errorf("event %s is %s; only DRAFT events may be edited", eventID, current.Attributes.EventState)
	}
	if len(patch) == 0 {
		return current, false, nil
	}
	if _, ok := patch["eventState"]; ok {
		return AppEventView{}, false, errors.New("eventState cannot be patched")
	}
	merged, err := prepareEventUpdate(ctx, c, appID, eventID, current.Attributes, patch)
	if err != nil {
		return AppEventView{}, false, err
	}
	if reflect.DeepEqual(merged, current.Attributes) {
		return current, false, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appEvents", "id": eventID, "attributes": patch}}
	resp, err := Patch[Single[AppEventAttributes]](ctx, c, "/v1/appEvents/"+url.PathEscape(eventID), nil, body)
	if err != nil {
		return AppEventView{}, false, fmt.Errorf("update event outcome may be uncertain; inspect before retrying: %w", err)
	}
	v, err := projectAppEvent(resp.Data, appID)
	if err != nil {
		return AppEventView{}, false, err
	}
	if v.ID != eventID {
		return AppEventView{}, false, errors.New("updated event ID mismatch")
	}
	return v, true, nil
}

func prepareEventUpdate(ctx context.Context, c *Client, appID, eventID string, current AppEventAttributes, patch map[string]any) (AppEventAttributes, error) {
	merged, err := mergeEventPatch(current, patch)
	if err != nil {
		return AppEventAttributes{}, err
	}
	if err := ValidateAppEventAttributes(merged, time.Now().UTC()); err != nil {
		return AppEventAttributes{}, err
	}
	if merged.ReferenceName == current.ReferenceName {
		return merged, nil
	}
	events, err := ListAppEvents(ctx, c, appID)
	if err != nil {
		return AppEventAttributes{}, err
	}
	for i := range events {
		if events[i].ID != eventID && events[i].Attributes.ReferenceName == merged.ReferenceName {
			return AppEventAttributes{}, fmt.Errorf("event reference name %q already exists", merged.ReferenceName)
		}
	}
	return merged, nil
}

func mergeEventPatch(current AppEventAttributes, patch map[string]any) (AppEventAttributes, error) {
	allowed := map[string]bool{"referenceName": true, "badge": true, "deepLink": true, "purchaseRequirement": true, "primaryLocale": true, "priority": true, "purpose": true, "territorySchedules": true}
	bytes, err := json.Marshal(current)
	if err != nil {
		return AppEventAttributes{}, err
	}
	var merged map[string]any
	if err := json.Unmarshal(bytes, &merged); err != nil {
		return AppEventAttributes{}, err
	}
	for key, value := range patch {
		if !allowed[key] {
			return AppEventAttributes{}, fmt.Errorf("unsupported event update field %q", key)
		}
		merged[key] = value
	}
	bytes, err = json.Marshal(merged)
	if err != nil {
		return AppEventAttributes{}, err
	}
	var result AppEventAttributes
	if err := json.Unmarshal(bytes, &result); err != nil {
		return AppEventAttributes{}, err
	}
	return result, nil
}

func DeleteAppEvent(ctx context.Context, c *Client, appID, eventID string) error {
	event, err := FindAppEvent(ctx, c, appID, eventID)
	if err != nil {
		return err
	}
	switch event.Attributes.EventState {
	case "DRAFT", "ARCHIVED", "APPROVED":
	default:
		return fmt.Errorf("event %s in %s cannot be deleted", eventID, event.Attributes.EventState)
	}
	return c.Delete(ctx, "/v1/appEvents/"+url.PathEscape(eventID), nil)
}

// VerifyAppEventProposal rechecks eligibility immediately before an item is attached.
// A proposal itself carries identity, not authorization to submit.
func VerifyAppEventProposal(ctx context.Context, c *Client, appID, eventID string) error {
	event, err := FindAppEvent(ctx, c, appID, eventID)
	if err != nil {
		return err
	}
	if event.Attributes.EventState != "DRAFT" {
		return fmt.Errorf("event %s is %s, not DRAFT", eventID, event.Attributes.EventState)
	}
	if len(event.Attributes.TerritorySchedules) == 0 {
		return errors.New("event proposal requires a territory schedule")
	}
	if err := ValidateAppEventAttributes(event.Attributes, time.Now().UTC()); err != nil {
		return err
	}
	if err := validateEventReviewMetadata(event.Attributes); err != nil {
		return err
	}
	localizations, err := ListAppEventLocalizations(ctx, c, eventID)
	if err != nil {
		return err
	}
	if len(localizations) == 0 {
		return errors.New("event proposal requires a localization")
	}
	return verifyEventLocalizations(ctx, c, localizations, event.Attributes.PrimaryLocale)
}

func verifyEventLocalizations(ctx context.Context, c *Client, localizations []AppEventLocalizationView, primary string) error {
	seen := make(map[string]bool, len(localizations))
	for i := range localizations {
		loc := &localizations[i]
		if seen[loc.Attributes.Locale] {
			return fmt.Errorf("duplicate event localization %s", loc.Attributes.Locale)
		}
		seen[loc.Attributes.Locale] = true
		if err := verifyEventLocalization(ctx, c, *loc); err != nil {
			return err
		}
	}
	if !seen[primary] {
		return fmt.Errorf("event primary locale %q is absent", primary)
	}
	return nil
}

func verifyEventLocalization(ctx context.Context, c *Client, loc AppEventLocalizationView) error {
	a := loc.Attributes
	if err := ValidateAppEventLocalization(a); err != nil {
		return err
	}
	if a.Name == "" || a.ShortDescription == "" || a.LongDescription == "" {
		return fmt.Errorf("event locale %s lacks review text", a.Locale)
	}
	media, err := ListAppEventMedia(ctx, c, loc.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range media {
		asset := &media[i]
		if asset.DeliveryState.State != "COMPLETE" {
			return fmt.Errorf("event media %s is %s; wait before proposing review", asset.ID, asset.DeliveryState.State)
		}
		if seen[asset.AssetType] {
			return fmt.Errorf("event locale %s has ambiguous %s media", a.Locale, asset.AssetType)
		}
		seen[asset.AssetType] = true
	}
	if !seen["EVENT_CARD"] || !seen["EVENT_DETAILS_PAGE"] {
		return fmt.Errorf("event locale %s requires complete card and detail media", a.Locale)
	}
	return nil
}

func validateEventReviewMetadata(a AppEventAttributes) error {
	if a.Badge == "" || a.DeepLink == "" || a.Purpose == "" || a.Priority == "" || a.PurchaseRequirement == "" || a.PrimaryLocale == "" {
		return errors.New("event proposal requires badge, deep link, purpose, priority, purchase requirement, and primary locale")
	}
	return nil
}

func ProposeAppEventSubmissionItem(ctx context.Context, c *Client, appID, eventID string) (SubmissionItemProposal, error) {
	if err := VerifyAppEventProposal(ctx, c, appID, eventID); err != nil {
		return SubmissionItemProposal{}, err
	}
	return SubmissionItemProposal{Relationship: "appEvent", Type: "appEvents", ID: eventID}, nil
}
