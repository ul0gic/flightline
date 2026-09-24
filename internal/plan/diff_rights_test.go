package plan

import (
	"reflect"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestF6B_DiffRightsMergesOmittedEULAFieldsAndNoops(t *testing.T) {
	text := "Original"
	wantText := "Updated"
	ids := []string{"GBR", "USA"}
	live := &config.State{Spec: config.StateSpec{AppEULA: &config.AppEULASpec{AgreementText: &text, Territories: &ids}}}
	desired := &config.State{Spec: config.StateSpec{AppEULA: &config.AppEULASpec{AgreementText: &wantText}}}
	var changes []Change
	diffRights(desired, live, &changes)
	if len(changes) != 1 || changes[0].Path != "/spec/appEula" || changes[0].Op != OpUpdate {
		t.Fatalf("changes=%+v", changes)
	}
	merged, ok := changes[0].To.(config.AppEULASpec)
	if !ok || merged.AgreementText == nil || *merged.AgreementText != wantText || merged.Territories == nil || !reflect.DeepEqual(*merged.Territories, ids) {
		t.Fatalf("merged=%+v", changes[0].To)
	}
	changes = nil
	diffRights(&config.State{Spec: config.StateSpec{AppEULA: &config.AppEULASpec{Territories: &ids}}}, live, &changes)
	if len(changes) != 0 {
		t.Fatalf("same territory set: %+v", changes)
	}
}
