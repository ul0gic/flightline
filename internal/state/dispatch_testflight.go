package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

// applyTestFlightField creates a beta group, PATCHes a field, or adds/removes a tester by sub-path.
func applyTestFlightField(ctx context.Context, c *asc.Client, actx ApplyContext, ch plan.Change) error {
	rest := strings.TrimPrefix(ch.Path, "/spec/testflight/groups/")
	parts := strings.SplitN(rest, "/", 2)
	groupName, err := unescapeTestFlightPathSegment(parts[0])
	if err != nil {
		return fmt.Errorf("apply testflight: malformed group path %s: %w", ch.Path, err)
	}
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}
	if subPath == "isInternal" {
		return fmt.Errorf("apply testflight.%s.isInternal: group kind is immutable after creation", groupName)
	}

	appID, err := resolveAppID(ctx, c, actx.BundleID)
	if err != nil {
		return err
	}

	if subPath == "" {
		return createTestFlightGroup(ctx, c, appID, groupName, ch.To)
	}

	groupID, err := resolveBetaGroupByName(ctx, c, appID, groupName)
	if err != nil {
		return err
	}

	if strings.HasPrefix(subPath, "testers/") {
		return applyTestFlightTester(ctx, c, groupID, groupName, subPath, ch.Op)
	}

	return patchTestFlightGroup(ctx, c, groupID, groupName, subPath, ch.To)
}

func createTestFlightGroup(ctx context.Context, c *asc.Client, appID, groupName string, value any) error {
	group, err := decodeTestFlightGroup(value)
	if err != nil {
		return fmt.Errorf("apply testflight.create %s: %w", groupName, err)
	}
	if group.IsInternal == nil {
		return fmt.Errorf("apply testflight.create %s: isInternal is required", groupName)
	}
	body := map[string]any{"data": map[string]any{
		"type":       "betaGroups",
		"attributes": testFlightGroupCreateAttributes(groupName, group),
		"relationships": map[string]any{
			"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}},
		},
	}}
	if _, err := asc.Post[asc.Single[asc.BetaGroupAttributes]](ctx, c, "/v1/betaGroups", nil, body); err != nil {
		return fmt.Errorf("apply testflight.create %s: %w", groupName, err)
	}
	return nil
}

func applyTestFlightTester(ctx context.Context, c *asc.Client, groupID, groupName, subPath string, op plan.Op) error {
	email, err := unescapeTestFlightPathSegment(strings.TrimPrefix(subPath, "testers/"))
	if err != nil {
		return fmt.Errorf("apply testflight.%s: malformed tester path: %w", groupName, err)
	}
	switch op {
	case plan.OpCreate:
		return createTester(ctx, c, groupID, email)
	case plan.OpDelete:
		return removeTester(ctx, c, groupID, email)
	default:
		return fmt.Errorf("apply testflight.%s.testers/%s: unsupported op %s", groupName, email, op)
	}
}

func unescapeTestFlightPathSegment(segment string) (string, error) {
	var out strings.Builder
	out.Grow(len(segment))
	for index := 0; index < len(segment); index++ {
		if segment[index] != '~' {
			out.WriteByte(segment[index])
			continue
		}
		if index+1 == len(segment) || (segment[index+1] != '0' && segment[index+1] != '1') {
			return "", errors.New("invalid JSON Pointer escape")
		}
		if segment[index+1] == '0' {
			out.WriteByte('~')
		} else {
			out.WriteByte('/')
		}
		index++
	}
	return out.String(), nil
}

func patchTestFlightGroup(ctx context.Context, c *asc.Client, groupID, groupName, subPath string, value any) error {
	if subPath != "publicLink" && subPath != "publicLinkLimit" {
		return fmt.Errorf("apply testflight.%s.%s: unsupported writable field", groupName, subPath)
	}
	// Group-attribute PATCH.
	wire := subPath
	if subPath == "publicLink" {
		wire = "publicLinkEnabled"
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "betaGroups",
			"id":         groupID,
			"attributes": map[string]any{wire: value},
		},
	}
	if _, err := asc.Patch[asc.Single[asc.BetaGroupAttributes]](ctx, c, "/v1/betaGroups/"+groupID, nil, body); err != nil {
		return fmt.Errorf("apply testflight.%s.%s: %w", groupName, subPath, err)
	}
	return nil
}

