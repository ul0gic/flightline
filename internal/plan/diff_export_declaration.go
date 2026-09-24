package plan

import (
	"reflect"

	"github.com/ul0gic/flightline/internal/config"
)

func diffExportCompliance(d, l *config.ExportComplianceSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.ExportComplianceSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "exportCompliance", "/spec/exportCompliance/usesNonExemptEncryption", d.UsesNonExemptEncryption, live.UsesNonExemptEncryption)
	if d.Declaration != nil && !reflect.DeepEqual(d.Declaration, live.Declaration) {
		var from any
		if live.Declaration != nil {
			from = *live.Declaration
		}
		*out = append(*out, Change{
			Op:       OpCreate,
			Resource: "exportCompliance",
			Path:     "/spec/exportCompliance/declaration",
			From:     from,
			To:       *d.Declaration,
			Hint:     "create and associate export encryption declaration",
		})
	}
}
