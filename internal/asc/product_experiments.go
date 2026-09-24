package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// ProductExperiment is a v2 App Store version experiment. Apple creates this
// resource against an app, not against a caller-selected version.
type ProductExperiment struct {
	ID                string `json:"id"`
	AppID             string `json:"appId"`
	Name              string `json:"name"`
	Platform          string `json:"platform"`
	TrafficProportion int    `json:"trafficProportion"`
	State             string `json:"state"`
	ReviewRequired    bool   `json:"reviewRequired"`
	StartDate         string `json:"startDate,omitempty"`
	EndDate           string `json:"endDate,omitempty"`
	LatestControlID   string `json:"latestControlVersionId,omitempty"`
}

type productExperimentAttributes struct {
	Name              string `json:"name"`
	Platform          string `json:"platform"`
	TrafficProportion int    `json:"trafficProportion"`
	State             string `json:"state"`
	ReviewRequired    bool   `json:"reviewRequired"`
	StartDate         string `json:"startDate"`
	EndDate           string `json:"endDate"`
}

func experimentRelationshipID(rels map[string]Relationship, key, typ string) (string, error) {
	rel, ok := rels[key]
	if !ok || len(rel.Data) == 0 || string(rel.Data) == "null" {
		return "", fmt.Errorf("missing %s relationship", key)
	}
	var ref struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(rel.Data, &ref); err != nil || ref.Type != typ || ref.ID == "" {
		return "", fmt.Errorf("invalid %s relationship", key)
	}
	return ref.ID, nil
}

func projectProductExperiment(row Resource[productExperimentAttributes], expectedApp string) (ProductExperiment, error) {
	if row.Type != "appStoreVersionExperiments" || row.ID == "" || row.Attributes.Name == "" || row.Attributes.Platform == "" || row.Attributes.State == "" {
		return ProductExperiment{}, errors.New("experiment response has incomplete identity or attributes")
	}
	appID, err := experimentRelationshipID(row.Relationships, "app", "apps")
	if err != nil {
		return ProductExperiment{}, err
	}
	if expectedApp != "" && appID != expectedApp {
		return ProductExperiment{}, errors.New("experiment belongs to another app")
	}
	latest := ""
	if rel, ok := row.Relationships["latestControlVersion"]; ok && len(rel.Data) > 0 && string(rel.Data) != "null" {
		latest, err = experimentRelationshipID(row.Relationships, "latestControlVersion", "appStoreVersions")
		if err != nil {
			return ProductExperiment{}, err
		}
	}
	a := row.Attributes
	return ProductExperiment{ID: row.ID, AppID: appID, Name: a.Name, Platform: a.Platform, TrafficProportion: a.TrafficProportion, State: a.State, ReviewRequired: a.ReviewRequired, StartDate: a.StartDate, EndDate: a.EndDate, LatestControlID: latest}, nil
}

func ListVersionExperiments(ctx context.Context, c *Client, appID, versionID string) ([]ProductExperiment, error) {
	if appID == "" || versionID == "" {
		return nil, errors.New("app and version IDs are required")
	}
	out := make([]ProductExperiment, 0)
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/appStoreVersionExperimentsV2"
	for page, err := range Pages[productExperimentAttributes](ctx, c, path, url.Values{"limit": {"200"}, "include": {"app"}}) {
		if err != nil {
			return nil, fmt.Errorf("list version experiments: %w", err)
		}
		for _, row := range page.Data {
			exp, err := projectProductExperiment(row, appID)
			if err != nil {
				return nil, err
			}
			out = append(out, exp)
		}
	}
	return out, nil
}

