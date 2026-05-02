// Package asc — typed row models + TSV decoders for /v1/salesReports and
// /v1/financeReports.
//
// Apple's Sales/Finance Reports endpoints respond with content-type
// "application/a-gzip" carrying a tab-separated-values payload — NOT
// comma-separated, despite what some Apple docs suggest. The 2A.1 wrapper
// gunzips transparently; this file decodes the resulting bytes into typed
// Go structs the cmd-layer can render as table or JSON.
//
// Column order matches Apple's iTunes Connect Sales and Trends Reporting
// Guide. Apple occasionally adds columns at the right end of the row; the
// decoders are header-driven (not positional) so a future column addition
// produces zero-value fields rather than a parse error.

package asc

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Sales report row
// ---------------------------------------------------------------------------

// SalesReportRow is one row of a SALES / SUBSCRIPTION / SUBSCRIPTION_EVENT
// report decoded from Apple's TSV stream. Field tags match the JSON contract
// the cmd-layer renders; column-header mapping happens inside DecodeSalesTSV
// keyed by the literal Apple header strings.
//
// Numeric units / proceeds are int / float64 because Apple's TSV uses
// integer units and decimal proceeds; missing or blank cells become zero.
// All other fields stay strings — Apple reports sometimes carry locale-
// specific punctuation in numeric-looking columns (Customer Price), so the
// caller decides whether to parse further.
type SalesReportRow struct {
	Provider              string  `json:"provider,omitempty"`
	ProviderCountry       string  `json:"providerCountry,omitempty"`
	SKU                   string  `json:"sku,omitempty"`
	Developer             string  `json:"developer,omitempty"`
	Title                 string  `json:"title,omitempty"`
	Version               string  `json:"version,omitempty"`
	ProductTypeIdentifier string  `json:"productTypeIdentifier,omitempty"`
	Units                 int     `json:"units"`
	DeveloperProceeds     float64 `json:"developerProceeds"`
	BeginDate             string  `json:"beginDate,omitempty"`
	EndDate               string  `json:"endDate,omitempty"`
	CustomerCurrency      string  `json:"customerCurrency,omitempty"`
	CountryCode           string  `json:"countryCode,omitempty"`
	CurrencyOfProceeds    string  `json:"currencyOfProceeds,omitempty"`
	AppleIdentifier       string  `json:"appleIdentifier,omitempty"`
	CustomerPrice         string  `json:"customerPrice,omitempty"`
	PromoCode             string  `json:"promoCode,omitempty"`
	ParentIdentifier      string  `json:"parentIdentifier,omitempty"`
	Subscription          string  `json:"subscription,omitempty"`
	Period                string  `json:"period,omitempty"`
	Category              string  `json:"category,omitempty"`
	CMB                   string  `json:"cmb,omitempty"`
	Device                string  `json:"device,omitempty"`
	SupportedPlatforms    string  `json:"supportedPlatforms,omitempty"`
	ProceedsReason        string  `json:"proceedsReason,omitempty"`
	PreservedPricing      string  `json:"preservedPricing,omitempty"`
	Client                string  `json:"client,omitempty"`
	OrderType             string  `json:"orderType,omitempty"`
}

// salesHeaderAlias maps the literal Apple TSV header strings to the
// SalesReportRow field they populate. Built once at package init so
// DecodeSalesTSV can do O(1) header-by-name lookups instead of a giant
// switch in the hot loop.
//
// Spelling matches Apple's published header line exactly, including
// capitalization and spacing. Missing entries render the column as a
// dropped field (forward-compat: Apple adds new columns over time).
var salesHeaderAlias = map[string]string{
	"Provider":                "Provider",
	"Provider Country":        "ProviderCountry",
	"SKU":                     "SKU",
	"Developer":               "Developer",
	"Title":                   "Title",
	"Version":                 "Version",
	"Product Type Identifier": "ProductTypeIdentifier",
	"Units":                   "Units",
	"Developer Proceeds":      "DeveloperProceeds",
	"Begin Date":              "BeginDate",
	"End Date":                "EndDate",
	"Customer Currency":       "CustomerCurrency",
	"Country Code":            "CountryCode",
	"Currency of Proceeds":    "CurrencyOfProceeds",
	"Apple Identifier":        "AppleIdentifier",
	"Customer Price":          "CustomerPrice",
	"Promo Code":              "PromoCode",
	"Parent Identifier":       "ParentIdentifier",
	"Subscription":            "Subscription",
	"Period":                  "Period",
	"Category":                "Category",
	"CMB":                     "CMB",
	"Device":                  "Device",
	"Supported Platforms":     "SupportedPlatforms",
	"Proceeds Reason":         "ProceedsReason",
	"Preserved Pricing":       "PreservedPricing",
	"Client":                  "Client",
	"Order Type":              "OrderType",
}

