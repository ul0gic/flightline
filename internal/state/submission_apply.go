package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/ul0gic/flightline/internal/asc"
)

// SubmissionItemVerifier rechecks proposal ownership and review readiness at
// the moment an assembly action runs. It must not trust prior planning output.
type SubmissionItemVerifier func(context.Context, *asc.Client, SubmissionPlan, asc.SubmissionItemProposal) error

// SubmissionPreflight is invoked only after exact membership is freshly
// confirmed. It must return an error for every blocking diagnostic.
type SubmissionPreflight func(context.Context, *asc.Client, SubmissionPlan, string) error

// SubmissionApplyResult reports a completed assembly, never a final submit.
type SubmissionApplyResult struct {
	SubmissionID string
	Created      bool
	Attached     []asc.SubmissionItemProposal
}

// ApplySubmissionPlan creates or resumes one review submission and attaches
// exact membership. A transport failure is always an unconfirmed outcome: the
// function re-reads once and continues only when membership proves the write.
func ApplySubmissionPlan(ctx context.Context, c *asc.Client, plan SubmissionPlan, verify SubmissionItemVerifier, preflight SubmissionPreflight) (SubmissionApplyResult, error) {
	if verify == nil || preflight == nil {
		return SubmissionApplyResult{}, errors.New("submission: verifier and fresh preflight are required")
	}
	normalized, err := normalizeSubmissionPlan(plan)
	if err != nil {
		return SubmissionApplyResult{}, err
	}
	plan = normalized
	if err := verifySubmissionItems(ctx, c, plan, verify, ""); err != nil {
		return SubmissionApplyResult{}, err
	}
	submission, created, err := selectOrCreateSubmission(ctx, c, plan)
	if err != nil {
		return SubmissionApplyResult{}, err
	}
	result := SubmissionApplyResult{SubmissionID: submission.ID, Created: created}
	if err := verifyCurrentMembershipSubset(ctx, c, submission.ID, plan.DesiredItems()); err != nil {
		return result, err
	}
	attached, err := attachSubmissionItems(ctx, c, plan, submission.ID, verify)
	if err != nil {
		return result, err
	}
	result.Attached = attached
	if err := verifyExactMembership(ctx, c, submission.ID, plan.DesiredItems()); err != nil {
		return result, err
	}
	if err := preflight(ctx, c, plan, submission.ID); err != nil {
		return result, fmt.Errorf("submission: fresh preflight: %w", err)
	}
	return result, nil
}

func normalizeSubmissionPlan(plan SubmissionPlan) (SubmissionPlan, error) {
	return BuildSubmissionPlan(plan.AppID, plan.Platform, plan.Version, plan.Items)
}

func verifySubmissionItems(ctx context.Context, c *asc.Client, plan SubmissionPlan, verify SubmissionItemVerifier, prefix string) error {
	for _, item := range plan.DesiredItems() {
		if err := verify(ctx, c, plan, item); err != nil {
			return fmt.Errorf("submission: %sverify %s: %w", prefix, proposalKey(item), err)
		}
	}
	return nil
}

func attachSubmissionItems(ctx context.Context, c *asc.Client, plan SubmissionPlan, submissionID string, verify SubmissionItemVerifier) ([]asc.SubmissionItemProposal, error) {
	attached := make([]asc.SubmissionItemProposal, 0)
	for _, proposal := range plan.DesiredItems() {
		if err := verify(ctx, c, plan, proposal); err != nil {
			return nil, fmt.Errorf("submission: verify before attach %s: %w", proposalKey(proposal), err)
		}
		items, err := asc.ListSubmissionAssemblyItems(ctx, c, submissionID)
		if err != nil {
			return nil, err
		}
		if err := membershipSubset(items, plan.DesiredItems()); err != nil {
			return nil, err
		}
		if hasProposal(items, proposal) {
			continue
		}
		if _, err := asc.AddSubmissionAssemblyItem(ctx, c, submissionID, proposal); err != nil {
			items, readErr := asc.ListSubmissionAssemblyItems(ctx, c, submissionID)
			if readErr == nil && membershipSubset(items, plan.DesiredItems()) == nil && hasProposal(items, proposal) {
				continue
			}
			return nil, fmt.Errorf("submission: attach %s outcome is unconfirmed; inspect before retrying: %w", proposalKey(proposal), err)
		}
		attached = append(attached, proposal)
	}
	return attached, nil
}

