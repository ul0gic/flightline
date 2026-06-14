package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/lint"
	"github.com/ul0gic/flightline/internal/plan"
)

func tblBool(b bool) *bool { return &b }

// TestTableRows_AllRenderables drives every TableRenderable through the
// production renderTo("table") pipeline; row counts derive from each TableRows().
func TestTableRows_AllRenderables(t *testing.T) {
	cases := []struct {
		name string
		view any
	}{
		{"apps list", AppList{Apps: []AppView{{
			ID: "app1", Type: "apps",
			Attributes: AppAttributes{BundleID: "com.example.app", Name: "Example App", SKU: "SKU123"},
		}}}},
		{"apps get", &AppView{
			ID: "app1", Type: "apps",
			Attributes: AppAttributes{
				BundleID: "com.example.app", Name: "Example App", SKU: "SKU123",
				PrimaryLocale: "en-US", ContentRightsDeclaration: "UNMODIFIED",
			},
		}},

		{"versions list", VersionList{Versions: []VersionView{{
			ID: "ver1", Type: "appStoreVersions",
			Attributes: asc.VersionAttributes{
				VersionString: "1.0.0", Platform: "IOS",
				AppVersionState: "READY_FOR_SALE", ReleaseType: "MANUAL",
			},
		}}}},
		{"versions get", &VersionView{
			ID: "ver1", Type: "appStoreVersions",
			Attributes: asc.VersionAttributes{
				VersionString: "1.0.0", Platform: "IOS",
				AppVersionState: "READY_FOR_SALE", ReleaseType: "MANUAL",
				ReviewType: "APP_STORE", Copyright: "(c) 2025 Example",
				Downloadable: tblBool(true),
			},
		}},
		{"versions write", &VersionWriteResult{
			Action: "created", Changed: true,
			Version: VersionView{
				ID: "ver1", Type: "appStoreVersions",
				Attributes: asc.VersionAttributes{
					VersionString: "1.0.0", Platform: "IOS",
					AppVersionState: "READY_FOR_SALE", ReleaseType: "MANUAL",
					ReviewType: "APP_STORE", Copyright: "(c) 2025 Example",
				},
			},
		}},

		{"builds list", BuildList{Builds: []BuildView{{
			ID: "bld1", Type: "builds",
			Attributes: asc.BuildAttributes{
				Version: "42", ProcessingState: "PROCESSING",
				Expired: tblBool(false), UploadedDate: "2025-01-15T10:00:00Z",
			},
		}}}},
		{"builds get", &BuildView{
			ID: "bld1", Type: "builds",
			Attributes: asc.BuildAttributes{
				Version: "42", ProcessingState: "PROCESSING",
				Expired: tblBool(false), ExpirationDate: "2025-02-15T10:00:00Z",
				UploadedDate: "2025-01-15T10:00:00Z", MinOsVersion: "14.0",
				UsesNonExemptEncryption: tblBool(false), BuildAudienceType: "INTERNAL",
			},
		}},
		{"builds attach", &BuildAttachResult{
			Action: "attached", Changed: true, Version: "1.0.0",
			VersionID: "ver1", Build: "42", BuildID: "bld1", Platform: "IOS",
		}},

		{"review-submissions list", ReviewSubmissionList{Submissions: []ReviewSubmissionView{{
			ID: "sub1", Type: "reviewSubmissions",
			Attributes: asc.ReviewSubmissionAttributes{
				State: "WAITING_FOR_REVIEW", Platform: "IOS", SubmittedDate: "2025-01-15T10:00:00Z",
			},
		}}}},
		{"review-submissions items", ReviewSubmissionItemList{Items: []ReviewSubmissionItemView{{
			ID: "item1", Type: "reviewSubmissionItems",
			Attributes:    asc.ReviewSubmissionItemAttributes{State: "ACCEPTED"},
			ReferenceType: "appStoreVersions", ReferenceID: "ver1",
		}}}},

		{"rejection", RejectionReport{
			BundleID: "com.example.app",
			Version: RejectionVersion{
				VersionString: "1.0.0", Platform: "IOS",
				State: "REJECTED", ReleaseType: "MANUAL",
			},
			Note: "no submission",
		}},

		{"iap list", IAPList{IAPs: []IAPView{{
			ID: "iap1", Type: "inAppPurchases",
			Attributes: asc.IAPAttributes{
				ProductID: "com.example.app.coins", Name: "Coins",
				InAppPurchaseType: "CONSUMABLE", State: "APPROVED",
			},
		}}}},
		{"iap get", &IAPView{
			ID: "iap1", Type: "inAppPurchases",
			Attributes: asc.IAPAttributes{
				ProductID: "com.example.app.coins", Name: "Coins",
				InAppPurchaseType: "CONSUMABLE", State: "APPROVED",
				ReviewNote: "Currency", FamilySharable: tblBool(true), ContentHosting: tblBool(false),
			},
			ReviewScreenshotURL: "https://example.com/shot.png",
		}},
		{"iap localizations list", IAPLocalizationList{Localizations: []IAPLocalizationView{{
			ID: "locl1", Type: "inAppPurchaseLocalizations",
			Attributes: asc.IAPLocalizationAttributes{Locale: "en-US", Name: "Coins", State: "APPROVED"},
		}}}},

		{"iap write", &IAPWriteResult{
			Action: "create", ID: "iap1", Type: "inAppPurchases",
			ProductID: "com.example.app.coins", NoOp: false,
			Attributes: asc.IAPAttributes{
				Name: "Coins", InAppPurchaseType: "CONSUMABLE",
				State: "MISSING_METADATA", ReviewNote: "Currency", FamilySharable: tblBool(true),
			},
		}},
		{"iap localization write", &IAPLocalizationWriteResult{
			Action: "create", ID: "locl1", Type: "inAppPurchaseLocalizations", NoOp: false,
			Attributes: asc.IAPLocalizationAttributes{
				Locale: "en-US", Name: "Coins", Description: "In-game currency", State: "PREPARE_FOR_SUBMISSION",
			},
		}},
		{"iap screenshot upload", &IAPScreenshotUploadResult{
			Action: "upload", ID: "shot1", Type: "inAppPurchaseAppStoreReviewScreenshots",
			IAPID: "iap1", ProductID: "com.example.app.coins", FileName: "screenshot.png",
			Checksum: "abc123", NoOp: false, TemplateURL: "https://example.com/{w}x{h}{f}",
		}},

		{"age-rating get", &AgeRatingView{
			ID: "ageRating123", Type: "ageRatingDeclarations",
			Attributes: asc.AgeRatingDeclarationAttributes{
				AlcoholTobaccoOrDrugUseOrReferences: "INFREQUENT_OR_MILD",
			},
		}},
		{"age-rating write", &AgeRatingWriteResult{
			Action: "set", ID: "ageRating123", Type: "ageRatingDeclarations",
			BundleID: "com.example.app", Version: "1.0.0", VersionState: "READY_FOR_REVIEW",
			NoOp: false, ChangedKeys: []string{"advertising"},
		}},

		{"export-compliance get", &ExportComplianceView{
			BundleID: "com.example.app", VersionString: "1.0.0",
			Build: asc.BuildEncryptionView{
				BuildID: "build123", BuildVersion: "123", UsesNonExemptEncryption: tblBool(true),
			},
			Declarations: []EncryptionDeclarationView{{
				ID: "decl123", Type: "appEncryptionDeclarations",
				Attributes: asc.AppEncryptionDeclarationAttributes{AppEncryptionDeclarationState: "APPROVED"},
			}},
		}},
		{"export-compliance write", &ExportComplianceWriteResult{
			Action: "set", BundleID: "com.example.app", VersionString: "1.0.0",
			BuildID: "build123", BuildVersion: "123", UsesNonExemptEncryption: tblBool(true), NoOp: false,
		}},

		{"privacy-labels get", &PrivacyLabelsView{
			BundleID: "com.example.app", Supported: false,
			Reason: "v4.3 stub", Reference: "https://developer.apple.com/",
		}},

		{"territories list", TerritoryList{Territories: []TerritoryView{{
			ID: "USA", Type: "territories",
			Attributes: asc.TerritoryAttributes{Currency: "USD"},
		}}}},

		{"categories list", CategoryList{Categories: []CategoryView{{
			ID: "GAMES", Type: "appCategories",
			Attributes: asc.AppCategoryAttributes{Platforms: []string{"IOS"}},
		}}}},
		{"categories get", &CategoryAssignmentView{
			BundleID: "com.example.app", AppInfoID: "appInfo123",
			AppInfoState: "PREPARE_FOR_SUBMISSION", PrimaryCategory: "GAMES", PrimarySubcategoryOne: "ACTION",
		}},
		{"categories set", &CategoriesSetResult{
			BundleID: "com.example.app", AppInfoID: "appInfo123",
			AppInfoState: "PREPARE_FOR_SUBMISSION", Changed: true,
			Changes: []CategoriesFieldChange{{Field: "primaryCategory", From: "PRODUCTIVITY", To: "GAMES"}},
		}},

		{"pricing get", &PricingView{
			BundleID: "com.example.app",
			Schedule: PriceScheduleSummary{
				ID: "sched123", BaseTerritoryID: "USA", BaseCurrency: "USD",
				ManualPriceCount: 1, AutomaticPriceCount: 0,
			},
			BasePrice: &PricePointSummary{
				Currency: "USD", CustomerPrice: "4.99", Proceeds: "3.50", StartDate: "2024-01-01",
			},
			Availability: AvailabilitySummary{ID: "avail123", AvailableTotal: 155, AvailableCount: 155},
		}},
		{"pricing set", &PricingSetResult{
			BundleID: "com.example.app", AppID: "app123", Changed: true,
			BaseTerritory: "USA", PricePointID: "PP-USA-999",
			ScheduleID: "sched456", PreviousScheduleID: "sched123",
		}},

		{"testflight groups list", BetaGroupList{Groups: []BetaGroupView{{
			ID: "BG-123", Type: "betaGroups",
			Attributes: asc.BetaGroupAttributes{Name: "Internal Testers", IsInternalGroup: tblBool(true)},
		}}}},
		{"testflight testers list", BetaTesterList{Testers: []BetaTesterView{{
			ID: "T-123", Type: "betaTesters",
			Attributes: asc.BetaTesterAttributes{
				Email: "tester@example.com", FirstName: "John", LastName: "Doe",
				InviteType: "EMAIL", State: "ACCEPTED",
			},
		}}}},
		{"testflight beta-review get", &BetaReviewView{
			BundleID: "com.example.app", BuildID: "build123", BuildNumber: "42",
			ID: "submission123", Type: "betaAppReviewSubmissions",
			Attributes: asc.BetaAppReviewSubmissionAttributes{
				BetaReviewState: "APPROVED", SubmittedDate: "2024-01-15",
			},
		}},
		{"testflight group set", &BetaGroupSetResult{
			GroupID: "BG-123", Changed: true, Created: true,
			Attributes: asc.BetaGroupAttributes{
				Name: "New Group", IsInternalGroup: tblBool(false),
				PublicLink: "https://testflight.apple.com/join/abc123", FeedbackEnabled: tblBool(true),
			},
		}},
		{"testflight group delete", &BetaGroupDeleteResult{GroupID: "BG-123", Changed: true}},
		{"testflight testers change", &BetaTestersChangeResult{
			GroupID: "BG-123", Changed: true, Action: "add",
			Requested: []string{"T-1", "T-2"}, Applied: []string{"T-1"}, Skipped: []string{"T-2"},
		}},
		{"testflight beta-review submit", &BetaReviewSubmitResult{
			BundleID: "com.example.app", BuildID: "build123", BuildNumber: "42",
			SubmissionID: "submission456", Changed: true,
			Attributes: asc.BetaAppReviewSubmissionAttributes{
				BetaReviewState: "WAITING_FOR_REVIEW", SubmittedDate: "2024-01-16",
			},
		}},

		{"custom-product-pages list", CustomProductPageList{Pages: []CustomProductPageView{{
			ID: "id1", Type: "appCustomProductPages", CurrentVersion: "1", CurrentState: "APPROVED",
			Attributes: asc.AppCustomProductPageAttributes{Name: "page", Visible: tblBool(true)},
		}}}},
		{"custom-product-pages get", &CustomProductPageDetail{
			ID:         "id1",
			Attributes: asc.AppCustomProductPageAttributes{Name: "page", URL: "url", Visible: tblBool(true)},
			Versions: []CustomProductPageVersionView{{
				Attributes: asc.AppCustomProductPageVersionAttributes{Version: "1", State: "APPROVED"},
			}},
			Localizations: []CustomProductPageLocalizationView{{
				Attributes: asc.AppCustomProductPageLocalizationAttributes{Locale: "en-US", PromotionalText: "text"},
			}},
		}},
		{"custom-product-pages set", &CustomProductPageSetResult{
			PageID: "id1", Changed: true, Created: true,
			Attributes: asc.AppCustomProductPageAttributes{Name: "page", Visible: tblBool(true)},
			Note:       "created",
		}},
		{"custom-product-pages delete", &CustomProductPageDeleteResult{
			PageID: "id1", Changed: true, Note: "deleted",
		}},

		{"reviews list", ReviewList{Reviews: []ReviewView{{
			ID: "rev1",
			Attributes: asc.CustomerReviewAttributes{
				Rating: 5, CreatedDate: "2026-01-01T00:00:00Z", Territory: "USA", Title: "Great",
			},
		}}}},
		{"reviews get", &ReviewView{
			ID: "rev1",
			Attributes: asc.CustomerReviewAttributes{
				Rating: 5, Title: "Great", ReviewerNickname: "user",
				Territory: "USA", CreatedDate: "2026-01-01T00:00:00Z", Body: "body",
			},
			Response: &ReviewResponseView{
				ID: "resp1",
				Attributes: asc.CustomerReviewResponseAttributes{
					State: "PUBLISHED", LastModifiedDate: "2026-01-02T00:00:00Z", ResponseBody: "response",
				},
			},
		}},
		{"reviews summary", &ReviewSummaryView{
			BundleID: "com.example.app",
			Summarizations: []ReviewSummarizationItem{{
				Attributes: asc.CustomerReviewSummarizationAttributes{
					Platform: "ios", Locale: "en-US", CreatedDate: "2026-01-01T00:00:00Z", Text: "summary text",
				},
			}},
		}},

		{"beta-feedback crash", BetaFeedbackCrashList{Submissions: []BetaFeedbackCrashView{{
			ID: "crash1",
			Attributes: asc.BetaFeedbackCrashSubmissionAttributes{
				BetaFeedbackBaseAttributes: asc.BetaFeedbackBaseAttributes{
					CreatedDate: "2026-01-01T00:00:00Z", DeviceModel: "iPhone15,3", OsVersion: "17.2", Comment: "crashed",
				},
			},
		}}}},
		{"beta-feedback screenshot", BetaFeedbackScreenshotList{Submissions: []BetaFeedbackScreenshotView{{
			ID: "screenshot1",
			Attributes: asc.BetaFeedbackScreenshotSubmissionAttributes{
				BetaFeedbackBaseAttributes: asc.BetaFeedbackBaseAttributes{
					CreatedDate: "2026-01-01T00:00:00Z", DeviceModel: "iPhone15,3", OsVersion: "17.2", Comment: "shot",
				},
				Screenshots: []asc.BetaFeedbackScreenshotImage{{URL: "http://example.com/img.png"}},
			},
		}}}},
		{"beta-feedback download", &BetaFeedbackDownloadView{
			ID: "download1", Type: "crashLog", SavedTo: "/tmp/file.txt", Bytes: 1024,
		}},

		{"diagnostics list", DiagnosticSignatureList{
			BundleID: "com.example.app", BuildID: "build1",
			Signatures: []DiagnosticSignatureView{{
				ID:         "diag1",
				Attributes: asc.DiagnosticSignatureAttributes{Weight: 1.5, DiagnosticType: "HANGS", Signature: "sig"},
			}},
		}},
		{"diagnostics get", &DiagnosticGetView{
			SignatureID: "sig1", Version: "1.0",
			ProductData: []asc.DiagnosticProductData{{
				SignatureID: "sig1",
				DiagnosticLogs: []asc.DiagnosticLogStackTree{{
					DiagnosticMetaData: asc.DiagnosticLogMetaData{
						Event: "hang", BuildVersion: "1", OsVersion: "17", DeviceType: "iPhone",
					},
				}},
			}},
		}},

		{"performance", &PerformanceView{
			BundleID: "com.example.app", BuildNumber: "42", BuildID: "bid1", Version: "1",
			Insights: &asc.PerfPowerMetricInsights{
				Regressions: []asc.PerfPowerInsight{{MetricCategory: "MEMORY", HighImpact: true, SummaryString: "regress"}},
				TrendingUp:  []asc.PerfPowerInsight{{MetricCategory: "BATTERY", HighImpact: false, SummaryString: "trend"}},
			},
			ProductData: []asc.PerfPowerProductData{{
				Platform:         "IOS",
				MetricCategories: []asc.PerfPowerMetricCategory{{Identifier: "memory"}},
			}},
		}},

		{"subscriptions list", SubscriptionGroupList{
			BundleID: "com.example.app",
			Groups: []SubscriptionGroupView{{
				ID: "grp1", MemberCount: 2,
				Attributes: asc.SubscriptionGroupAttributes{ReferenceName: "Pro"},
			}},
		}},
		{"subscriptions get", &SubscriptionDetailView{
			BundleID: "com.example.app", ID: "sub1",
			Attributes: asc.SubscriptionAttributes{
				ProductID: "com.example.pro.monthly", Name: "Pro Monthly", State: "APPROVED",
				SubscriptionPeriod: "ONE_MONTH", GroupLevel: 1, FamilySharable: tblBool(true), ReviewNote: "note",
			},
			Group: &SubscriptionGroupSummary{
				ID: "grp1", Attributes: asc.SubscriptionGroupAttributes{ReferenceName: "Pro"},
			},
			Localizations:      []SubscriptionLocalizationItem{{Attributes: asc.SubscriptionLocalizationAttributes{Name: "Pro"}}},
			IntroductoryOffers: []SubscriptionIntroductoryOfferItem{{Attributes: asc.SubscriptionIntroductoryOfferAttributes{Duration: "P1M"}}},
			Prices:             []SubscriptionPriceItem{{Attributes: asc.SubscriptionPriceAttributes{}}},
		}},

		{"analytics request", AnalyticsRequestView{
			BundleID: "com.example.app", RequestID: "req1", AccessType: "ONE_TIME_SNAPSHOT",
			Status: "queued", SubmittedAt: "2026-01-01T00:00:00Z", LastPollAt: "2026-01-01T01:00:00Z",
			Reports: []asc.PersistedAnalyticsReport{{ID: asc.ReportID("rpt1"), Name: "report"}},
		}},
		{"analytics instances", AnalyticsInstancesView{
			BundleID: "com.example.app", RequestID: "req1",
			Reports: []AnalyticsReportInstancesEntry{{
				Report: asc.PersistedAnalyticsReport{
					ID: asc.ReportID("rpt1"), Name: "report", Category: asc.AnalyticsCategory("APP_USAGE"),
				},
				Instances: []asc.AnalyticsReportInstance{{
					ID: asc.InstanceID("inst1"), Granularity: asc.AnalyticsGranularity("daily"), ProcessingDate: "2026-01-01",
				}},
			}},
		}},
		{"analytics download", AnalyticsDownloadView{
			BundleID: "com.example.app", InstanceID: "inst1",
			Segments: []asc.SegmentDownloadResult{{SegmentID: "seg1", ByteCount: 1024}},
			Files:    []string{"file.csv"},
		}},
		{"analytics status", AnalyticsStatusView{
			BundleID: "com.example.app", StateFile: "/path/file.json", RequestID: "req1",
			Status: "completed", SubmittedAt: "2026-01-01T00:00:00Z", LastPollAt: "2026-01-01T01:00:00Z",
			Reports:    []asc.PersistedAnalyticsReport{{ID: asc.ReportID("rpt1"), Name: "report"}},
			Downloaded: []string{"seg1"},
		}},

		{"finance report", FinanceReport{Summary: []FinanceRegionSummary{{
			CountryOfSale: "US", Currency: "USD", Quantity: 100, PartnerShare: 50.00, ExtendedPartnerShare: 75.00,
		}}}},
		{"sales report", SalesReport{Summary: []SalesDailySummary{{
			Date: "2026-01-01", Units: 10, DeveloperProceeds: 99.99, Currency: "USD",
		}}}},
		{"subscription report", SubscriptionReport{Rows: []asc.SalesReportRow{{
			SKU: "com.example.sub1", BeginDate: "2026-01-01", Units: 5, DeveloperProceeds: 9.99, CurrencyOfProceeds: "USD",
		}}}},

		{"reviewer-demo write", &ReviewerDemoWriteResult{
			Action: "create", ID: "1", Type: "appStoreReviewDetails",
			BundleID: "com.example.app", VersionString: "1.0.0", NoOp: false, DemoAccountPasswordSet: true,
		}},
		{"metadata set", &MetadataSetResult{
			Action: "both", Changed: true,
			Metadata: MetadataView{
				Locale: "en-US", Name: "MyApp", Subtitle: "Slogan", Description: "desc",
				Keywords: "k1, k2", WhatsNew: "fixes", PromotionalText: "promo",
				MarketingURL: "https://example.com", SupportURL: "https://example.com/support",
			},
		}},

		{"lint", &LintResult{Diagnostics: []lint.Diagnostic{{
			Severity: lint.SeverityError, RuleID: "rule1", Path: "/path", Message: "error message",
		}}}},
		{"plan", &PlanResult{Changes: []plan.Change{{
			Op: "create", Path: "/field", From: nil, To: "value",
		}}}},
		{"apply", &ApplyResult{Applied: []plan.Change{{Op: "create", Path: "/field"}}}},

		{"whoami", WhoamiInfo{
			KeyID: "ABC123", IssuerID: "DEF456", VendorNumber: "123456789",
			Authorized: true, APIBaseURL: "https://api.appstoreconnect.apple.com",
		}},
		{"screenshots upload", &ScreenshotUploadResult{Files: []ScreenshotUploadResultEntry{{
			FileName: "shot1.png", Action: "uploaded", MD5: "abc123", AssetID: "asset1",
		}}}},
	}

	// Floor guard: if a case row is deleted, the count drops and the test
	// flags it rather than silently shrinking table coverage.
	if len(cases) < 64 {
		t.Fatalf("test case count = %d, want >= 64: a TableRenderable row was removed", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			renderable, ok := tc.view.(TableRenderable)
			if !ok {
				t.Fatalf("view %T does not implement TableRenderable", tc.view)
			}
			headers, rows := renderable.TableRows()
			if len(headers) == 0 {
				t.Fatalf("TableRows returned no headers for %T: construct a non-empty value", tc.view)
			}

			var buf bytes.Buffer
			if err := renderTo(&buf, tc.view, "table", true); err != nil {
				t.Fatalf("renderTo: %v\nview: %T", err, tc.view)
			}

			assertTable(t, buf.String(), headers, len(rows))
		})
	}
}