// ---------------------------------------------------------------------------
// Finance report row
// ---------------------------------------------------------------------------

// FinanceReportRow is one row of a FINANCIAL / FINANCE_DETAIL report.
// Field meanings mirror Apple's "App Store Connect Payments and Financial
// Reports Guide". As with sales rows, numeric quantity/share fields are
// typed; everything else stays string.
type FinanceReportRow struct {
	StartDate              string  `json:"startDate,omitempty"`
	EndDate                string  `json:"endDate,omitempty"`
	UPC                    string  `json:"upc,omitempty"`
	ISRC                   string  `json:"isrc,omitempty"`
	VendorIdentifier       string  `json:"vendorIdentifier,omitempty"`
	Quantity               int     `json:"quantity"`
	PartnerShare           float64 `json:"partnerShare"`
	ExtendedPartnerShare   float64 `json:"extendedPartnerShare"`
	PartnerShareCurrency   string  `json:"partnerShareCurrency,omitempty"`
	SalesOrReturn          string  `json:"salesOrReturn,omitempty"`
	AppleIdentifier        string  `json:"appleIdentifier,omitempty"`
	ArtistShowDeveloper    string  `json:"artistShowDeveloperAuthor,omitempty"`
	Title                  string  `json:"title,omitempty"`
	LabelStudioNetwork     string  `json:"labelStudioNetworkDeveloperPublisher,omitempty"`
	Grid                   string  `json:"grid,omitempty"`
	ProductTypeIdentifier  string  `json:"productTypeIdentifier,omitempty"`
	ISANOtherIdentifier    string  `json:"isanOtherIdentifier,omitempty"`
	CountryOfSale          string  `json:"countryOfSale,omitempty"`
	PreOrderFlag           string  `json:"preOrderFlag,omitempty"`
	PromoCode              string  `json:"promoCode,omitempty"`
	CustomerPrice          string  `json:"customerPrice,omitempty"`
	CustomerCurrency       string  `json:"customerCurrency,omitempty"`
	AssetContentFlavor     string  `json:"assetContentFlavor,omitempty"`
	VendorOfferCode        string  `json:"vendorOfferCode,omitempty"`
	GracePeriod            string  `json:"gracePeriod,omitempty"`
	StandardSubscriptionTy string  `json:"standardSubscriptionType,omitempty"`
}

// financeHeaderAlias mirrors salesHeaderAlias for the finance report. The
// key is the literal Apple-published header string; the value is the
// SalesReportRow / FinanceReportRow field name (matched via reflection-free
// dispatch in the decoder).
var financeHeaderAlias = map[string]string{
	"Start Date":                   "StartDate",
	"End Date":                     "EndDate",
	"UPC":                          "UPC",
	"ISRC":                         "ISRC",
	"Vendor Identifier":            "VendorIdentifier",
	"Quantity":                     "Quantity",
	"Partner Share":                "PartnerShare",
	"Extended Partner Share":       "ExtendedPartnerShare",
	"Partner Share Currency":       "PartnerShareCurrency",
	"Sales or Return":              "SalesOrReturn",
	"Apple Identifier":             "AppleIdentifier",
	"Artist/Show/Developer/Author": "ArtistShowDeveloper",
	"Title":                        "Title",
	"Label/Studio/Network/Developer/Publisher": "LabelStudioNetwork",
	"Grid":                       "Grid",
	"Product Type Identifier":    "ProductTypeIdentifier",
	"ISAN/Other Identifier":      "ISANOtherIdentifier",
	"Country Of Sale":            "CountryOfSale",
	"Pre-order Flag":             "PreOrderFlag",
	"Promo Code":                 "PromoCode",
	"Customer Price":             "CustomerPrice",
	"Customer Currency":          "CustomerCurrency",
	"Asset/Content Flavor":       "AssetContentFlavor",
	"Vendor Offer Code":          "VendorOfferCode",
	"Grace Period":               "GracePeriod",
	"Standard Subscription Type": "StandardSubscriptionTy",
}

