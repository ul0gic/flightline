// asc_helpers.go — small ASC HTTP shims shared by multiple rules.
//
// The first ASC helper a rule needs lives next to the rule. The moment a
// second rule needs the same shape, it moves here so the duplication doesn't
// fork. Anything still rule-local at the gate review is fair game to lift.

package lint

import (
	"fmt"
	"net/url"

	"github.com/ul0gic/skipper/internal/asc"
)

// resolveVersionIDOnApp looks up an App Store version by versionString
// (and platform=IOS, the default). Returns the resource ID. If
// versionStr is empty the latest editable version is picked.
func resolveVersionIDOnApp(ctx CheckContext, appID, versionStr string) (string, error) {
	q := url.Values{
		"filter[platform]": {"IOS"},
		"limit":            {"50"},
	}
	if versionStr != "" {
		q.Set("filter[versionString]", versionStr)
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		ctx.Ctx, ctx.Client, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("no version %q on app", versionStr)
	}
	return page.Data[0].ID, nil
}
