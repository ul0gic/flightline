package state

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ul0gic/flightline/internal/asc"
)

// SubmissionPlan is a deterministic, single-version review assembly request.
// It contains resource identities only; ApplySubmissionPlan must freshly verify
// every identity before it writes.
type SubmissionPlan struct {
	AppID    string
	Platform string
	Version  asc.SubmissionItemProposal
	Items    []asc.SubmissionItemProposal
}

// BuildSubmissionPlan validates exact membership and orders the version before
// IAP versions, events, and experiments.
func BuildSubmissionPlan(appID, platform string, version asc.SubmissionItemProposal, proposals []asc.SubmissionItemProposal) (SubmissionPlan, error) {
	if appID == "" {
		return SubmissionPlan{}, errors.New("submission: app ID is required")
	}
	if !submissionPlatform(platform) {
		return SubmissionPlan{}, fmt.Errorf("submission: unsupported platform %q", platform)
	}
	if version.Relationship != "appStoreVersion" || version.Type != "appStoreVersions" {
		return SubmissionPlan{}, errors.New("submission: exactly one app store version is required")
	}
	if err := version.Validate(); err != nil {
		return SubmissionPlan{}, fmt.Errorf("submission: version: %w", err)
	}
	seen := map[string]bool{proposalKey(version): true}
	items := make([]asc.SubmissionItemProposal, 0, len(proposals))
	for _, proposal := range proposals {
		if err := proposal.Validate(); err != nil {
			return SubmissionPlan{}, fmt.Errorf("submission: item: %w", err)
		}
		if proposal.Relationship == "appStoreVersion" {
			return SubmissionPlan{}, errors.New("submission: app store version belongs in the version slot")
		}
		key := proposalKey(proposal)
		if seen[key] {
			return SubmissionPlan{}, fmt.Errorf("submission: duplicate item %s", key)
		}
		seen[key] = true
		items = append(items, proposal)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return proposalOrder(items[i]) < proposalOrder(items[j])
	})
	return SubmissionPlan{AppID: appID, Platform: platform, Version: version, Items: items}, nil
}

func submissionPlatform(platform string) bool {
	switch platform {
	case "IOS", "MAC_OS", "TV_OS", "VISION_OS":
		return true
	default:
		return false
	}
}

func proposalKey(p asc.SubmissionItemProposal) string {
	return p.Relationship + "/" + p.Type + "/" + p.ID
}

func proposalOrder(p asc.SubmissionItemProposal) int {
	switch p.Relationship {
	case "inAppPurchaseVersion":
		return 1
	case "appEvent":
		return 2
	case "appStoreVersionExperimentV2":
		return 3
	default:
		return 4
	}
}

func (p SubmissionPlan) DesiredItems() []asc.SubmissionItemProposal {
	items := make([]asc.SubmissionItemProposal, 0, len(p.Items)+1)
	items = append(items, p.Version)
	items = append(items, p.Items...)
	return items
}