// ---------------------------------------------------------------------------
// Decoders
// ---------------------------------------------------------------------------

// DecodeSalesTSV parses Apple's gunzipped sales-report TSV bytes into
// typed rows. Returns an empty slice (never nil) when the body is the
// header-only "no sales" response Apple produces for empty days.
//
// The decoder is header-driven: column order is read from the first row,
// then each subsequent data row is mapped by header name. Unknown columns
// are silently dropped (forward-compat with new Apple columns); missing
// columns leave the corresponding struct field at its zero value.
//
// Whitespace around cells is preserved — Apple's TSV is whitespace-clean,
// and trimming risks munging fields that legitimately end with a space.
func DecodeSalesTSV(b []byte) ([]SalesReportRow, error) {
	rows, err := readTSV(b)
	if err != nil {
		return nil, fmt.Errorf("asc: decode sales TSV: %w", err)
	}
	if len(rows) == 0 {
		return []SalesReportRow{}, nil
	}
	headers := rows[0]
	out := make([]SalesReportRow, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		var row SalesReportRow
		if err := assignSalesRow(&row, headers, rows[i]); err != nil {
			return nil, fmt.Errorf("asc: decode sales TSV row %d: %w", i, err)
		}
		out = append(out, row)
	}
	return out, nil
}

// DecodeFinanceTSV parses Apple's gunzipped finance-report TSV bytes into
// typed rows. Same forward-compat semantics as DecodeSalesTSV.
//
// Apple's finance reports prepend a few "Total" rows at the bottom of the
// file in some accounts; the decoder treats those as ordinary data rows
// (the "Sales or Return" cell is empty, so the column-typed renderer
// just shows the totals). Callers that want strict per-transaction views
// can filter on SalesOrReturn != "".
func DecodeFinanceTSV(b []byte) ([]FinanceReportRow, error) {
	rows, err := readTSV(b)
	if err != nil {
		return nil, fmt.Errorf("asc: decode finance TSV: %w", err)
	}
	if len(rows) == 0 {
		return []FinanceReportRow{}, nil
	}
	headers := rows[0]
	out := make([]FinanceReportRow, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		var row FinanceReportRow
		if err := assignFinanceRow(&row, headers, rows[i]); err != nil {
			return nil, fmt.Errorf("asc: decode finance TSV row %d: %w", i, err)
		}
		out = append(out, row)
	}
	return out, nil
}

