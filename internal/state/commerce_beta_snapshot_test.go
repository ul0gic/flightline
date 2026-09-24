package state

// Complete synthetic W5 observations shared by the full-surface integration fixture.
func commerceBetaSnapshotResponse(path string) (string, bool) {
	responses := map[string]string{
		"/v2/inAppPurchases/IAP1/iapPriceSchedule":                 `{"data":{"type":"inAppPurchasePriceSchedules","id":"IPS1","relationships":{"baseTerritory":{"data":{"type":"territories","id":"USA"}}}}}`,
		"/v1/inAppPurchasePriceSchedules/IPS1/manualPrices":        `{"data":[{"type":"inAppPurchasePrices","id":"IPR1","attributes":{"manual":true},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}},"inAppPurchasePricePoint":{"data":{"type":"inAppPurchasePricePoints","id":"IP1"}}}}]}`,
		"/v2/inAppPurchases/IAP1/inAppPurchaseAvailability":        `{"data":{"type":"inAppPurchaseAvailabilities","id":"IA1","attributes":{"availableInNewTerritories":true}}}`,
		"/v1/inAppPurchaseAvailabilities/IA1/availableTerritories": `{"data":[{"type":"territories","id":"USA"}]}`,
		"/v1/apps/APP1/appAvailabilityV2":                          `{"data":{"type":"appAvailabilities","id":"AVA1","attributes":{"availableInNewTerritories":true}}}`,
		"/v2/appAvailabilities/AVA1/territoryAvailabilities":       `{"data":[{"type":"territoryAvailabilities","id":"AVT1","attributes":{"available":true,"preOrderEnabled":true,"releaseDate":"2099-01-01","contentStatuses":[]},"relationships":{"territory":{"data":{"type":"territories","id":"USA"}}}}]}`,
		"/v1/apps/APP1/betaAppLocalizations":                       `{"data":[{"type":"betaAppLocalizations","id":"BA1","attributes":{"locale":"en-US","description":"Beta description"}}]}`,
		"/v1/betaAppReviewDetails":                                 `{"data":[{"type":"betaAppReviewDetails","id":"BR1","attributes":{"contactEmail":"beta@example.com","demoAccountRequired":false}}]}`,
		"/v1/builds":                                               `{"data":[{"type":"builds","id":"BUILD1","attributes":{"version":"42"}}]}`,
		"/v1/builds/BUILD1/preReleaseVersion":                      `{"data":{"type":"preReleaseVersions","id":"PRE1","attributes":{"version":"1.0","platform":"IOS"}}}`,
		"/v1/builds/BUILD1/betaBuildLocalizations":                 `{"data":[{"type":"betaBuildLocalizations","id":"BB1","attributes":{"locale":"en-US","whatsNew":"Try sync"}}]}`,
		"/v1/betaGroups/BG1/builds":                                `{"data":[{"type":"builds","id":"BUILD1","attributes":{"version":"42"}}]}`,
	}
	response, ok := responses[path]
	return response, ok
}
