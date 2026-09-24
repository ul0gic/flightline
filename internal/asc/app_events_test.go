package asc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestF8B_EventListPaginationAndOwnership(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"type":"appEvents","id":"E2","attributes":{"referenceName":"second","eventState":"DRAFT"},"relationships":{"app":{"data":{"type":"apps","id":"A1"}}}}],"links":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appEvents","id":"E1","attributes":{"referenceName":"first","eventState":"DRAFT"}}],"links":{"next":"` + serverURL + `/v1/apps/A1/appEvents?page=2"}}`))
	}))
	defer srv.Close()
	serverURL = srv.URL
	items, err := ListAppEvents(context.Background(), fixtureClient(t, srv), "A1")
	if err != nil || len(items) != 2 || items[1].ID != "E2" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestF8B_CreateEventPayloadAndDraftConfirmation(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		case http.MethodPost:
			posts++
			var body struct {
				Data struct {
					Type          string                     `json:"type"`
					Attributes    map[string]json.RawMessage `json:"attributes"`
					Relationships struct {
						App struct {
							Data struct {
								ID string `json:"id"`
							} `json:"data"`
						} `json:"app"`
					} `json:"relationships"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Data.Type != "appEvents" || body.Data.Relationships.App.Data.ID != "A1" {
				t.Fatalf("body=%v", body)
			}
			if _, present := body.Data.Attributes["eventState"]; present {
				t.Fatal("created event attempted state transition")
			}
			_, _ = w.Write([]byte(`{"data":{"type":"appEvents","id":"E1","attributes":{"referenceName":"fall","eventState":"DRAFT"}}}`))
		}
	}))
	defer srv.Close()
	item, changed, err := CreateAppEvent(context.Background(), fixtureClient(t, srv), "A1", AppEventAttributes{ReferenceName: "fall"}, time.Now())
	if err != nil || !changed || posts != 1 || item.ID != "E1" {
		t.Fatalf("item=%+v changed=%v posts=%d err=%v", item, changed, posts, err)
	}
}

func TestF8B_ProposalRequiresCompleteMediaWithoutWriting(t *testing.T) {
	state := "PROCESSING"
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes++
			t.Errorf("unexpected write %s", r.Method)
		}
		switch r.URL.Path {
		case "/v1/apps/A1/appEvents":
			start := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
			end := time.Now().UTC().Add(49 * time.Hour).Format(time.RFC3339)
			publish := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
			_, _ = w.Write([]byte(`{"data":[{"type":"appEvents","id":"E1","attributes":{"referenceName":"fall","eventState":"DRAFT","badge":"LIVE_EVENT","deepLink":"example://event","purpose":"ATTRACT_NEW_USERS","priority":"NORMAL","purchaseRequirement":"NOT_REQUIRED","primaryLocale":"en-US","territorySchedules":[{"territories":["USA"],"publishStart":"` + publish + `","eventStart":"` + start + `","eventEnd":"` + end + `"}]}}],"links":{}}`))
		case "/v1/appEvents/E1/localizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appEventLocalizations","id":"L1","attributes":{"locale":"en-US","name":"Fall","shortDescription":"Event card","longDescription":"Event details"}}],"links":{}}`))
		case "/v1/appEventLocalizations/L1/appEventScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appEventScreenshots","id":"S1","attributes":{"fileName":"card.png","appEventAssetType":"EVENT_CARD","assetDeliveryState":{"state":"COMPLETE"}}},{"type":"appEventScreenshots","id":"S2","attributes":{"fileName":"detail.png","appEventAssetType":"EVENT_DETAILS_PAGE","assetDeliveryState":{"state":"` + state + `"}}}],"links":{}}`))
		case "/v1/appEventLocalizations/L1/appEventVideoClips":
			_, _ = w.Write([]byte(`{"data":[],"links":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	if _, err := ProposeAppEventSubmissionItem(context.Background(), c, "A1", "E1"); err == nil || !strings.Contains(err.Error(), "wait") {
		t.Fatalf("pending proposal err=%v", err)
	}
	state = "COMPLETE"
	p, err := ProposeAppEventSubmissionItem(context.Background(), c, "A1", "E1")
	if err != nil || p.Relationship != "appEvent" || p.ID != "E1" || writes != 0 {
		t.Fatalf("proposal=%+v writes=%d err=%v", p, writes, err)
	}
}

func TestF8B_EventScheduleBoundaries(t *testing.T) {
	now := time.Now().UTC()
	base := AppEventAttributes{ReferenceName: "fall", TerritorySchedules: []AppEventTerritorySchedule{{Territories: []string{"USA"}, PublishStart: now.Add(24 * time.Hour).Format(time.RFC3339), EventStart: now.Add(48 * time.Hour).Format(time.RFC3339), EventEnd: now.Add(49 * time.Hour).Format(time.RFC3339)}}}
	if err := ValidateAppEventAttributes(base, now); err != nil {
		t.Fatal(err)
	}
	base.TerritorySchedules[0].EventEnd = now.Add(47 * time.Hour).Format(time.RFC3339)
	if err := ValidateAppEventAttributes(base, now); err == nil {
		t.Fatal("reverse duration accepted")
	}
}
