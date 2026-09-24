package state

import (
	"context"
	"errors"
	"net/http"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

// FetchPhasedRelease reads a version's rollout without exposing the ASC resource ID.
func FetchPhasedRelease(ctx context.Context, c *asc.Client, versionID string) (*config.PhasedReleaseSpec, error) {
	release, err := asc.ReadAppStoreVersionPhasedRelease(ctx, c, versionID)
	if err != nil {
		var apiErr *asc.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	enabled := true
	return &config.PhasedReleaseSpec{Enabled: &enabled, State: phasedString(release.PhasedReleaseState), StartDate: phasedString(release.StartDate), TotalPauseDuration: clonePhasedInt(release.TotalPauseDuration), CurrentDayNumber: clonePhasedInt(release.CurrentDayNumber)}, nil
}

func phasedString(value string) *string {
	if value == "" {
		return nil
	}
	cloned := value
	return &cloned
}
