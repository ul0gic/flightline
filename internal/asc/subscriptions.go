package asc

// Subscriptions read surface. Apple structures auto-renewable subscriptions
// as a tree:
//
//   - SubscriptionGroup: a competing-tier group (e.g. "Pro Tiers").
//     One app can have multiple groups; users can be in only one
//     subscription per group at a time.
//   - Subscription: one product within a group (e.g. "Pro Monthly").
//     Carries productId, name, period, group level, family-shareability.
//   - SubscriptionLocalization: per-locale name/description for one
//     Subscription.
//   - SubscriptionGroupLocalization: per-locale name for the group.
//   - SubscriptionPrice / SubscriptionIntroductoryOffer: price ladder and
//     intro offers.
//
// v1 read surface is intentionally narrow — full CRUD lands in Phase 3.
//
// Source:
//
//	jq '.components.schemas.SubscriptionGroup.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.Subscription.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.SubscriptionLocalization.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.SubscriptionGroupLocalization.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.SubscriptionPrice.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.SubscriptionIntroductoryOffer.properties.attributes.properties' openapi.oas.json
//
// Reused by Phase 3 write verbs (`subscriptions update`, `subscriptions
// localize`); the wire shape stays the same on POST/PATCH.

// Apple-defined enums surfaced as named constants.
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

// SubscriptionGroupAttributes is the subset of Apple's
// SubscriptionGroup.attributes Skipper reads. ReferenceName is the
// developer-facing label (not user-visible); user-visible names live on
// SubscriptionGroupLocalization.
type SubscriptionGroupAttributes struct {
	ReferenceName string `json:"referenceName,omitempty"`
}

// SubscriptionAttributes is the subset of Apple's Subscription.attributes
// Skipper reads. GroupLevel is the rank within the group (1 = lowest tier);
// users in higher tiers cannot downgrade through normal flow.
type SubscriptionAttributes struct {
	Name               string `json:"name,omitempty"`
	ProductID          string `json:"productId,omitempty"`
	FamilySharable     *bool  `json:"familySharable,omitempty"`
	State              string `json:"state,omitempty"`
	SubscriptionPeriod string `json:"subscriptionPeriod,omitempty"`
	ReviewNote         string `json:"reviewNote,omitempty"`
	GroupLevel         int    `json:"groupLevel,omitempty"`
}

// SubscriptionLocalizationAttributes is the subset of Apple's
// SubscriptionLocalization.attributes Skipper reads. Apple stores the
// user-visible name and description per locale.
type SubscriptionLocalizationAttributes struct {
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
}

// SubscriptionGroupLocalizationAttributes is the subset of Apple's
// SubscriptionGroupLocalization.attributes Skipper reads.
type SubscriptionGroupLocalizationAttributes struct {
	Name          string `json:"name,omitempty"`
	CustomAppName string `json:"customAppName,omitempty"`
	Locale        string `json:"locale,omitempty"`
	State         string `json:"state,omitempty"`
}

// SubscriptionPriceAttributes is the subset of Apple's
// SubscriptionPrice.attributes Skipper reads. Apple's price ladder is
// expressed via prices linked to price-points (territory-priced); this
// struct is the price-record, not the price-point.
type SubscriptionPriceAttributes struct {
	StartDate string `json:"startDate,omitempty"`
	Preserved *bool  `json:"preserved,omitempty"`
}

// SubscriptionIntroductoryOfferAttributes is the subset of Apple's
// SubscriptionIntroductoryOffer.attributes Skipper reads.
type SubscriptionIntroductoryOfferAttributes struct {
	StartDate       string `json:"startDate,omitempty"`
	EndDate         string `json:"endDate,omitempty"`
	Duration        string `json:"duration,omitempty"`
	OfferMode       string `json:"offerMode,omitempty"`
	NumberOfPeriods int    `json:"numberOfPeriods,omitempty"`
}
