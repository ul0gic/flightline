package cmd

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"
)

// TestJSONStability_AllCommands locks the top-level JSON keys for every
// read command Skipper exposes. Adding a new key is safe; renaming or
// removing one is a breaking change for shell pipelines (`jq '.foo.bar'`)
// and LLM consumers parsing structured output.
//
// Each row exercises the production view struct through the shared
// renderTo() pipeline (the same code path Render() uses with --output json).
// Coverage is shape-only: we assert documented top-level keys are present
// and nothing more — value assertions live in the per-command tests where
// they belong.
//
// When this fails, the diff between expected and actual keys names the
// breaking surface. Fix by either:
//   - restoring the missing key (preserve the contract), or
//   - adding the key to wantTopLevel here AND bumping a major version
//     (intentional break with a documented migration note).
func TestJSONStability_AllCommands(t *testing.T) {
	cases := []struct {
		// name identifies the command + variant. Reads as
		// "<command> <subcommand>" e.g. "apps list", "iap get".
		name string

		// view is a populated instance of the production view type the
		// command renders. Fields are filled with realistic placeholder
		// values; the test only inspects keys, not values.
		view any

		// wantTopLevel is the documented JSON contract — every key here
		// MUST appear in the rendered output. Sorted alphabetically by
		// convention (mapKeys() sorts the diagnostic output too).
		wantTopLevel []string

		// nestedKeys, when non-nil, asserts a nested object's keys.
		// Use for view structs whose primary contract is nested under
		// a single "data" / "attributes" wrapper (e.g. AppList.apps[]).
		// Path is a JSON path into the decoded map; nil = top level only.
		nestedPath []string
		nestedKeys []string
	}{
		// ---- apps ----
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

		// ---- whoami ----
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

		// ---- versions ----
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

		// ---- builds ----
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

		// ---- review-submissions ----
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
			// referenceType / referenceId are omitempty — populated above.
			nestedKeys: []string{"id", "type", "attributes", "referenceType", "referenceId"},
		},

		// ---- rejection ----
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

		// ---- iap ----
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

		// ---- age-rating ----
		{
			name: "age-rating get",
			view: &AgeRatingView{
				ID:           "AAAA0000-0000-0000-0000-000000000001",
				Type:         "ageRatingDeclarations",
				VersionState: "PREPARE_FOR_SUBMISSION",
			},
			wantTopLevel: []string{"id", "type", "attributes", "versionState"},
		},

		// ---- export-compliance ----
		{
			name: "export-compliance get",
			view: &ExportComplianceView{
				BundleID:      "com.example.alpha",
				VersionString: "1.0.0",
			},
			wantTopLevel: []string{"bundleId", "versionString", "build"},
		},

		// ---- privacy-labels (v4.3 stub) ----
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

		// ---- territories ----
		{
			name: "territories list",
			view: TerritoryList{
				Territories: []TerritoryView{{ID: "USA", Type: "territories"}},
			},
			wantTopLevel: []string{"territories"},
			nestedPath:   []string{"territories", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},

		// ---- categories ----
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
			// AppInfoState / PrimaryCategory etc. are omitempty — populated above.
			wantTopLevel: []string{"bundleId", "appInfoId", "appInfoState", "primaryCategory"},
		},

		// ---- pricing ----
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

		// ---- testflight ----
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

		// ---- custom-product-pages ----
		{
			name: "custom-product-pages list",
			view: CustomProductPageList{
				Pages: []CustomProductPageView{{ID: "cpp-1", Type: "appCustomProductPages"}},
			},
			wantTopLevel: []string{"pages"},
			nestedPath:   []string{"pages", "0"},
			nestedKeys:   []string{"id", "type", "attributes"},
		},
		{
			name:         "custom-product-pages get",
			view:         &CustomProductPageView{ID: "cpp-1", Type: "appCustomProductPages"},
			wantTopLevel: []string{"id", "type", "attributes"},
		},

		// ---- reviews ----
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

		// ---- beta-feedback ----
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

		// ---- diagnostics ----
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

		// ---- performance ----
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

		// ---- subscriptions ----
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

	// Sanity: every documented command in build-plan.md appears here.
	// 22 commands, several with multiple variants → expect 30+ rows. If the
	// count drops below the floor, a row was deleted accidentally.
	if len(cases) < 30 {
		t.Fatalf("test case count = %d, want >= 30 — a command stability row was removed", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderTo(&buf, tc.view, "json", true); err != nil {
				t.Fatalf("renderTo: %v\nview: %T", err, tc.view)
			}

			// Decode into either an object or array depending on shape.
			// All the registered views serialize to objects at top level.
			var decoded map[string]any
			if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
				t.Fatalf("decode top-level object: %v\nraw: %s", err, buf.String())
			}

			gotKeys := sortedMapKeys(decoded)
			for _, key := range tc.wantTopLevel {
				if _, ok := decoded[key]; !ok {
					t.Errorf("missing top-level key %q — JSON output is a contract; "+
						"adding fields is safe but removing/renaming breaks consumers "+
						"(shell pipelines using `jq`, LLM consumers parsing structured output, "+
						"and the L2 state-as-code layer that round-trips this shape).\n"+
						"Got keys: %v", key, gotKeys)
				}
			}

			if tc.nestedPath != nil {
				nested := walkPath(t, decoded, tc.nestedPath)
				if nested == nil {
					return // walkPath already emitted an error
				}
				nestedMap, ok := nested.(map[string]any)
				if !ok {
					t.Fatalf("nested path %v resolved to %T, want object", tc.nestedPath, nested)
				}
				gotNested := sortedMapKeys(nestedMap)
				for _, key := range tc.nestedKeys {
					if _, ok := nestedMap[key]; !ok {
						t.Errorf("missing nested key %q at path %v — JSON contract drift in row shape. Got: %v",
							key, tc.nestedPath, gotNested)
					}
				}
			}
		})
	}
}

// sortedMapKeys returns the top-level keys of a JSON object in stable order.
// Used in failure diagnostics so missing-key errors print a deterministic
// "got" set across test runs.
func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// walkPath descends into a decoded JSON object via a path of keys / array
// indices. Numeric strings (e.g. "0") select array indices; other strings
// select object keys. Returns nil and emits a t.Errorf if the path is bad.
//
// Kept tiny on purpose — this is test scaffolding, not a path library.
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
			for j, c := range []byte(seg) {
				if j == 0 && c == '-' {
					t.Errorf("walkPath: negative index %q at path %v", seg, path)
					return nil
				}
				if c < '0' || c > '9' {
					t.Errorf("walkPath: non-numeric array index %q at path %v", seg, path)
					return nil
				}
			}
			// Manual atoi to avoid a strconv import for the trivial case.
			idx := 0
			for _, c := range []byte(seg) {
				idx = idx*10 + int(c-'0')
			}
			if idx >= len(v) {
				t.Errorf("walkPath: index %d out of range (len=%d) at path %v", idx, len(v), path)
				return nil
			}
			cur = v[idx]
		default:
			t.Errorf("walkPath: cannot descend into %T at index %d in path %v", cur, i, path)
			return nil
		}
	}
	return cur
}
