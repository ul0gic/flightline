package state

import "github.com/ul0gic/flightline/internal/config"

func assetsRightsSnapshotResponse(path string) (string, bool) {
	responses := map[string]string{
		"/v1/appStoreVersions/VER1/appStoreVersionPhasedRelease": `{"data":{"type":"appStoreVersionPhasedReleases","id":"PHASE1","attributes":{"phasedReleaseState":"INACTIVE","currentDayNumber":0,"totalPauseDuration":0}}}`,
		"/v1/apps/APP1":                                              `{"data":{"type":"apps","id":"APP1","attributes":{"contentRightsDeclaration":"DOES_NOT_USE_THIRD_PARTY_CONTENT"}}}`,
		"/v1/apps/APP1/endUserLicenseAgreement":                      `{"data":{"type":"endUserLicenseAgreements","id":"EULA1","attributes":{"agreementText":"Synthetic agreement"}}}`,
		"/v1/endUserLicenseAgreements/EULA1/territories":             `{"data":[{"type":"territories","id":"USA"}]}`,
		"/v1/appStoreVersionLocalizations/VL1/appPreviewSets":        `{"data":[{"type":"appPreviewSets","id":"PREVS1","attributes":{"previewType":"IPHONE_67"}}]}`,
		"/v1/appCustomProductPageLocalizations/CPPL1/appPreviewSets": `{"data":[{"type":"appPreviewSets","id":"CPREVS1","attributes":{"previewType":"IPHONE_67"}}]}`,
		"/v1/appPreviewSets/PREVS1/appPreviews":                      `{"data":[{"type":"appPreviews","id":"PREV1","attributes":{"fileName":"main.mov","sourceFileChecksum":"11111111111111111111111111111111","previewFrameTimeCode":"00:00:01.000","videoDeliveryState":{"state":"COMPLETE"}}}]}`,
		"/v1/appPreviewSets/CPREVS1/appPreviews":                     `{"data":[{"type":"appPreviews","id":"CPREV1","attributes":{"fileName":"custom.mov","sourceFileChecksum":"22222222222222222222222222222222","videoDeliveryState":{"state":"COMPLETE"}}}]}`,
	}
	response, ok := responses[path]
	return response, ok
}

func assetsRightsSurfaceChecks(s *config.State) []surfaceCheck {
	main := s.Spec.Previews
	eula := s.Spec.AppEULA
	var cppFiles []config.PreviewFile
	if s.Spec.CustomProductPages != nil {
		cppFiles = (*s.Spec.CustomProductPages)["summer-2026"].Localizations["en-US"].Previews["IPHONE_67"]
	}
	return []surfaceCheck{
		{"spec.contentRights", derefEq(s.Spec.ContentRights, "DOES_NOT_USE_THIRD_PARTY_CONTENT")},
		{"spec.appEula", eula != nil && derefEq(eula.AgreementText, "Synthetic agreement") && eula.Territories != nil && len(*eula.Territories) == 1},
		{"spec.previews", main != nil && len(main.Locales["en-US"]["IPHONE_67"]) == 1 && main.Locales["en-US"]["IPHONE_67"][0].SourceFileChecksum == "11111111111111111111111111111111"},
		{"spec.customProductPages.previews", len(cppFiles) == 1 && cppFiles[0].SourceFileChecksum == "22222222222222222222222222222222"},
	}
}
