package state

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// applyEncryptionDeclaration creates or reuses a matching declaration, then associates it with the current version build.
func applyEncryptionDeclaration(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if ch.Path != "/spec/exportCompliance/declaration" {
		return fmt.Errorf("apply exportCompliance.declaration: unexpected path %s", ch.Path)
	}
	attrs, err := declarationCreateAttributes(ch.To)
	if err != nil {
		return fmt.Errorf("apply exportCompliance.declaration: %w", err)
	}
	appID, buildID, err := declarationTargetBuild(ctx, c, actx)
	if err != nil {
		return err
	}
	matched, err := currentDeclarationMatches(ctx, c, buildID, attrs)
	if err != nil {
		return err
	}
	if matched {
		return nil
	}
	declarationID, err := findOrCreateDeclaration(ctx, c, appID, attrs)
	if err != nil {
		return fmt.Errorf("apply exportCompliance.declaration: %w", err)
	}
	if err := asc.SetBuildEncryptionDeclaration(ctx, c, buildID, declarationID); err != nil {
		return fmt.Errorf("apply exportCompliance.declaration: associate declaration with build: %w", err)
	}
	return nil
}

func declarationTargetBuild(ctx context.Context, c *asc.Client, actx ApplyContext) (appID, buildID string, err error) {
	appID, err = resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return "", "", err
	}
	platform := actx.Platform
	if platform == "" {
		platform = "IOS"
	}
	_, versionID, err := fetchVersion(ctx, c, appID, actx.Version, platform)
	if err != nil {
		return "", "", fmt.Errorf("apply exportCompliance.declaration: resolve version: %w", err)
	}
	buildID, _, err = fetchVersionBuildEncryption(ctx, c, versionID)
	if err != nil {
		return "", "", fmt.Errorf("apply exportCompliance.declaration: read version build: %w", err)
	}
	if buildID == "" {
		return "", "", errors.New("apply exportCompliance.declaration: version has no attached build; apply spec.build.number first")
	}
	return appID, buildID, nil
}

func currentDeclarationMatches(ctx context.Context, c *asc.Client, buildID string, attrs asc.AppEncryptionDeclarationCreateAttributes) (bool, error) {
	current, err := asc.GetBuildEncryptionDeclaration(ctx, c, buildID)
	if err == nil {
		return current != nil && declarationAttributesMatch(attrs, current.Attributes), nil
	}
	if optionalMissing(err) {
		return false, nil
	}
	return false, fmt.Errorf("apply exportCompliance.declaration: read build declaration: %w", err)
}

func findOrCreateDeclaration(ctx context.Context, c *asc.Client, appID string, attrs asc.AppEncryptionDeclarationCreateAttributes) (string, error) {
	declarationID, err := reusableDeclarationID(ctx, c, appID, attrs)
	if err != nil {
		return "", err
	}
	if declarationID != "" {
		return declarationID, nil
	}
	created, err := asc.CreateAppEncryptionDeclaration(ctx, c, appID, attrs)
	if err != nil {
		return "", fmt.Errorf("create declaration: %w", err)
	}
	return created.ID, nil
}

func declarationCreateAttributes(value any) (asc.AppEncryptionDeclarationCreateAttributes, error) {
	declaration, ok := value.(config.ExportComplianceDeclaration)
	if !ok {
		if ptr, ptrOK := value.(*config.ExportComplianceDeclaration); ptrOK && ptr != nil {
			declaration = *ptr
			ok = true
		}
	}
	if !ok {
		return asc.AppEncryptionDeclarationCreateAttributes{}, fmt.Errorf("expected ExportComplianceDeclaration, got %T", value)
	}
	if declaration.AppDescription == nil || declaration.ContainsProprietaryCryptography == nil ||
		declaration.ContainsThirdPartyCryptography == nil || declaration.AvailableOnFrenchStore == nil {
		return asc.AppEncryptionDeclarationCreateAttributes{}, errors.New("appDescription, containsProprietaryCryptography, containsThirdPartyCryptography, and availableOnFrenchStore are all required")
	}
	if declaration.UsesEncryption != nil || declaration.Exempt != nil || declaration.ECCN != nil ||
		declaration.DocumentName != nil || declaration.DocumentURL != nil {
		return asc.AppEncryptionDeclarationCreateAttributes{}, errors.New("legacy declaration attributes are unsupported; documents and Apple-assigned classification use separate lifecycles")
	}
	return asc.AppEncryptionDeclarationCreateAttributes{
		AppDescription:                  *declaration.AppDescription,
		ContainsProprietaryCryptography: *declaration.ContainsProprietaryCryptography,
		ContainsThirdPartyCryptography:  *declaration.ContainsThirdPartyCryptography,
		AvailableOnFrenchStore:          *declaration.AvailableOnFrenchStore,
	}, nil
}

func reusableDeclarationID(ctx context.Context, c *asc.Client, appID string, attrs asc.AppEncryptionDeclarationCreateAttributes) (string, error) {
	declarations, err := asc.ListAppEncryptionDeclarations(ctx, c, appID)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		if declaration.ID != "" && declarationReusable(declaration.Attributes) && declarationAttributesMatch(attrs, declaration.Attributes) {
			ids = append(ids, declaration.ID)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	sort.Strings(ids)
	return ids[0], nil
}

func declarationAttributesMatch(want asc.AppEncryptionDeclarationCreateAttributes, got asc.AppEncryptionDeclarationAttributes) bool {
	return want.AppDescription == got.AppDescription &&
		got.ContainsProprietaryCryptography != nil && want.ContainsProprietaryCryptography == *got.ContainsProprietaryCryptography &&
		got.ContainsThirdPartyCryptography != nil && want.ContainsThirdPartyCryptography == *got.ContainsThirdPartyCryptography &&
		got.AvailableOnFrenchStore != nil && want.AvailableOnFrenchStore == *got.AvailableOnFrenchStore
}

func declarationReusable(attrs asc.AppEncryptionDeclarationAttributes) bool {
	switch attrs.AppEncryptionDeclarationState {
	case asc.EncryptionDeclarationStateCreated, asc.EncryptionDeclarationStateInReview, asc.EncryptionDeclarationStateApproved:
		return true
	default:
		return false
	}
}