func decodeTestFlightGroup(value any) (config.TestFlightGroup, error) {
	buf, err := json.Marshal(value)
	if err != nil {
		return config.TestFlightGroup{}, err
	}
	var group config.TestFlightGroup
	if err := json.Unmarshal(buf, &group); err != nil {
		return config.TestFlightGroup{}, err
	}
	return group, nil
}

func testFlightGroupCreateAttributes(name string, group config.TestFlightGroup) map[string]any {
	attributes := map[string]any{"name": name, "isInternalGroup": *group.IsInternal}
	if group.PublicLink != nil {
		attributes["publicLinkEnabled"] = *group.PublicLink
	}
	if group.PublicLinkLimit != nil {
		attributes["publicLinkLimit"] = *group.PublicLinkLimit
	}
	return attributes
}

func resolveBetaGroupByName(ctx context.Context, c *asc.Client, appID, name string) (string, error) {
	q := url.Values{"filter[name]": {name}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.BetaGroupAttributes]](
		ctx, c, "/v1/apps/"+appID+"/betaGroups", q,
	)
	if err != nil {
		return "", fmt.Errorf("resolve betaGroup %s: %w", name, err)
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("betaGroup %s not found on app", name)
	}
	return page.Data[0].ID, nil
}

func createTester(ctx context.Context, c *asc.Client, groupID, email string) error {
	q := url.Values{"filter[email]": {email}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.BetaTesterAttributes]](ctx, c, "/v1/betaTesters", q)
	if err != nil {
		return fmt.Errorf("lookup tester %s: %w", email, err)
	}
	var testerID string
	if len(page.Data) > 0 {
		testerID = page.Data[0].ID
	} else {
		body := map[string]any{
			"data": map[string]any{
				"type":       "betaTesters",
				"attributes": map[string]any{"email": email},
				"relationships": map[string]any{
					"betaGroups": map[string]any{
						"data": []any{map[string]any{"type": "betaGroups", "id": groupID}},
					},
				},
			},
		}
		resp, err := asc.Post[asc.Single[asc.BetaTesterAttributes]](ctx, c, "/v1/betaTesters", nil, body)
		if err != nil {
			return fmt.Errorf("create tester %s: %w", email, err)
		}
		return relationshipNoOpIfPresent(ctx, c, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", resp.Data.ID)
	}
	return relationshipNoOpIfPresent(ctx, c, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", testerID)
}

func removeTester(ctx context.Context, c *asc.Client, groupID, email string) error {
	q := url.Values{"filter[email]": {email}, "limit": {"1"}}
	page, err := asc.Get[asc.Collection[asc.BetaTesterAttributes]](ctx, c, "/v1/betaTesters", q)
	if err != nil {
		return fmt.Errorf("lookup tester %s: %w", email, err)
	}
	if len(page.Data) == 0 {
		return nil // nothing to remove
	}
	body := map[string]any{
		"data": []any{map[string]any{"type": "betaTesters", "id": page.Data[0].ID}},
	}
	if err := c.DeleteWithBody(ctx, "/v1/betaGroups/"+groupID+"/relationships/betaTesters", nil, body); err != nil {
		return fmt.Errorf("remove tester %s: %w", email, err)
	}
	return nil
}

// relationshipNoOpIfPresent POSTs a to-many relationship link; JSON:API treats duplicate adds as idempotent.
func relationshipNoOpIfPresent(ctx context.Context, c *asc.Client, path, testerID string) error {
	body := map[string]any{
		"data": []any{map[string]any{"type": "betaTesters", "id": testerID}},
	}
	if _, err := asc.Post[map[string]any](ctx, c, path, nil, body); err != nil {
		return fmt.Errorf("add tester to group: %w", err)
	}
	return nil
}
