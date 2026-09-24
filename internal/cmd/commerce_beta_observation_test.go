package cmd

import (
	"net/http"
	"strings"
)

// Preflight fixtures have no configured commercial or beta metadata resources.
func g5PreflightObservation(w http.ResponseWriter, r *http.Request) bool {
	var response string
	switch {
	case strings.HasSuffix(r.URL.Path, "/appStoreVersionPhasedRelease"):
		w.WriteHeader(http.StatusNotFound)
		return true
	case r.URL.Path == "/v1/apps/app-1":
		response = `{"data":{"id":"app-1","type":"apps","attributes":{}}}`
	case r.URL.Path == "/v1/apps/app-1/endUserLicenseAgreement":
		response = `{"data":null}`
	case strings.HasSuffix(r.URL.Path, "/appPreviewSets"):
		response = `{"data":[]}`
	case strings.HasSuffix(r.URL.Path, "/appAvailabilityV2"), strings.HasSuffix(r.URL.Path, "/iapPriceSchedule"), strings.HasSuffix(r.URL.Path, "/inAppPurchaseAvailability"):
		w.WriteHeader(http.StatusNotFound)
		return true
	case r.URL.Path == "/v1/builds":
		response = `{"data":[{"id":"b-1","type":"builds","attributes":{"version":"42"}}]}`
	case strings.HasSuffix(r.URL.Path, "/preReleaseVersion"):
		response = `{"data":{"id":"pre-1","type":"preReleaseVersions","attributes":{"version":"1.0.1","platform":"IOS"}}}`
	case strings.HasSuffix(r.URL.Path, "/betaBuildLocalizations"), strings.HasSuffix(r.URL.Path, "/betaAppLocalizations"), r.URL.Path == "/v1/betaAppReviewDetails":
		response = `{"data":[]}`
	default:
		return false
	}
	_, _ = w.Write([]byte(response))
	return true
}
