package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fullCoverageHandler returns an httptest handler with one row per surface, matching the
// resource IDs used in TestFetch_FullSurfaceCoverage.
func fullCoverageHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"type":"apps","id":"APP1","attributes":{"bundleId":"com.example.app"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0","platform":"IOS","copyright":"© 2026","releaseType":"MANUAL"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appInfos":
			_, _ = w.Write([]byte(`{"data":[{"type":"appInfos","id":"AINFO1","attributes":{"state":"PREPARE_FOR_SUBMISSION"}}],"links":{}}`))
		case r.URL.Path == "/v1/appInfos/AINFO1/ageRatingDeclaration":
			_, _ = w.Write([]byte(`{"data":{"type":"ageRatingDeclarations","id":"AR1","attributes":{"violenceCartoonOrFantasy":"NONE","gambling":false}}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/"):
			rel := strings.TrimPrefix(r.URL.Path, "/v1/appInfos/AINFO1/relationships/")
			switch rel {
			case "primaryCategory":
				_, _ = w.Write([]byte(`{"data":{"type":"appCategories","id":"EDUCATION"}}`))
			case "secondaryCategory":
				_, _ = w.Write([]byte(`{"data":{"type":"appCategories","id":"REFERENCE"}}`))
			default:
				_, _ = w.Write([]byte(`{"data":null}`))
			}
		case r.URL.Path == "/v1/appStoreVersions/VER1/build":
			_, _ = w.Write([]byte(`{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42","usesNonExemptEncryption":false}}}`))
		case r.URL.Path == "/v1/builds/BUILD1":
			_, _ = w.Write([]byte(`{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42"}}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1/appStoreReviewDetail":
			_, _ = w.Write([]byte(`{"data":{"type":"appStoreReviewDetails","id":"RD1","attributes":{"demoAccountName":"demo@x.com","contactFirstName":"Joe","contactLastName":"Tester","contactEmail":"qa@x.com","contactPhone":"555-0100","notes":"tap unlock"}}}`))
		case r.URL.Path == "/v1/appStoreVersions/VER1/appStoreVersionLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersionLocalizations","id":"VL1","attributes":{"locale":"en-US","description":"hello","keywords":"a,b","whatsNew":"new","promotionalText":"promo","marketingUrl":"https://x.com/m","supportUrl":"https://x.com/s"}}],"links":{}}`))
		case r.URL.Path == "/v1/appInfos/AINFO1/appInfoLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"appInfoLocalizations","id":"AL1","attributes":{"locale":"en-US","name":"App","subtitle":"sub","privacyPolicyUrl":"https://x.com/p"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/appPriceSchedule":
			_, _ = w.Write([]byte(`{"data":{"type":"appPriceSchedules","id":"PS1"},"included":[{"type":"territories","id":"USA"},{"type":"appPricePoints","id":"FREE"}]}`))
		case r.URL.Path == "/v1/apps/APP1/inAppPurchasesV2":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchases","id":"IAP1","attributes":{"productId":"com.x.lifetime","name":"Lifetime","inAppPurchaseType":"NON_CONSUMABLE","reviewNote":"unlock"}}],"links":{}}`))
		case r.URL.Path == "/v2/inAppPurchases/IAP1/inAppPurchaseLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseLocalizations","id":"IAPL1","attributes":{"locale":"en-US","name":"Lifetime","description":"unlock"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"type":"betaGroups","id":"BG1","attributes":{"name":"family","isInternalGroup":false,"publicLinkEnabled":false}}],"links":{}}`))
		case r.URL.Path == "/v1/betaGroups/BG1/betaTesters":
			_, _ = w.Write([]byte(`{"data":[{"type":"betaTesters","id":"BT1","attributes":{"email":"tester@x.com","firstName":"T","lastName":"One"}}],"links":{}}`))
		case r.URL.Path == "/v1/appStoreVersionLocalizations/VL1/appScreenshotSets":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshotSets","id":"SS1","attributes":{"screenshotDisplayType":"APP_IPHONE_69"}}],"links":{}}`))
		case r.URL.Path == "/v1/appScreenshotSets/SS1/appScreenshots":
			_, _ = w.Write([]byte(`{"data":[{"type":"appScreenshots","id":"SH1","attributes":{"fileName":"shot1.png","sourceFileChecksum":"abc"}}],"links":{}}`))
		case r.URL.Path == "/v1/apps/APP1/customProductPages":
			_, _ = w.Write([]byte(`{"data":[{"type":"customProductPages","id":"CPP1","attributes":{"name":"summer-2026","visible":true}}],"links":{}}`))
		case r.URL.Path == "/v1/customProductPages/CPP1/customProductPageVersions":
			_, _ = w.Write([]byte(`{"data":[{"type":"customProductPageVersions","id":"CPPV1","attributes":{"state":"APPROVED"}}],"links":{}}`))
		case r.URL.Path == "/v1/customProductPageVersions/CPPV1/customProductPageLocalizations":
			_, _ = w.Write([]byte(`{"data":[{"type":"customProductPageLocalizations","id":"CPPL1","attributes":{"locale":"en-US","promotionalText":"promo"}}],"links":{}}`))
		default:
			http.Error(w, "unhandled "+r.URL.Path, http.StatusNotFound)
		}
	})
}

