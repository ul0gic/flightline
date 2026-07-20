package cmd

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"testing"
)

// TestJSONStability_AllCommands locks the top-level JSON keys for every read
// command. Adding a key is safe; renaming or removing one breaks consumers.
func TestJSONStability_AllCommands(t *testing.T) {
	cases := []struct {
		name string
		view any

		// wantTopLevel keys MUST appear in the rendered output.
		wantTopLevel []string

		// nestedPath/nestedKeys, when set, assert keys on a nested object.
		nestedPath []string
		nestedKeys []string
	}{
		{
			name: "apps list",
			view: AppList{
				Apps: []AppView{{
					ID:   "1234567890",
					Type: "apps",
					Attributes: AppAttributes{
						Name:          "Example Alpha",
						BundleID:      "com.example.alpha",
						SKU:           "EXAMPLE_ALPHA",
						PrimaryLocale: "en-US",
					},
				}},
			},
			wantTopLevel: []string{"apps"},
			nestedPath:   []string{"apps", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "apps get",
			view: &AppView{
				ID:   "1234567890",
				Type: "apps",
				Attributes: AppAttributes{
					Name:          "Example Alpha",
					BundleID:      "com.example.alpha",
					SKU:           "EXAMPLE_ALPHA",
					PrimaryLocale: "en-US",
				},
			},
			wantTopLevel: []string{"id", "type", "attributes"},
		},

		{
			name: "whoami",
			view: WhoamiInfo{
				KeyID:        "TEST123ABC",
				IssuerID:     "11111111-2222-3333-4444-555555555555",
				VendorNumber: "99999999",
				Authorized:   true,
				APIBaseURL:   "https://api.appstoreconnect.apple.com",
			},
			wantTopLevel: []string{"keyId", "issuerId", "vendorNumber", "authorized", "apiBaseUrl"},
		},

		{
			name: "versions list",
			view: VersionList{
				Versions: []VersionView{{ID: "8000000001", Type: "appStoreVersions"}},
			},
			wantTopLevel: []string{"versions"},
			nestedPath:   []string{"versions", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name:         "versions get",
			view:         &VersionView{ID: "8000000001", Type: "appStoreVersions"},
			wantTopLevel: []string{"id", "type", "attributes"},
		},

		{
			name: "builds list",
			view: BuildList{
				Builds: []BuildView{{ID: "9000000001", Type: "builds"}},
			},
			wantTopLevel: []string{"builds"},
			nestedPath:   []string{"builds", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name:         "builds get",
			view:         &BuildView{ID: "9000000001", Type: "builds"},
			wantTopLevel: []string{"id", "type", "attributes"},
		},

		{
			name: "review-submissions list",
			view: ReviewSubmissionList{
				Submissions: []ReviewSubmissionView{{ID: "rs-7700000000", Type: "reviewSubmissions"}},
			},
			wantTopLevel: []string{"submissions"},
			nestedPath:   []string{"submissions", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "review-submissions items",
			view: ReviewSubmissionItemList{
				Items: []ReviewSubmissionItemView{{
					ID: "rsi-1", Type: "reviewSubmissionItems",
					ReferenceType: "appStoreVersion", ReferenceID: "8000000001",
				}},
			},
			wantTopLevel: []string{"items"},
			nestedPath:   []string{"items", "0"},
			// referenceType / referenceId are omitempty: populated above.
			nestedKeys: []string{"id", "type", "attributes", "referenceType", "referenceId"},
		},

		{
			name: "rejection",
			view: &RejectionReport{
				BundleID: "com.example.alpha",
				Version: RejectionVersion{
					ID:            "8000000001",
					VersionString: "1.0.0",
					Platform:      "IOS",
				},
				Note: "no submission",
			},
			wantTopLevel: []string{"bundleId", "version", "note"},
		},

		{
			name: "iap list",
			view: IAPList{
				IAPs: []IAPView{{ID: "iap-1", Type: "inAppPurchases"}},
			},
			wantTopLevel: []string{"iaps"},
			nestedPath:   []string{"iaps", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name:         "iap get",
			view:         &IAPView{ID: "iap-1", Type: "inAppPurchases"},
			wantTopLevel: []string{"id", "type", "attributes"},
		},
		{
			name: "iap localizations list",
			view: IAPLocalizationList{
				Localizations: []IAPLocalizationView{{ID: "loc-1", Type: "inAppPurchaseLocalizations"}},
			},
			wantTopLevel: []string{"localizations"},
			nestedPath:   []string{"localizations", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},

		{
			name: "age-rating get",
			view: &AgeRatingView{
				ID:           "AAAA0000-0000-0000-0000-000000000001",
				Type:         "ageRatingDeclarations",
				VersionState: "PREPARE_FOR_SUBMISSION",
			},
			wantTopLevel: []string{"id", "type", "attributes", "versionState"},
		},

		{
			name: "export-compliance get",
			view: &ExportComplianceView{
				BundleID:      "com.example.alpha",
				VersionString: "1.0.0",
			},
			wantTopLevel: []string{"bundleId", "versionString", "build"},
		},

		{
			name: "privacy-labels get",
			view: &PrivacyLabelsView{
				BundleID:  "com.example.alpha",
				Supported: false,
				Reason:    "stub",
				Reference: "ref",
			},
			wantTopLevel: []string{"bundleId", "supported", "reason", "reference"},
		},

		{
			name: "territories list",
			view: TerritoryList{
				Territories: []TerritoryView{{ID: "USA", Type: "territories"}},
			},
			wantTopLevel: []string{"territories"},
			nestedPath:   []string{"territories", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},

		{
			name: "categories list",
			view: CategoryList{
				Categories: []CategoryView{{ID: "PRODUCTIVITY", Type: "appCategories"}},
			},
			wantTopLevel: []string{"categories"},
			nestedPath:   []string{"categories", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "categories get",
			view: &CategoryAssignmentView{
				BundleID:        "com.example.alpha",
				AppInfoID:       "info-1",
				AppInfoState:    "READY_FOR_DISTRIBUTION",
				PrimaryCategory: "PRODUCTIVITY",
			},
			// AppInfoState / PrimaryCategory etc. are omitempty: populated above.
			wantTopLevel: []string{"bundleId", "appInfoId", "appInfoState", "primaryCategory"},
		},

		{
			name: "pricing get",
			view: &PricingView{
				BundleID: "com.example.alpha",
				Schedule: PriceScheduleSummary{
					ID:                  "sched-1",
					BaseTerritoryID:     "USA",
					BaseCurrency:        "USD",
					ManualPriceCount:    1,
					AutomaticPriceCount: 0,
				},
				Availability: AvailabilitySummary{
					ID:             "avail-1",
					AvailableTotal: 175,
					AvailableCount: 175,
				},
			},
			wantTopLevel: []string{"bundleId", "schedule", "availability"},
		},

		{
			name: "testflight groups list",
			view: BetaGroupList{
				Groups: []BetaGroupView{{ID: "bg-1", Type: "betaGroups"}},
			},
			wantTopLevel: []string{"groups"},
			nestedPath:   []string{"groups", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "testflight testers list",
			view: BetaTesterList{
				Testers: []BetaTesterView{{ID: "bt-1", Type: "betaTesters"}},
			},
			wantTopLevel: []string{"testers"},
			nestedPath:   []string{"testers", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "testflight beta-review get",
			view: &BetaReviewView{
				BundleID:    "com.example.alpha",
				BuildID:     "9000000001",
				BuildNumber: "42",
				ID:          "br-1",
				Type:        "betaAppReviewSubmissions",
			},
			wantTopLevel: []string{"bundleId", "buildId", "buildNumber", "id", "type", "attributes"},
		},

		{
			name: "custom-product-pages list",
			view: CustomProductPageList{
				Pages: []CustomProductPageView{{ID: "cpp-1", Type: "appCustomProductPages"}},
			},
			wantTopLevel: []string{"pages", "complete", "enrichedCount", "totalCount"},
			nestedPath:   []string{"pages", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name:         "custom-product-pages get",
			view:         &CustomProductPageView{ID: "cpp-1", Type: "appCustomProductPages"},
			wantTopLevel: []string{"id", "type", "attributes"},
		},

		{
			name: "reviews list",
			view: ReviewList{
				Reviews: []ReviewView{{ID: "rev-1", Type: "customerReviews"}},
			},
			wantTopLevel: []string{"reviews"},
			nestedPath:   []string{"reviews", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name:         "reviews get",
			view:         &ReviewView{ID: "rev-1", Type: "customerReviews"},
			wantTopLevel: []string{"id", "type", "attributes"},
		},
		{
			name: "reviews summary",
			view: &ReviewSummaryView{
				BundleID: "com.example.alpha",
				Summarizations: []ReviewSummarizationItem{{
					ID: "sum-1", Type: "customerReviewSummarizations",
				}},
			},
			wantTopLevel: []string{"bundleId", "summarizations"},
		},

		{
			name: "beta-feedback crash",
			view: BetaFeedbackCrashList{
				Submissions: []BetaFeedbackCrashView{{ID: "bfc-1", Type: "betaFeedbackCrashSubmissions"}},
			},
			wantTopLevel: []string{"submissions"},
			nestedPath:   []string{"submissions", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "beta-feedback screenshot",
			view: BetaFeedbackScreenshotList{
				Submissions: []BetaFeedbackScreenshotView{{ID: "bfs-1", Type: "betaFeedbackScreenshotSubmissions"}},
			},
			wantTopLevel: []string{"submissions"},
			nestedPath:   []string{"submissions", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name: "beta-feedback download",
			view: &BetaFeedbackDownloadView{
				ID:      "bfc-1",
				Type:    "betaFeedbackCrashSubmissions",
				SavedTo: "/tmp/x.crash",
				Bytes:   2048,
			},
			wantTopLevel: []string{"id", "type", "savedTo", "bytes"},
		},

		{
			name: "diagnostics list",
			view: DiagnosticSignatureList{
				BundleID: "com.example.alpha",
				BuildID:  "9000000001",
				Signatures: []DiagnosticSignatureView{{
					ID: "diag-1", Type: "diagnosticSignatures",
				}},
			},
			wantTopLevel: []string{"bundleId", "buildId", "signatures"},
		},
		{
			name: "diagnostics get",
			view: &DiagnosticGetView{
				SignatureID: "diag-1",
				Version:     "16.0",
			},
			wantTopLevel: []string{"signatureId", "version"},
		},

		{
			name: "performance app",
			view: &PerformanceView{
				BundleID: "com.example.alpha",
				Version:  "1.0.0",
			},
			wantTopLevel: []string{"bundleId", "version"},
		},
		{
			name: "performance build",
			view: &PerformanceView{
				BundleID:    "com.example.alpha",
				BuildNumber: "42",
				BuildID:     "9000000001",
				Version:     "1.0.0",
			},
			wantTopLevel: []string{"bundleId", "buildNumber", "buildId", "version"},
		},

		{
			name: "subscriptions list",
			view: SubscriptionGroupList{
				BundleID: "com.example.alpha",
				Groups: []SubscriptionGroupView{{
					ID: "sg-1", Type: "subscriptionGroups", MemberCount: 2,
				}},
			},
			wantTopLevel: []string{"bundleId", "groups"},
			nestedPath:   []string{"groups", "0"},
			nestedKeys:   []string{"id", "type", "attributes", "memberCount"},
		},
		{
			name: "subscriptions get",
			view: &SubscriptionDetailView{
				BundleID: "com.example.alpha",
				ID:       "sub-1",
				Type:     "subscriptions",
			},
			wantTopLevel: []string{"bundleId", "id", "type", "attributes"},
		},
	}

	// A drop below the floor means a stability row was deleted accidentally.
	if len(cases) < 30 {
		t.Fatalf("test case count = %d, want >= 30: a command stability row was removed", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderTo(&buf, tc.view, "json", true); err != nil {
				t.Fatalf("renderTo: %v\nview: %T", err, tc.view)
			}
			var decoded map[string]any
			if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
				t.Fatalf("decode top-level object: %v\nraw: %s", err, buf.String())
			}
			assertKeysPresent(t, decoded, tc.wantTopLevel)
			if tc.nestedPath != nil {
				assertNestedKeys(t, decoded, tc.nestedPath, tc.nestedKeys)
			}
		})
	}
}

// assertKeysPresent fails for any key absent from the decoded JSON object.
func assertKeysPresent(t *testing.T, decoded map[string]any, want []string) {
	t.Helper()
	for _, key := range want {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q: JSON output is a contract; adding "+
				"fields is safe but removing/renaming breaks consumers (jq pipelines, "+
				"LLM parsers, the L2 state-as-code round-trip).\nGot keys: %v",
				key, sortedMapKeys(decoded))
		}
	}
}

// assertNestedKeys descends path into decoded and fails for any missing key.
func assertNestedKeys(t *testing.T, decoded map[string]any, path, want []string) {
	t.Helper()
	nested := walkPath(t, decoded, path)
	if nested == nil {
		return // walkPath already emitted an error
	}
	nestedMap, ok := nested.(map[string]any)
	if !ok {
		t.Fatalf("nested path %v resolved to %T, want object", path, nested)
	}
	for _, key := range want {
		if _, ok := nestedMap[key]; !ok {
			t.Errorf("missing nested key %q at path %v: JSON contract drift in row shape. Got: %v",
				key, path, sortedMapKeys(nestedMap))
		}
	}
}

// sortedMapKeys returns a JSON object's keys in stable order for diagnostics.
func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// walkPath descends into a decoded JSON object via a path of keys / array
// indices. Numeric segments select array indices; others select object keys.
func walkPath(t *testing.T, root map[string]any, path []string) any {
	t.Helper()
	var cur any = root
	for i, seg := range path {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				t.Errorf("walkPath: missing key %q at index %d in path %v", seg, i, path)
				return nil
			}
			cur = next
		case []any:
			cur = indexInto(t, v, seg, path)
		default:
			t.Errorf("walkPath: cannot descend into %T at index %d in path %v", cur, i, path)
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

// indexInto resolves a numeric path segment against a JSON array.
func indexInto(t *testing.T, arr []any, seg string, path []string) any {
	t.Helper()
	idx, err := strconv.Atoi(seg)
	if err != nil || idx < 0 {
		t.Errorf("walkPath: invalid array index %q at path %v", seg, path)
		return nil
	}
	if idx >= len(arr) {
		t.Errorf("walkPath: index %d out of range (len=%d) at path %v", idx, len(arr), path)
		return nil
	}
	return arr[idx]
}
