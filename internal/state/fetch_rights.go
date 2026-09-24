package state

import (
	"context"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func FetchContentRights(ctx context.Context, c *asc.Client, appID string) (*string, error) {
	observed, err := asc.ReadContentRights(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	return observed.Declaration, nil
}

func FetchAppEULA(ctx context.Context, c *asc.Client, appID string) (*config.AppEULASpec, error) {
	observed, err := asc.ReadAppEULA(ctx, c, appID)
	if err != nil || observed == nil {
		return nil, err
	}
	return appEULASpecFromObserved(observed), nil
}

func appEULASpecFromObserved(observed *asc.AppEULA) *config.AppEULASpec {
	text := observed.AgreementText
	territories := observed.Territories
	return &config.AppEULASpec{AgreementText: &text, Territories: &territories}
}