// TestFetch_FullSurfaceCoverage verifies every spec surface (except privacyLabels, absent by design)
// ends up populated by Fetch.
func TestFetch_FullSurfaceCoverage(t *testing.T) {
	srv := httptest.NewServer(fullCoverageHandler(t))
	defer srv.Close()

	c := fixtureClient(t, srv)
	got, err := Fetch(context.Background(), c, "com.example.app", FetchOpts{Version: "1.0"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	checks := []struct {
		name string
		ok   bool
	}{
		{"version", got.Spec.Version != nil && got.Spec.Version.Copyright != nil},
		{"build", got.Spec.Build != nil && got.Spec.Build.Number == "42"},
		{"metadata", got.Spec.Metadata != nil && got.Spec.Metadata.Locales["en-US"].Description != nil},
		{"metadata.appInfo.name", got.Spec.Metadata != nil && got.Spec.Metadata.Locales["en-US"].Name != nil},
		{"screenshots", got.Spec.Screenshots != nil && len(got.Spec.Screenshots.Locales["en-US"]) > 0},
		{"iap", got.Spec.IAP != nil && len(got.Spec.IAP.Products) == 1},
		{"iap.localizations", got.Spec.IAP != nil && len(got.Spec.IAP.Products["com.x.lifetime"].Localizations) == 1},
		{"ageRating", got.Spec.AgeRating != nil && got.Spec.AgeRating.CartoonOrFantasyViolence != nil},
		{"exportCompliance", got.Spec.ExportCompliance != nil && got.Spec.ExportCompliance.UsesNonExemptEncryption != nil},
		{"reviewerDemo", got.Spec.ReviewerDemo != nil && got.Spec.ReviewerDemo.ContactEmail != nil},
		{"categories", got.Spec.Categories != nil && got.Spec.Categories.Primary != nil},
		{"pricing", got.Spec.Pricing != nil && got.Spec.Pricing.BaseTerritory != nil},
		{"testflight", got.Spec.TestFlight != nil && len(got.Spec.TestFlight.Groups) == 1},
		{"testflight.testers", got.Spec.TestFlight != nil && len(got.Spec.TestFlight.Groups["family"].Testers) == 1},
		{"customProductPages", got.Spec.CustomProductPages != nil && (*got.Spec.CustomProductPages)["summer-2026"].Visible != nil},
	}
	for _, ck := range checks {
		if !ck.ok {
			t.Errorf("surface %s: not populated", ck.name)
		}
	}
}