// assertTable checks the header line and the data-row count against the
// headers/rows the source TableRows produced.
func assertTable(t *testing.T, out string, headers []string, wantRows int) {
	t.Helper()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("table output too short: %q", out)
	}

	headerCells := splitCols(lines[0])
	if len(headerCells) != len(headers) {
		t.Fatalf("header column count = %d, want %d\nheader line: %q", len(headerCells), len(headers), lines[0])
	}
	for i, h := range headers {
		if headerCells[i] != h {
			t.Errorf("header[%d] = %q, want %q\nheader line: %q", i, headerCells[i], h, lines[0])
		}
	}

	if !strings.HasPrefix(lines[1], "-") {
		t.Errorf("line 1 is not a dashes separator: %q", lines[1])
	}

	gotRows := len(lines) - 2
	if gotRows != wantRows {
		t.Errorf("data row count = %d, want %d\noutput:\n%s", gotRows, wantRows, out)
	}
}

// splitCols recovers a rendered row's cells: columns are separated by runs of
// two-or-more spaces (single spaces inside a cell are preserved).
func splitCols(line string) []string {
	out := make([]string, 0, 8)
	var cell strings.Builder
	spaces := 0
	flush := func() {
		if cell.Len() > 0 {
			out = append(out, cell.String())
			cell.Reset()
		}
	}
	for _, r := range line {
		if r == ' ' {
			spaces++
			continue
		}
		if spaces >= 2 {
			flush()
		} else if spaces == 1 && cell.Len() > 0 {
			cell.WriteByte(' ')
		}
		spaces = 0
		cell.WriteRune(r)
	}
	flush()
	return out
}
