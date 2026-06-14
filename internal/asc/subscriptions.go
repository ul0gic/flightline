package asc

// Users can hold only one subscription per group at a time.
const (
	SubscriptionStateMissingMetadata          = "MISSING_METADATA"
	SubscriptionStateReadyToSubmit            = "READY_TO_SUBMIT"
	SubscriptionStateWaitingForReview         = "WAITING_FOR_REVIEW"
	SubscriptionStateInReview                 = "IN_REVIEW"
	SubscriptionStateDeveloperActionNeeded    = "DEVELOPER_ACTION_NEEDED"
	SubscriptionStatePendingBinaryApproval    = "PENDING_BINARY_APPROVAL"
	SubscriptionStateApproved                 = "APPROVED"
	SubscriptionStateDeveloperRemovedFromSale = "DEVELOPER_REMOVED_FROM_SALE"
	SubscriptionStateRemovedFromSale          = "REMOVED_FROM_SALE"
	SubscriptionStateRejected                 = "REJECTED"

	SubscriptionPeriodOneWeek     = "ONE_WEEK"
	SubscriptionPeriodOneMonth    = "ONE_MONTH"
	SubscriptionPeriodTwoMonths   = "TWO_MONTHS"
	SubscriptionPeriodThreeMonths = "THREE_MONTHS"
	SubscriptionPeriodSixMonths   = "SIX_MONTHS"
	SubscriptionPeriodOneYear     = "ONE_YEAR"

	SubscriptionGroupLocalizationStatePrepare          = "PREPARE_FOR_SUBMISSION"
	SubscriptionGroupLocalizationStateWaitingForReview = "WAITING_FOR_REVIEW"
	SubscriptionGroupLocalizationStateApproved         = "APPROVED"
	SubscriptionGroupLocalizationStateRejected         = "REJECTED"
)

// SubscriptionGroupAttributes is the subset of Apple's SubscriptionGroup.attributes Flightline reads.
// ReferenceName is developer-facing only; user-visible names live on SubscriptionGroupLocalization.
type SubscriptionGroupAttributes struct {
	ReferenceName string `json:"referenceName,omitempty"`
}

// SubscriptionAttributes is the subset of Apple's Subscription.attributes Flightline reads.
// GroupLevel is the tier rank (1 = lowest); users cannot downgrade to a lower tier through normal flow.
type SubscriptionAttributes struct {
	Name               string `json:"name,omitempty"`
	ProductID          string `json:"productId,omitempty"`
	FamilySharable     *bool  `json:"familySharable,omitempty"`
	State              string `json:"state,omitempty"`
	SubscriptionPeriod string `json:"subscriptionPeriod,omitempty"`
	ReviewNote         string `json:"reviewNote,omitempty"`
	GroupLevel         int    `json:"groupLevel,omitempty"`
}

// SubscriptionLocalizationAttributes is the subset of Apple's SubscriptionLocalization.attributes Flightline reads.
type SubscriptionLocalizationAttributes struct {
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
}

// SubscriptionGroupLocalizationAttributes is the subset of Apple's
// SubscriptionGroupLocalization.attributes Flightline reads.
type SubscriptionGroupLocalizationAttributes struct {
	Name          string `json:"name,omitempty"`
	CustomAppName string `json:"customAppName,omitempty"`
	Locale        string `json:"locale,omitempty"`
	State         string `json:"state,omitempty"`
}

// SubscriptionPriceAttributes is the subset of Apple's SubscriptionPrice.attributes Flightline reads.
// This is the price-record (a window linked to a price-point), not the price-point itself.
type SubscriptionPriceAttributes struct {
	StartDate string `json:"startDate,omitempty"`
	Preserved *bool  `json:"preserved,omitempty"`
}

// SubscriptionIntroductoryOfferAttributes is the subset of Apple's
// SubscriptionIntroductoryOffer.attributes Flightline reads.
type SubscriptionIntroductoryOfferAttributes struct {
	StartDate       string `json:"startDate,omitempty"`
	EndDate         string `json:"endDate,omitempty"`
	Duration        string `json:"duration,omitempty"`
	OfferMode       string `json:"offerMode,omitempty"`
	NumberOfPeriods int    `json:"numberOfPeriods,omitempty"`
}
