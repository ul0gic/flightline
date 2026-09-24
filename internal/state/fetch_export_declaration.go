package state

import (
	"context"
	"fmt"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func fetchExportDeclaration(ctx context.Context, c *asc.Client, buildID string) (*config.ExportComplianceDeclaration, error) {
	declaration, err := asc.GetBuildEncryptionDeclaration(ctx, c, buildID)
	if err != nil {
		if optionalMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if declaration == nil {
		return nil, nil
	}
	attrs := declaration.Attributes
	if attrs.ContainsProprietaryCryptography == nil || attrs.ContainsThirdPartyCryptography == nil || attrs.AvailableOnFrenchStore == nil {
		return nil, fmt.Errorf("build %s declaration %s response omits required create attributes", buildID, declaration.ID)
	}
	appDescription := attrs.AppDescription
	proprietary := *attrs.ContainsProprietaryCryptography
	thirdParty := *attrs.ContainsThirdPartyCryptography
	frenchStore := *attrs.AvailableOnFrenchStore
	return &config.ExportComplianceDeclaration{
		AppDescription:                  &appDescription,
		ContainsProprietaryCryptography: &proprietary,
		ContainsThirdPartyCryptography:  &thirdParty,
		AvailableOnFrenchStore:          &frenchStore,
	}, nil
}