func selectOrCreateSubmission(ctx context.Context, c *asc.Client, plan SubmissionPlan) (asc.SubmissionAssembly, bool, error) {
	submissions, err := asc.ListSubmissionAssemblies(ctx, c, plan.AppID)
	if err != nil {
		return asc.SubmissionAssembly{}, false, err
	}
	resumable, err := resumableSubmission(submissions, plan)
	if err != nil {
		return asc.SubmissionAssembly{}, false, err
	}
	if resumable != nil {
		return *resumable, false, nil
	}
	return createSubmissionForPlan(ctx, c, plan)
}

func resumableSubmission(submissions []asc.SubmissionAssembly, plan SubmissionPlan) (*asc.SubmissionAssembly, error) {
	var resumable []asc.SubmissionAssembly
	for _, submission := range submissions {
		if submission.Platform != "" && plan.Platform != "" && submission.Platform != plan.Platform {
			continue
		}
		switch submission.State {
		case asc.ReviewSubmissionStateReadyForReview:
			resumable = append(resumable, submission)
		case asc.ReviewSubmissionStateUnresolvedIssues, asc.ReviewSubmissionStateCanceling, asc.ReviewSubmissionStateCompleting:
			return nil, fmt.Errorf("submission: existing %s submission %s requires explicit recovery", submission.State, submission.ID)
		}
	}
	if len(resumable) > 1 {
		return nil, errors.New("submission: multiple resumable review submissions; inspect before assembly")
	}
	if len(resumable) == 1 {
		return &resumable[0], nil
	}
	return nil, nil
}

func createSubmissionForPlan(ctx context.Context, c *asc.Client, plan SubmissionPlan) (asc.SubmissionAssembly, bool, error) {
	created, err := asc.CreateSubmissionAssembly(ctx, c, plan.AppID, plan.Platform)
	if err != nil {
		if id := discoveredSubmissionID(ctx, c, plan); id != "" {
			return asc.SubmissionAssembly{}, false, fmt.Errorf("submission: create outcome is unconfirmed; discovered resumable submission %s, inspect before retrying: %w", id, err)
		}
		return asc.SubmissionAssembly{}, false, fmt.Errorf("submission: create outcome is unconfirmed; inspect before retrying: %w", err)
	}
	if created.State != asc.ReviewSubmissionStateReadyForReview {
		return asc.SubmissionAssembly{}, false, fmt.Errorf("submission: created review submission %s is %s; inspect before retrying", created.ID, created.State)
	}
	return created, true, nil
}

func discoveredSubmissionID(ctx context.Context, c *asc.Client, plan SubmissionPlan) string {
	after, err := asc.ListSubmissionAssemblies(ctx, c, plan.AppID)
	if err != nil {
		return ""
	}
	resumable, err := resumableSubmission(after, plan)
	if err != nil || resumable == nil {
		return ""
	}
	return resumable.ID
}

func hasProposal(items []asc.SubmissionAssemblyItem, want asc.SubmissionItemProposal) bool {
	for _, item := range items {
		if item.Proposal == want {
			return true
		}
	}
	return false
}

func verifyExactMembership(ctx context.Context, c *asc.Client, submissionID string, want []asc.SubmissionItemProposal) error {
	items, err := asc.ListSubmissionAssemblyItems(ctx, c, submissionID)
	if err != nil {
		return err
	}
	if len(items) != len(want) {
		return errors.New("submission: membership differs from the exact assembly plan")
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item.State != asc.ReviewSubmissionItemStateReadyForReview {
			return fmt.Errorf("submission: attached item %s is %s, not READY_FOR_REVIEW", item.ID, item.State)
		}
		key := proposalKey(item.Proposal)
		if seen[key] {
			return fmt.Errorf("submission: duplicate attached item %s", key)
		}
		seen[key] = true
	}
	for _, proposal := range want {
		if !seen[proposalKey(proposal)] {
			return fmt.Errorf("submission: missing attached item %s", proposalKey(proposal))
		}
	}
	return nil
}

