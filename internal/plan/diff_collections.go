package plan

import (
	"fmt"

	"github.com/ul0gic/flightline/internal/config"
)

func diffCategories(d, l *config.CategoriesSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := config.CategoriesSpec{}
	if l != nil {
		live = *l
	}
	emitIfDiff(out, "categories", "/spec/categories/primary", d.Primary, live.Primary)
	emitIfDiff(out, "categories", "/spec/categories/secondary", d.Secondary, live.Secondary)
	if d.PrimarySubcategories != nil && !equalStringSlices(d.PrimarySubcategories, live.PrimarySubcategories) {
		op := OpUpdate
		if len(live.PrimarySubcategories) == 0 {
			op = OpCreate
		}
		*out = append(*out, Change{
			Op: op, Resource: "categories", Path: "/spec/categories/primarySubcategories",
			From: live.PrimarySubcategories, To: d.PrimarySubcategories,
			Hint: fmt.Sprintf("primarySubcategories: %v -> %v", live.PrimarySubcategories, d.PrimarySubcategories),
		})
	}
	if d.SecondarySubcategories != nil && !equalStringSlices(d.SecondarySubcategories, live.SecondarySubcategories) {
		op := OpUpdate
		if len(live.SecondarySubcategories) == 0 {
			op = OpCreate
		}
		*out = append(*out, Change{
			Op: op, Resource: "categories", Path: "/spec/categories/secondarySubcategories",
			From: live.SecondarySubcategories, To: d.SecondarySubcategories,
			Hint: fmt.Sprintf("secondarySubcategories: %v -> %v", live.SecondarySubcategories, d.SecondarySubcategories),
		})
	}
}
