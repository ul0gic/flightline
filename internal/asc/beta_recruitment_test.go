package asc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF5C_BetaRecruitmentReadAndActionPayloads(t *testing.T) {
	var patch, notify map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/betaGroups/G1/betaRecruitmentCriteria":
			_, _ = fmt.Fprint(w, `{"data":{"type":"betaRecruitmentCriteria","id":"C1","attributes":{"deviceFamilyOsVersionFilters":[{"deviceFamily":"IPHONE","minimumOsInclusive":"18.0"}]}}}`)
		case "PATCH /v1/buildBetaDetails/D1":
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Errorf("patch decode: %v", err)
			}
			_, _ = fmt.Fprint(w, `{"data":{"type":"buildBetaDetails","id":"D1","attributes":{"autoNotifyEnabled":false}}}`)
		case "POST /v1/buildBetaNotifications":
			if err := json.NewDecoder(r.Body).Decode(&notify); err != nil {
				t.Errorf("notify decode: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"data":{"type":"buildBetaNotifications","id":"N1"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	criterion, err := GetBetaRecruitmentCriterion(context.Background(), c, "G1")
	if err != nil || criterion == nil || len(criterion.Attributes.DeviceFamilyOSVersionFilters) != 1 {
		t.Fatalf("criterion=%+v err=%v", criterion, err)
	}
	if _, err := UpdateBuildAutoNotify(context.Background(), c, "D1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := NotifyBetaBuild(context.Background(), c, "B1"); err != nil {
		t.Fatal(err)
	}
	patchData := requiredMap(t, patch, "data")
	if patchData["type"] != "buildBetaDetails" || patchData["id"] != "D1" || requiredMap(t, patchData, "attributes")["autoNotifyEnabled"] != false {
		t.Fatalf("patch=%v", patch)
	}
	notifyData := requiredMap(t, notify, "data")
	build := requiredMap(t, requiredMap(t, requiredMap(t, notifyData, "relationships"), "build"), "data")
	if build["type"] != "builds" || build["id"] != "B1" {
		t.Fatalf("notify=%v", notify)
	}
}

func TestF5C_BetaNotifyIncompleteConfirmationWarnsBeforeRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"data":{"type":"buildBetaNotifications"}}`)
	}))
	defer srv.Close()
	_, err := NotifyBetaBuild(context.Background(), fixtureClient(t, srv), "B1")
	if err == nil || !strings.Contains(err.Error(), "before retrying") {
		t.Fatalf("err=%v", err)
	}
}
