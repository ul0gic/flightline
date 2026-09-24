package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func ValidateAccessibilityChange(ch plan.Change) error {
	const prefix = "/spec/accessibilityDeclarations/families/"
	if !strings.HasPrefix(ch.Path, prefix) {
		return errUnmapped(ch)
	}
	family := strings.TrimPrefix(ch.Path, prefix)
	if !asc.ValidAccessibilityFamily(family) {
		return fmt.Errorf("accessibility declaration: unsupported device family %q", family)
	}
	if ch.Op != plan.OpCreate && ch.Op != plan.OpUpdate {
		return fmt.Errorf("accessibility declaration: unsupported %s operation", ch.Op)
	}
	target, err := accessibilityChangeTarget(ch.To)
	if err != nil {
		return err
	}
	if target.State != nil {
		return errors.New("accessibility declaration state is observed and cannot be written")
	}
	if !config.AccessibilityHasAnswers(target) {
		return errors.New("accessibility declaration requires at least one explicit support answer")
	}
	if ch.Op == plan.OpUpdate {
		from, err := accessibilityChangeTarget(ch.From)
		if err != nil || from.State == nil || *from.State != "DRAFT" {
			return errors.New("accessibility declaration update requires an observed DRAFT")
		}
	}
	return nil
}

func applyAccessibilityDeclaration(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	if err := ValidateAccessibilityChange(ch); err != nil {
		return err
	}
	family := strings.TrimPrefix(ch.Path, "/spec/accessibilityDeclarations/families/")
	target, err := accessibilityChangeTarget(ch.To)
	if err != nil {
		return err
	}
	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}
	declarations, err := asc.ListAccessibilityDeclarations(ctx, c, appID)
	if err != nil {
		return err
	}
	selected, err := selectAccessibilityDeclarations(declarations)
	if err != nil {
		return err
	}
	current, exists := selected[family]
	if ch.Op == plan.OpCreate {
		if exists {
			return fmt.Errorf("accessibility declaration %s appeared since planning; replan", family)
		}
		attributes, err := accessibilityAttributes(target)
		if err != nil {
			return err
		}
		attributes.DeviceFamily = family
		_, err = asc.CreateAccessibilityDeclaration(ctx, c, appID, attributes)
		return err
	}
	if !exists || current.State != "DRAFT" {
		return fmt.Errorf("accessibility declaration %s is no longer an editable DRAFT; replan", family)
	}
	before, err := accessibilityChangeTarget(ch.From)
	if err != nil {
		return err
	}
	observed, err := accessibilityDeclarationSpec(current.AccessibilityDeclarationAttributes)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, observed) {
		return fmt.Errorf("accessibility declaration %s changed since planning; replan", family)
	}
	attributes, err := accessibilityAttributes(target)
	if err != nil {
		return err
	}
	_, err = asc.UpdateAccessibilityDeclaration(ctx, c, current.ID, attributes)
	return err
}

func accessibilityChangeTarget(value any) (config.AccessibilityDeclarationSpec, error) {
	declaration, ok := value.(config.AccessibilityDeclarationSpec)
	if !ok {
		return config.AccessibilityDeclarationSpec{}, fmt.Errorf("expected AccessibilityDeclarationSpec, got %T", value)
	}
	return declaration, nil
}

func accessibilityAttributes(spec config.AccessibilityDeclarationSpec) (asc.AccessibilityDeclarationAttributes, error) {
	buf, err := json.Marshal(spec)
	if err != nil {
		return asc.AccessibilityDeclarationAttributes{}, err
	}
	var attrs asc.AccessibilityDeclarationAttributes
	if err := json.Unmarshal(buf, &attrs); err != nil {
		return asc.AccessibilityDeclarationAttributes{}, err
	}
	return attrs, nil
}
