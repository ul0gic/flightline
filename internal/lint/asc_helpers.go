package lint

import (
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
)

// resolveVersionIDOnApp looks up an App Store version by versionString (platform=IOS). Empty versionStr picks latest.
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
