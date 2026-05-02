package asc

// In-App Purchase v2 enums (Apple-defined). Listed as named constants so
// command code can filter/compare without typos and so readers can grep for
// the canonical set.
//
// Source: jq '.components.schemas.InAppPurchaseType, .components.schemas.InAppPurchaseState' openapi.oas.json
//
// Note on subscriptions: Apple's auto-renewable subscriptions live under a
// separate /v1/subscriptionGroups resource and are NOT modeled here — see the
// Phase 2.4 subscriptions slot for that surface. The v2 InAppPurchaseType enum
// only carries the non-subscription kinds plus the legacy NON_RENEWING_SUBSCRIPTION.
const (
	IAPTypeConsumable              = "CONSUMABLE"
	IAPTypeNonConsumable           = "NON_CONSUMABLE"
	IAPTypeNonRenewingSubscription = "NON_RENEWING_SUBSCRIPTION"

	IAPStateMissingMetadata          = "MISSING_METADATA"
	IAPStateWaitingForUpload         = "WAITING_FOR_UPLOAD"
	IAPStateProcessingContent        = "PROCESSING_CONTENT"
	IAPStateReadyToSubmit            = "READY_TO_SUBMIT"
	IAPStateWaitingForReview         = "WAITING_FOR_REVIEW"
	IAPStateInReview                 = "IN_REVIEW"
	IAPStateDeveloperActionNeeded    = "DEVELOPER_ACTION_NEEDED"
	IAPStatePendingBinaryApproval    = "PENDING_BINARY_APPROVAL"
	IAPStateApproved                 = "APPROVED"
	IAPStateDeveloperRemovedFromSale = "DEVELOPER_REMOVED_FROM_SALE"
	IAPStateRemovedFromSale          = "REMOVED_FROM_SALE"
	IAPStateRejected                 = "REJECTED"

	IAPLocalizationStatePrepareForSubmission = "PREPARE_FOR_SUBMISSION"
	IAPLocalizationStateWaitingForReview     = "WAITING_FOR_REVIEW"
	IAPLocalizationStateApproved             = "APPROVED"
	IAPLocalizationStateRejected             = "REJECTED"
)

// IAPAttributes is the subset of Apple's InAppPurchaseV2.attributes Skipper
// reads.
//
// Source: jq '.components.schemas.InAppPurchaseV2.properties.attributes.properties' openapi.oas.json
//
// Field notes:
//   - ProductID is the developer-chosen StoreKit identifier
//     (e.g. com.example.lifetime). Stable across renames; the L2 state file
//     keys IAPs by this rather than Apple's numeric ID.
//   - InAppPurchaseType: see IAPType* constants. Auto-renewable subs don't
//     appear in this enum; they're a separate resource entirely.
//   - State: see IAPState* constants. MISSING_METADATA is the on-create state
//     before localizations and pricing exist; READY_TO_SUBMIT is the gate to
//     review.
//   - FamilySharable / ContentHosting: pointer-bool because nil ("not set")
//     and false carry different operational meaning during create flows.
//
// Reused by Phase 3 write verbs (`iap update` patches these); add fields
// conservatively. Field names match Apple's wire casing exactly — the JSON
// output is a stable contract.
type IAPAttributes struct {
	Name              string `json:"name,omitempty"`
	ProductID         string `json:"productId,omitempty"`
	InAppPurchaseType string `json:"inAppPurchaseType,omitempty"`
	State             string `json:"state,omitempty"`
	ReviewNote        string `json:"reviewNote,omitempty"`
	FamilySharable    *bool  `json:"familySharable,omitempty"`
	ContentHosting    *bool  `json:"contentHosting,omitempty"`
}

// IAPLocalizationAttributes is the subset of Apple's
// InAppPurchaseLocalization.attributes Skipper reads.
//
// Source: jq '.components.schemas.InAppPurchaseLocalization.properties.attributes.properties' openapi.oas.json
//
// Per-locale display name + description; State enum is narrower than the
// parent IAPState enum (localizations have their own review lifecycle).
type IAPLocalizationAttributes struct {
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
}

// IAPReviewScreenshotAttributes is the subset of Apple's
// InAppPurchaseAppStoreReviewScreenshot.attributes Skipper reads.
//
// Source: jq '.components.schemas.InAppPurchaseAppStoreReviewScreenshot.properties.attributes.properties' openapi.oas.json
//
// Skipper exposes the screenshot's filename, size, asset-delivery state, and
// the templated image URL so callers can fetch the rendered preview without
// reaching into the upload-operations array. AssetDeliveryState is Apple's
// AppMediaAssetState envelope: a string state plus optional errors/warnings.
type IAPReviewScreenshotAttributes struct {
	FileSize           int                `json:"fileSize,omitempty"`
	FileName           string             `json:"fileName,omitempty"`
	SourceFileChecksum string             `json:"sourceFileChecksum,omitempty"`
	AssetToken         string             `json:"assetToken,omitempty"`
	AssetType          string             `json:"assetType,omitempty"`
	AssetDeliveryState AppMediaAssetState `json:"assetDeliveryState,omitempty"`
	ImageAsset         ImageAsset         `json:"imageAsset,omitempty"`
}

// AppMediaAssetState is Apple's standard media-asset delivery-state envelope:
// a state string plus optional errors/warnings. Reused across screenshot,
// preview, icon, and IAP review screenshot resources.
//
// Source: jq '.components.schemas.AppMediaAssetState' openapi.oas.json
type AppMediaAssetState struct {
	State    string               `json:"state,omitempty"`
	Errors   []AppMediaStateError `json:"errors,omitempty"`
	Warnings []AppMediaStateError `json:"warnings,omitempty"`
}

// AppMediaStateError is one entry in an AppMediaAssetState's errors or
// warnings array. The shape is a loose key/value bag in Apple's spec; we
// surface the documented fields and let extras drop on the floor.
type AppMediaStateError struct {
	Code        string `json:"code,omitempty"`
	Description string `json:"description,omitempty"`
}

// ImageAsset is Apple's standard image-asset shape: a templated URL with
// {w}x{h}{f} placeholders plus the native size. Reused across IAP
// screenshots, app icons, and screenshot sets.
//
// Source: jq '.components.schemas.ImageAsset' openapi.oas.json
type ImageAsset struct {
	TemplateURL string `json:"templateUrl,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}
