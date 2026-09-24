package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/lint"
	"github.com/ul0gic/flightline/internal/state"
)

func verifySubmissionItem(ctx context.Context, c *asc.Client, plan state.SubmissionPlan, item asc.SubmissionItemProposal) error {
	if err := item.Validate(); err != nil {
		return err
	}
	switch item.Relationship {
	case "appStoreVersion":
		_, err := submissionVersion(ctx, c, plan)
		return err
	case "inAppPurchaseVersion":
		return verifySubmissionIAP(ctx, c, plan.AppID, item.ID)
	case "appEvent":
		return asc.VerifyAppEventProposal(ctx, c, plan.AppID, item.ID)
	case "appStoreVersionExperimentV2":
		_, err := asc.VerifyExperimentSubmissionProposal(ctx, c, plan.AppID, plan.Version.ID, item.ID)
		return err
	default:
		return errors.New("unsupported submission proposal")
	}
}

func submissionVersion(ctx context.Context, c *asc.Client, plan state.SubmissionPlan) (asc.VersionAttributes, error) {
	var target asc.VersionAttributes
	found := 0
	q := url.Values{"limit": {"200"}, "filter[platform]": {plan.Platform}}
	for page, err := range asc.Pages[asc.VersionAttributes](ctx, c, "/v1/apps/"+url.PathEscape(plan.AppID)+"/appStoreVersions", q) {
		if err != nil {
			return target, err
		}
		for _, row := range page.Data {
			if row.ID != plan.Version.ID {
				continue
			}
			if row.Type != "appStoreVersions" || row.Attributes.Platform != plan.Platform || row.Attributes.VersionString == "" {
				return target, errors.New("submission version identity is incomplete or inconsistent")
			}
			target = row.Attributes
			found++
		}
	}
	if found != 1 {
		return target, fmt.Errorf("submission version has %d matches under selected app/platform", found)
	}
	status := target.AppVersionState
	if status == "" {
		status = target.AppStoreState
	}
	switch status {
	case "PREPARE_FOR_SUBMISSION", "READY_FOR_REVIEW", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED":
		return target, nil
	default:
		return target, fmt.Errorf("submission version state %q is not eligible for assembly", status)
	}
}

func verifySubmissionIAP(ctx context.Context, c *asc.Client, appID, versionID string) error {
	response, err := asc.Get[asc.Single[asc.IAPVersionAttributes]](ctx, c, "/v1/inAppPurchaseVersions/"+url.PathEscape(versionID), url.Values{"include": {"inAppPurchase"}})
	if err != nil {
		return err
	}
	row := response.Data
	if row.ID != versionID || row.Type != "inAppPurchaseVersions" || row.Attributes.State != "READY_FOR_REVIEW" {
		return errors.New("submission IAP version must have exact identity and READY_FOR_REVIEW state")
	}
	var parent struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(row.Relationships["inAppPurchase"].Data, &parent); err != nil {
		return fmt.Errorf("submission IAP parent: %w", err)
	}
	if parent.Type != "inAppPurchases" || parent.ID == "" {
		return errors.New("submission IAP version has no verified parent")
	}
	return verifySubmissionIAPParent(ctx, c, appID, parent.ID)
}

func verifySubmissionIAPParent(ctx context.Context, c *asc.Client, appID, parentID string) error {
	found := 0
	for page, err := range asc.Pages[asc.IAPAttributes](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/inAppPurchasesV2", url.Values{"limit": {"200"}}) {
		if err != nil {
			return err
		}
		for _, iap := range page.Data {
			if iap.ID == parentID {
				if iap.Type != "inAppPurchases" {
					return errors.New("submission IAP parent type mismatch")
				}
				if iap.Attributes.State != asc.IAPStateReadyToSubmit {
					return errors.New("submission IAP parent is not READY_TO_SUBMIT")
				}
				found++
			}
		}
	}
	if found != 1 {
		return fmt.Errorf("submission IAP parent has %d matches under selected app", found)
	}
	return nil
}

func preflightSubmission(ctx context.Context, c *asc.Client, plan state.SubmissionPlan, submissionID string) error {
	app, err := asc.Get[asc.Single[AppAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(plan.AppID), nil)
	if err != nil {
		return err
	}
	if app.Data.ID != plan.AppID || app.Data.Type != "apps" || app.Data.Attributes.BundleID == "" {
		return errors.New("submission preflight app identity mismatch")
	}
	version, err := submissionVersion(ctx, c, plan)
	if err != nil {
		return err
	}
	live, err := state.Fetch(ctx, c, app.Data.Attributes.BundleID, state.FetchOpts{Version: version.VersionString, Platform: plan.Platform})
	if err != nil {
		return fmt.Errorf("submission preflight fetch: %w", err)
	}
	diagnostics := lint.NewRunner(lint.All()).Run(lint.CheckContext{State: live, Client: c, BundleID: app.Data.Attributes.BundleID, Version: version.VersionString, Live: true, Ctx: ctx, ReviewSubmissionID: submissionID})
	diagnostics = mergeSchemaIntoLint(config.Validate("", live), diagnostics)
	var blocked []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == lint.SeverityError {
			blocked = append(blocked, diagnostic.RuleID+": "+diagnostic.Message)
		}
	}
	if len(blocked) > 0 {
		return fmt.Errorf("submission preflight blocked: %v", blocked)
	}
	return nil
}
