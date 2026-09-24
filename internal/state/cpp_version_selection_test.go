package state

import (
	"github.com/ul0gic/flightline/internal/asc"
	"testing"
)

func TestBUG077_CPPVersionSelection(t *testing.T) {
	row := func(id, version, status string) asc.Resource[asc.AppCustomProductPageVersionAttributes] {
		return asc.Resource[asc.AppCustomProductPageVersionAttributes]{ID: id, Attributes: asc.AppCustomProductPageVersionAttributes{Version: version, State: status}}
	}
	for _, tc := range []struct {
		name string
		rows []asc.Resource[asc.AppCustomProductPageVersionAttributes]
		want string
	}{
		{"numeric boundary", []asc.Resource[asc.AppCustomProductPageVersionAttributes]{row("nine", "9", "REPLACED_WITH_NEW_VERSION"), row("ten", "10", "APPROVED")}, "ten"},
		{"draft matches writer", []asc.Resource[asc.AppCustomProductPageVersionAttributes]{row("published", "10", "APPROVED"), row("draft", "9", "PREPARE_FOR_SUBMISSION")}, "draft"},
		{"ambiguous draft", []asc.Resource[asc.AppCustomProductPageVersionAttributes]{row("one", "1", "REJECTED"), row("two", "2", "PREPARE_FOR_SUBMISSION")}, ""},
		{"unknown ordering", []asc.Resource[asc.AppCustomProductPageVersionAttributes]{row("one", "unknown", "APPROVED"), row("two", "2", "APPROVED")}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := latestCPPVersionID(tc.rows)
			if tc.want == "" {
				if err == nil {
					t.Fatal("expected ambiguous/unknown target error")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got=%s err=%v", got, err)
			}
		})
	}
}
