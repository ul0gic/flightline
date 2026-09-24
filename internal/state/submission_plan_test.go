package state

import (
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF8A_SubmissionPlanOrdersVersionBeforeIAP(t *testing.T) {
	version := asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}
	plan, err := BuildSubmissionPlan("A1", "IOS", version, []asc.SubmissionItemProposal{
		{Relationship: "appStoreVersionExperimentV2", Type: "appStoreVersionExperiments", ID: "E1"},
		{Relationship: "appEvent", Type: "appEvents", ID: "EV1"},
		{Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: "I1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := plan.DesiredItems()
	want := []string{"appStoreVersion/V1", "inAppPurchaseVersion/I1", "appEvent/EV1", "appStoreVersionExperimentV2/E1"}
	for i, item := range got {
		if key := item.Relationship + "/" + item.ID; key != want[i] {
			t.Fatalf("items=%+v want=%v", got, want)
		}
	}
}

func TestF8A_SubmissionPlanRejectsDuplicateAndSecondVersion(t *testing.T) {
	version := asc.SubmissionItemProposal{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V1"}
	for _, items := range [][]asc.SubmissionItemProposal{
		{{Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: "I1"}, {Relationship: "inAppPurchaseVersion", Type: "inAppPurchaseVersions", ID: "I1"}},
		{{Relationship: "appStoreVersion", Type: "appStoreVersions", ID: "V2"}},
	} {
		if _, err := BuildSubmissionPlan("A1", "IOS", version, items); err == nil || !strings.Contains(err.Error(), "submission:") {
			t.Fatalf("err=%v", err)
		}
	}
}
