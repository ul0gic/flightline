package asc

import "fmt"

// SubmissionItemProposal is an untrusted resource selection, not permission to
// attach or submit it. Assembly must freshly verify ownership and readiness.
type SubmissionItemProposal struct {
	Relationship string `json:"relationship"`
	Type         string `json:"type"`
	ID           string `json:"id"`
}

// Validate checks only the supported wire identity, never resource eligibility.
func (p SubmissionItemProposal) Validate() error {
	types := map[string]string{
		"appStoreVersion":             "appStoreVersions",
		"inAppPurchaseVersion":        "inAppPurchaseVersions",
		"appEvent":                    "appEvents",
		"appStoreVersionExperimentV2": "appStoreVersionExperiments",
	}
	want, ok := types[p.Relationship]
	if !ok || want != p.Type || p.ID == "" {
		return fmt.Errorf("asc: unsupported or incomplete submission item %q/%q/%q", p.Relationship, p.Type, p.ID)
	}
	return nil
}
