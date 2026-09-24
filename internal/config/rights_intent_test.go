package config

import "testing"

func TestF6B_RightsIntentRequiresAuthoredDeclarationAndCompleteCreation(t *testing.T) {
	bad := "UNKNOWN"
	empty := ""
	desired := &State{Spec: StateSpec{ContentRights: &bad, AppEULA: &AppEULASpec{AgreementText: &empty}}}
	if got := ValidateRightsIntent("state.yaml", desired, nil); len(got) != 3 {
		t.Fatalf("diagnostics=%+v", got)
	}
	valid := "USES_THIRD_PARTY_CONTENT"
	text := "Terms"
	ids := []string{"USA"}
	desired.Spec.ContentRights = &valid
	desired.Spec.AppEULA = &AppEULASpec{AgreementText: &text, Territories: &ids}
	if got := ValidateRightsIntent("state.yaml", desired, nil); len(got) != 0 {
		t.Fatalf("diagnostics=%+v", got)
	}
}

func TestF6B_RightsIntentAllowsUnchangedEmptyObservedFields(t *testing.T) {
	empty := ""
	ids := []string{}
	live := &State{Spec: StateSpec{AppEULA: &AppEULASpec{AgreementText: &empty, Territories: &ids}}}
	desired := &State{Spec: StateSpec{AppEULA: &AppEULASpec{AgreementText: &empty, Territories: &ids}}}
	if got := ValidateRightsIntent("state.yaml", desired, live); len(got) != 0 {
		t.Fatalf("diagnostics=%+v", got)
	}
}