// readTSV parses Apple's TSV bytes. csv.Reader with Comma='\t' handles the
// wire format faithfully, including embedded newlines inside quoted cells
// (rare but documented for review-note style fields).
//
// Returns an empty slice when the input is empty or whitespace-only — this
// is the "no sales today" shape Apple sends.
func readTSV(b []byte) ([][]string, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	r := csv.NewReader(bytes.NewReader(b))
	r.Comma = '\t'
	r.LazyQuotes = true
	// Apple's TSV is variable-width on legitimate boundaries (older months
	// have fewer columns than newer months). Disable the strict count check
	// so we don't reject on schema drift mid-file.
	r.FieldsPerRecord = -1

	var out [][]string
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// salesSetter is one column-assigner: writes the raw cell into the right
// SalesReportRow field, returning an error if numeric parsing fails. Per-
// column setters are tiny (1 statement each) so the gocyclo budget stays
// well under the project's ceiling-of-15 instead of consuming it on a
// single ~30-arm switch.
type salesSetter func(row *SalesReportRow, raw string) error

// salesSetters maps the SalesReportRow field name (the value side of
// salesHeaderAlias) to its setter. Adding a column = add a header alias
// entry + a setter entry; the dispatcher stays unchanged.
var salesSetters = map[string]salesSetter{
	"Provider":              func(r *SalesReportRow, v string) error { r.Provider = v; return nil },
	"ProviderCountry":       func(r *SalesReportRow, v string) error { r.ProviderCountry = v; return nil },
	"SKU":                   func(r *SalesReportRow, v string) error { r.SKU = v; return nil },
	"Developer":             func(r *SalesReportRow, v string) error { r.Developer = v; return nil },
	"Title":                 func(r *SalesReportRow, v string) error { r.Title = v; return nil },
	"Version":               func(r *SalesReportRow, v string) error { r.Version = v; return nil },
	"ProductTypeIdentifier": func(r *SalesReportRow, v string) error { r.ProductTypeIdentifier = v; return nil },
	"Units": func(r *SalesReportRow, v string) error {
		n, err := parseInt(v)
		if err != nil {
			return err
		}
		r.Units = n
		return nil
	},
	"DeveloperProceeds": func(r *SalesReportRow, v string) error {
		f, err := parseFloat(v)
		if err != nil {
			return err
		}
		r.DeveloperProceeds = f
		return nil
	},
	"BeginDate":          func(r *SalesReportRow, v string) error { r.BeginDate = v; return nil },
	"EndDate":            func(r *SalesReportRow, v string) error { r.EndDate = v; return nil },
	"CustomerCurrency":   func(r *SalesReportRow, v string) error { r.CustomerCurrency = v; return nil },
	"CountryCode":        func(r *SalesReportRow, v string) error { r.CountryCode = v; return nil },
	"CurrencyOfProceeds": func(r *SalesReportRow, v string) error { r.CurrencyOfProceeds = v; return nil },
	"AppleIdentifier":    func(r *SalesReportRow, v string) error { r.AppleIdentifier = v; return nil },
	"CustomerPrice":      func(r *SalesReportRow, v string) error { r.CustomerPrice = v; return nil },
	"PromoCode":          func(r *SalesReportRow, v string) error { r.PromoCode = v; return nil },
	"ParentIdentifier":   func(r *SalesReportRow, v string) error { r.ParentIdentifier = v; return nil },
	"Subscription":       func(r *SalesReportRow, v string) error { r.Subscription = v; return nil },
	"Period":             func(r *SalesReportRow, v string) error { r.Period = v; return nil },
	"Category":           func(r *SalesReportRow, v string) error { r.Category = v; return nil },
	"CMB":                func(r *SalesReportRow, v string) error { r.CMB = v; return nil },
	"Device":             func(r *SalesReportRow, v string) error { r.Device = v; return nil },
	"SupportedPlatforms": func(r *SalesReportRow, v string) error { r.SupportedPlatforms = v; return nil },
	"ProceedsReason":     func(r *SalesReportRow, v string) error { r.ProceedsReason = v; return nil },
	"PreservedPricing":   func(r *SalesReportRow, v string) error { r.PreservedPricing = v; return nil },
	"Client":             func(r *SalesReportRow, v string) error { r.Client = v; return nil },
	"OrderType":          func(r *SalesReportRow, v string) error { r.OrderType = v; return nil },
}

// assignSalesRow dispatches header names to SalesReportRow setters. Avoids
// reflection costs of a tag-driven decoder; per-column lookup is a single
// map probe so the hot loop stays straight-line.
func assignSalesRow(row *SalesReportRow, headers, cells []string) error {
	for i, h := range headers {
		if i >= len(cells) {
			break
		}
		field := salesHeaderAlias[h]
		if field == "" {
			continue
		}
		set, ok := salesSetters[field]
		if !ok {
			continue
		}
		if err := set(row, cells[i]); err != nil {
			return fmt.Errorf("column %q: %w", h, err)
		}
	}
	return nil
}

// financeSetter mirrors salesSetter for finance rows.
type financeSetter func(row *FinanceReportRow, raw string) error

// financeSetters mirrors salesSetters for the finance row's columns.
var financeSetters = map[string]financeSetter{
	"StartDate":        func(r *FinanceReportRow, v string) error { r.StartDate = v; return nil },
	"EndDate":          func(r *FinanceReportRow, v string) error { r.EndDate = v; return nil },
	"UPC":              func(r *FinanceReportRow, v string) error { r.UPC = v; return nil },
	"ISRC":             func(r *FinanceReportRow, v string) error { r.ISRC = v; return nil },
	"VendorIdentifier": func(r *FinanceReportRow, v string) error { r.VendorIdentifier = v; return nil },
	"Quantity": func(r *FinanceReportRow, v string) error {
		n, err := parseInt(v)
		if err != nil {
			return err
		}
		r.Quantity = n
		return nil
	},
	"PartnerShare": func(r *FinanceReportRow, v string) error {
		f, err := parseFloat(v)
		if err != nil {
			return err
		}
		r.PartnerShare = f
		return nil
	},
	"ExtendedPartnerShare": func(r *FinanceReportRow, v string) error {
		f, err := parseFloat(v)
		if err != nil {
			return err
		}
		r.ExtendedPartnerShare = f
		return nil
	},
	"PartnerShareCurrency":   func(r *FinanceReportRow, v string) error { r.PartnerShareCurrency = v; return nil },
	"SalesOrReturn":          func(r *FinanceReportRow, v string) error { r.SalesOrReturn = v; return nil },
	"AppleIdentifier":        func(r *FinanceReportRow, v string) error { r.AppleIdentifier = v; return nil },
	"ArtistShowDeveloper":    func(r *FinanceReportRow, v string) error { r.ArtistShowDeveloper = v; return nil },
	"Title":                  func(r *FinanceReportRow, v string) error { r.Title = v; return nil },
	"LabelStudioNetwork":     func(r *FinanceReportRow, v string) error { r.LabelStudioNetwork = v; return nil },
	"Grid":                   func(r *FinanceReportRow, v string) error { r.Grid = v; return nil },
	"ProductTypeIdentifier":  func(r *FinanceReportRow, v string) error { r.ProductTypeIdentifier = v; return nil },
	"ISANOtherIdentifier":    func(r *FinanceReportRow, v string) error { r.ISANOtherIdentifier = v; return nil },
	"CountryOfSale":          func(r *FinanceReportRow, v string) error { r.CountryOfSale = v; return nil },
	"PreOrderFlag":           func(r *FinanceReportRow, v string) error { r.PreOrderFlag = v; return nil },
	"PromoCode":              func(r *FinanceReportRow, v string) error { r.PromoCode = v; return nil },
	"CustomerPrice":          func(r *FinanceReportRow, v string) error { r.CustomerPrice = v; return nil },
	"CustomerCurrency":       func(r *FinanceReportRow, v string) error { r.CustomerCurrency = v; return nil },
	"AssetContentFlavor":     func(r *FinanceReportRow, v string) error { r.AssetContentFlavor = v; return nil },
	"VendorOfferCode":        func(r *FinanceReportRow, v string) error { r.VendorOfferCode = v; return nil },
	"GracePeriod":            func(r *FinanceReportRow, v string) error { r.GracePeriod = v; return nil },
	"StandardSubscriptionTy": func(r *FinanceReportRow, v string) error { r.StandardSubscriptionTy = v; return nil },
}

// assignFinanceRow mirrors assignSalesRow for finance rows.
func assignFinanceRow(row *FinanceReportRow, headers, cells []string) error {
	for i, h := range headers {
		if i >= len(cells) {
			break
		}
		field := financeHeaderAlias[h]
		if field == "" {
			continue
		}
		set, ok := financeSetters[field]
		if !ok {
			continue
		}
		if err := set(row, cells[i]); err != nil {
			return fmt.Errorf("column %q: %w", h, err)
		}
	}
	return nil
}

// parseInt is the lenient int parser used for cells that may be blank.
// Empty / whitespace-only → 0; otherwise strconv.Atoi.
func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse int %q: %w", s, err)
	}
	return v, nil
}

// parseFloat is the lenient float parser. Apple's TSV uses "." as the
// decimal separator regardless of locale, so we don't need locale-aware
// parsing here.
func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", s, err)
	}
	return v, nil
}
