// Sales/Finance reports arrive as gzipped TSV (not CSV); decoders are header-driven so new trailing columns don't break parsing.
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

// SalesReportRow is one row of a SALES/SUBSCRIPTION/SUBSCRIPTION_EVENT report.
// Non-numeric fields stay string because Apple's TSV column set varies by report.
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

// salesHeaderAlias maps Apple's published TSV header strings to SalesReportRow fields.
// Keys must match Apple's spelling exactly; unmapped columns drop silently (forward-compat).
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

// FinanceReportRow is one row of a FINANCIAL/FINANCE_DETAIL report.
// Non-numeric fields stay string because Apple's TSV column set varies by report.
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

// financeHeaderAlias mirrors salesHeaderAlias for the finance report.
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

// DecodeSalesTSV parses Apple's gunzipped sales-report TSV into typed rows.
// Returns empty (never nil) on no-sales days; unknown columns drop silently.
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

// DecodeFinanceTSV parses Apple's gunzipped finance-report TSV into typed rows.
// Apple's trailing "Total" summary rows decode as-is; filter on SalesOrReturn != "" for per-transaction views.
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

// readTSV parses Apple's TSV bytes; whitespace-only input is the "no sales today" shape Apple sends.
func readTSV(b []byte) ([][]string, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	r := csv.NewReader(bytes.NewReader(b))
	r.Comma = '\t'
	r.LazyQuotes = true
	// Apple's TSV is legitimately variable-width across months; disable the strict count check.
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

// salesSetter assigns one raw cell into its SalesReportRow field.
// Per-column setters keep gocyclo off a single ~30-arm switch.
type salesSetter func(row *SalesReportRow, raw string) error

// salesSetters maps SalesReportRow field names (salesHeaderAlias values) to their setters.
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

// assignSalesRow dispatches header names to SalesReportRow setters.
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

// parseInt parses an int, treating blank cells as 0.
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

// parseFloat parses a float, treating blank cells as 0.
// Apple's TSV always uses "." as the decimal separator regardless of locale.
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
