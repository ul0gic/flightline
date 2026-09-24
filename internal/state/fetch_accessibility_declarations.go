package state

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
)

func FetchAccessibilityDeclarations(ctx context.Context, c *asc.Client, appID string) (*config.AccessibilityDeclarationsSpec, error) {
	declarations, err := asc.ListAccessibilityDeclarations(ctx, c, appID)
	if err != nil {
		return nil, err
	}
	if len(declarations) == 0 {
		return nil, nil
	}
	selected, err := selectAccessibilityDeclarations(declarations)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, nil
	}
	spec := &config.AccessibilityDeclarationsSpec{Families: make(map[string]config.AccessibilityDeclarationSpec, len(selected))}
	for family, declaration := range selected {
		value, err := accessibilityDeclarationSpec(declaration.AccessibilityDeclarationAttributes)
		if err != nil {
			return nil, fmt.Errorf("decode accessibility declaration %s: %w", declaration.ID, err)
		}
		spec.Families[family] = value
	}
	return spec, nil
}

func selectAccessibilityDeclarations(declarations []asc.AccessibilityDeclaration) (map[string]asc.AccessibilityDeclaration, error) {
	selected := make(map[string]asc.AccessibilityDeclaration)
	seen := make(map[string]map[string]bool)
	for _, declaration := range declarations {
		if declaration.State == "REPLACED" {
			continue
		}
		if seen[declaration.DeviceFamily] == nil {
			seen[declaration.DeviceFamily] = make(map[string]bool)
		}
		if seen[declaration.DeviceFamily][declaration.State] {
			return nil, fmt.Errorf("multiple %s accessibility declarations for %s", declaration.State, declaration.DeviceFamily)
		}
		seen[declaration.DeviceFamily][declaration.State] = true
		if previous, ok := selected[declaration.DeviceFamily]; !ok || (previous.State == "PUBLISHED" && declaration.State == "DRAFT") {
			selected[declaration.DeviceFamily] = declaration
		}
	}
	return selected, nil
}

func accessibilityDeclarationSpec(attrs asc.AccessibilityDeclarationAttributes) (config.AccessibilityDeclarationSpec, error) {
	buf, err := json.Marshal(attrs)
	if err != nil {
		return config.AccessibilityDeclarationSpec{}, err
	}
	var spec config.AccessibilityDeclarationSpec
	if err := json.Unmarshal(buf, &spec); err != nil {
		return config.AccessibilityDeclarationSpec{}, err
	}
	return spec, nil
}