func verifyCurrentMembershipSubset(ctx context.Context, c *asc.Client, submissionID string, want []asc.SubmissionItemProposal) error {
	items, err := asc.ListSubmissionAssemblyItems(ctx, c, submissionID)
	if err != nil {
		return err
	}
	return membershipSubset(items, want)
}

func membershipSubset(items []asc.SubmissionAssemblyItem, want []asc.SubmissionItemProposal) error {
	wanted := make(map[string]bool, len(want))
	for _, proposal := range want {
		wanted[proposalKey(proposal)] = true
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item.State != asc.ReviewSubmissionItemStateReadyForReview {
			return fmt.Errorf("submission: attached item %s is %s, not READY_FOR_REVIEW", item.ID, item.State)
		}
		key := proposalKey(item.Proposal)
		if seen[key] {
			return fmt.Errorf("submission: duplicate attached item %s", key)
		}
		if !wanted[key] {
			return fmt.Errorf("submission: existing item %s is outside the exact assembly plan", key)
		}
		seen[key] = true
	}
	return nil
}

// SubmitSubmissionPlan keeps final submit separate from assembly. It verifies
// every proposal, membership, and preflight even when assembly was a no-op.
func SubmitSubmissionPlan(ctx context.Context, c *asc.Client, plan SubmissionPlan, submissionID string, verify SubmissionItemVerifier, preflight SubmissionPreflight) error {
	if submissionID == "" || verify == nil || preflight == nil {
		return errors.New("submission: submission ID, verifier, and fresh preflight are required")
	}
	normalized, err := normalizeSubmissionPlan(plan)
	if err != nil {
		return err
	}
	plan = normalized
	if err := selectedSubmissionReady(ctx, c, plan, submissionID); err != nil {
		return err
	}
	if err := verifySubmissionItems(ctx, c, plan, verify, ""); err != nil {
		return err
	}
	if err := verifyExactMembership(ctx, c, submissionID, plan.DesiredItems()); err != nil {
		return err
	}
	if err := preflight(ctx, c, plan, submissionID); err != nil {
		return fmt.Errorf("submission: fresh preflight: %w", err)
	}
	if err := selectedSubmissionReady(ctx, c, plan, submissionID); err != nil {
		return err
	}
	if err := verifyExactMembership(ctx, c, submissionID, plan.DesiredItems()); err != nil {
		return err
	}
	if _, err := asc.SubmitSubmissionAssembly(ctx, c, submissionID); err != nil {
		return fmt.Errorf("submission: final submit outcome is unconfirmed; inspect before retrying: %w", err)
	}
	return nil
}

func selectedSubmissionReady(ctx context.Context, c *asc.Client, plan SubmissionPlan, submissionID string) error {
	submissions, err := asc.ListSubmissionAssemblies(ctx, c, plan.AppID)
	if err != nil {
		return err
	}
	var selected *asc.SubmissionAssembly
	for i := range submissions {
		if submissions[i].ID != submissionID {
			continue
		}
		if selected != nil {
			return errors.New("submission: selected review submission is ambiguous")
		}
		selected = &submissions[i]
	}
	if selected == nil {
		return errors.New("submission: selected review submission does not belong to this app")
	}
	if selected.Platform != "" && plan.Platform != "" && selected.Platform != plan.Platform {
		return errors.New("submission: selected review submission platform does not match plan")
	}
	if selected.State != asc.ReviewSubmissionStateReadyForReview {
		return fmt.Errorf("submission: selected review submission is %s and cannot be submitted", selected.State)
	}
	return nil
}
