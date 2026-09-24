package cmd

func init() {
	rootCmd.AddCommand(newAppTagsCommand(), newAccessibilityDeclarationsCommand())
	performanceCmd.AddCommand(newPerformanceOverviewCommand())
	rootCmd.AddCommand(newAppAvailabilityCommand(), newAppPricePointsCommand())
	iapCmd.AddCommand(newIAPCommerceCommand(), newIAPOfferCodesCommand(), newIAPPromotionCommand())
	testflightCmd.AddCommand(newBetaMetadataCommand(), newBetaDistributionCommand(), newBetaRecruitmentCommand())
	rootCmd.AddCommand(newAppPreviewsCommand(), newReviewAttachmentsCommand(), newContentRightsCommand(), newAppEULACommand())
	screenshotsCmd.AddCommand(newScreenshotOrderCommand())
	reviewsCmd.AddCommand(newReviewResponsesCommand())
	rootCmd.AddCommand(newPhasedReleaseCommand(), newVersionReleaseCommand(), newWebhooksCommand())
	submissionAssemblyDependencies = SubmissionAssemblyDependencies{Verify: verifySubmissionItem, Preflight: preflightSubmission}
	rootCmd.AddCommand(newSubmissionAssemblyCommand(), newEventsCommand(), newExperimentsCommand())
}
