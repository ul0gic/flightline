package asc

// IAP v2 enums. Auto-renewable subscriptions live under /v1/subscriptionGroups, not here.
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

// IAPAttributes is the subset of Apple's InAppPurchaseV2.attributes Flightline reads.
// FamilySharable/ContentHosting are pointer-bool: nil ("not set") and false differ during create flows.
type IAPAttributes struct {
	Name              string `json:"name,omitempty"`
	ProductID         string `json:"productId,omitempty"`
	InAppPurchaseType string `json:"inAppPurchaseType,omitempty"`
	State             string `json:"state,omitempty"`
	ReviewNote        string `json:"reviewNote,omitempty"`
	FamilySharable    *bool  `json:"familySharable,omitempty"`
	ContentHosting    *bool  `json:"contentHosting,omitempty"`
}

// IAPLocalizationAttributes is the subset of Apple's InAppPurchaseLocalization.attributes Flightline reads.
// Per-locale display name + description; State follows the localization review lifecycle (narrower than IAPState).
type IAPLocalizationAttributes struct {
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
}

// IAPReviewScreenshotAttributes is the subset of Apple's InAppPurchaseAppStoreReviewScreenshot.attributes Flightline reads.
// Exposes filename, size, asset-delivery state, and templated image URL; upload operations are not included.
type IAPReviewScreenshotAttributes struct {
	FileSize           int                `json:"fileSize,omitempty"`
	FileName           string             `json:"fileName,omitempty"`
	SourceFileChecksum string             `json:"sourceFileChecksum,omitempty"`
	AssetToken         string             `json:"assetToken,omitempty"`
	AssetType          string             `json:"assetType,omitempty"`
	AssetDeliveryState AppMediaAssetState `json:"assetDeliveryState,omitempty"`
	ImageAsset         ImageAsset         `json:"imageAsset,omitempty"`
}

// AppMediaAssetState is Apple's standard media-asset delivery-state envelope: state string + optional errors/warnings.
// Reused across screenshot, preview, icon, and IAP review screenshot resources.
type AppMediaAssetState struct {
	State    string               `json:"state,omitempty"`
	Errors   []AppMediaStateError `json:"errors,omitempty"`
	Warnings []AppMediaStateError `json:"warnings,omitempty"`
}

// AppMediaStateError is one entry in an AppMediaAssetState errors/warnings array; undocumented fields drop silently.
type AppMediaStateError struct {
	Code        string `json:"code,omitempty"`
	Description string `json:"description,omitempty"`
}

// ImageAsset is Apple's standard image-asset shape: a templated URL ({w}x{h}{f} placeholders) plus native size.
// Reused across IAP screenshots, app icons, and screenshot sets.
type ImageAsset struct {
	TemplateURL string `json:"templateUrl,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}