func ListAppExperiments(ctx context.Context, c *Client, appID string) ([]ProductExperiment, error) {
	if appID == "" {
		return nil, errors.New("app ID is required")
	}
	out := make([]ProductExperiment, 0)
	for page, err := range Pages[productExperimentAttributes](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appStoreVersionExperimentsV2", url.Values{"limit": {"200"}, "include": {"app"}}) {
		if err != nil {
			return nil, fmt.Errorf("list app experiments: %w", err)
		}
		for _, row := range page.Data {
			item, err := projectProductExperiment(row, appID)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func GetProductExperiment(ctx context.Context, c *Client, appID, experimentID string) (ProductExperiment, error) {
	if appID == "" || experimentID == "" {
		return ProductExperiment{}, errors.New("app and experiment IDs are required")
	}
	resp, err := Get[Single[productExperimentAttributes]](ctx, c, "/v2/appStoreVersionExperiments/"+url.PathEscape(experimentID), url.Values{"include": {"app,latestControlVersion"}})
	if err != nil {
		return ProductExperiment{}, fmt.Errorf("read experiment: %w", err)
	}
	exp, err := projectProductExperiment(resp.Data, appID)
	if err != nil {
		return ProductExperiment{}, err
	}
	if exp.ID != experimentID {
		return ProductExperiment{}, errors.New("experiment response ID mismatch")
	}
	return exp, nil
}

func CreateProductExperiment(ctx context.Context, c *Client, appID, name, platform string, traffic int) (ProductExperiment, error) {
	if appID == "" || name == "" || platform == "" || traffic < 1 || traffic > 100 {
		return ProductExperiment{}, errors.New("app, name, platform, and traffic 1..100 required")
	}
	body := map[string]any{"data": map[string]any{"type": "appStoreVersionExperiments", "attributes": map[string]any{"name": name, "platform": platform, "trafficProportion": traffic}, "relationships": map[string]any{"app": map[string]any{"data": map[string]any{"type": "apps", "id": appID}}}}}
	resp, err := Post[Single[productExperimentAttributes]](ctx, c, "/v2/appStoreVersionExperiments", nil, body)
	if err != nil {
		return ProductExperiment{}, fmt.Errorf("create experiment (inspect live before retry): %w", err)
	}
	exp, err := projectProductExperiment(resp.Data, appID)
	if err != nil {
		return ProductExperiment{}, err
	}
	if exp.Name != name || exp.Platform != platform || exp.TrafficProportion != traffic {
		return ProductExperiment{}, errors.New("created experiment response differs; inspect live before retry")
	}
	return exp, nil
}

// UpdateProductExperiment only changes mutable draft metadata. A fresh read
// prevents changing a terminal or concurrently changed experiment.
func UpdateProductExperiment(ctx context.Context, c *Client, appID, experimentID string, name *string, traffic *int) (ProductExperiment, bool, error) {
	if name == nil && traffic == nil {
		return ProductExperiment{}, false, errors.New("name or traffic is required")
	}
	current, err := GetProductExperiment(ctx, c, appID, experimentID)
	if err != nil {
		return ProductExperiment{}, false, err
	}
	if !CanEditProductExperiment(current) {
		return ProductExperiment{}, false, errors.New("experiment is no longer editable")
	}
	attrs, err := productExperimentUpdateAttrs(current, name, traffic)
	if err != nil {
		return ProductExperiment{}, false, err
	}
	if len(attrs) == 0 {
		return current, false, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appStoreVersionExperiments", "id": experimentID, "attributes": attrs}}
	resp, err := Patch[Single[productExperimentAttributes]](ctx, c, "/v2/appStoreVersionExperiments/"+url.PathEscape(experimentID), nil, body)
	if err != nil {
		return ProductExperiment{}, false, fmt.Errorf("update experiment (inspect live before retry): %w", err)
	}
	updated, err := projectProductExperiment(resp.Data, appID)
	if err != nil {
		return ProductExperiment{}, false, err
	}
	if updated.ID != experimentID {
		return ProductExperiment{}, false, errors.New("updated experiment ID mismatch; inspect live")
	}
	if name != nil && updated.Name != *name {
		return ProductExperiment{}, false, errors.New("experiment name update not confirmed; inspect live")
	}
	if traffic != nil && updated.TrafficProportion != *traffic {
		return ProductExperiment{}, false, errors.New("experiment traffic update not confirmed; inspect live")
	}
	return updated, true, nil
}

func CanEditProductExperiment(current ProductExperiment) bool {
	return current.StartDate == "" && current.EndDate == "" && current.State == "PREPARE_FOR_SUBMISSION"
}

func productExperimentUpdateAttrs(current ProductExperiment, name *string, traffic *int) (map[string]any, error) {
	attrs := map[string]any{}
	if name != nil {
		if *name == "" {
			return nil, errors.New("name is required")
		}
		if *name != current.Name {
			attrs["name"] = *name
		}
	}
	if traffic != nil {
		if *traffic < 1 || *traffic > 100 {
			return nil, errors.New("traffic must be 1..100")
		}
		if *traffic != current.TrafficProportion {
			attrs["trafficProportion"] = *traffic
		}
	}
	return attrs, nil
}

func SetProductExperimentStarted(ctx context.Context, c *Client, appID, experimentID string, started bool) (ProductExperiment, bool, error) {
	current, err := GetProductExperiment(ctx, c, appID, experimentID)
	if err != nil {
		return ProductExperiment{}, false, err
	}
	noop, err := checkExperimentLifecycle(current, started)
	if err != nil {
		return ProductExperiment{}, false, err
	}
	if noop {
		return current, false, nil
	}
	body := map[string]any{"data": map[string]any{"type": "appStoreVersionExperiments", "id": experimentID, "attributes": map[string]any{"started": started}}}
	resp, err := Patch[Single[productExperimentAttributes]](ctx, c, "/v2/appStoreVersionExperiments/"+url.PathEscape(experimentID), nil, body)
	if err != nil {
		return ProductExperiment{}, false, fmt.Errorf("change experiment lifecycle (inspect live before retry): %w", err)
	}
	updated, err := projectProductExperiment(resp.Data, appID)
	if err != nil {
		return ProductExperiment{}, false, err
	}
	if updated.ID != experimentID {
		return ProductExperiment{}, false, errors.New("experiment lifecycle response ID mismatch; inspect live")
	}
	return updated, true, nil
}

func checkExperimentLifecycle(current ProductExperiment, started bool) (bool, error) {
	terminal := current.State == "STOPPED" || current.State == "COMPLETED" || current.EndDate != ""
	if started {
		if terminal {
			return false, errors.New("stopped or completed experiment cannot restart")
		}
		if current.StartDate != "" {
			return true, nil
		}
		if current.State != "APPROVED" || current.ReviewRequired {
			return false, errors.New("experiment is not approved for starting")
		}
		return false, nil
	}
	if terminal {
		return true, nil
	}
	if current.StartDate == "" {
		return false, errors.New("experiment has not started")
	}
	return false, nil
}

func DeleteProductExperiment(ctx context.Context, c *Client, appID, experimentID string) error {
	current, err := GetProductExperiment(ctx, c, appID, experimentID)
	if err != nil {
		return err
	}
	if !CanEditProductExperiment(current) {
		return errors.New("experiment lifecycle is not deletable")
	}
	if err := c.Delete(ctx, "/v2/appStoreVersionExperiments/"+url.PathEscape(experimentID), nil); err != nil {
		return fmt.Errorf("delete experiment (inspect live before retry): %w", err)
	}
	return nil
}

// VerifyExperimentSubmissionProposal rechecks the selected version relationship
// and review readiness. Call again immediately before attaching an item.
func VerifyExperimentSubmissionProposal(ctx context.Context, c *Client, appID, versionID, experimentID string) (SubmissionItemProposal, error) {
	listed, err := ListVersionExperiments(ctx, c, appID, versionID)
	if err != nil {
		return SubmissionItemProposal{}, err
	}
	count := 0
	for i := range listed {
		if listed[i].ID == experimentID {
			count++
		}
	}
	if count != 1 {
		return SubmissionItemProposal{}, errors.New("experiment is not uniquely attached to the selected version")
	}
	current, err := GetProductExperiment(ctx, c, appID, experimentID)
	if err != nil {
		return SubmissionItemProposal{}, err
	}
	if current.State != "READY_FOR_REVIEW" || current.StartDate != "" || current.EndDate != "" {
		return SubmissionItemProposal{}, errors.New("experiment is not ready for review")
	}
	proposal := SubmissionItemProposal{Relationship: "appStoreVersionExperimentV2", Type: "appStoreVersionExperiments", ID: experimentID}
	if err := proposal.Validate(); err != nil {
		return SubmissionItemProposal{}, err
	}
	return proposal, nil
}
