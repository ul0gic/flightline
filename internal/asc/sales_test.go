package asc

import (
	"os"
	"path/filepath"
	"testing"
)

// readTestdata loads a golden TSV from internal/asc/testdata/golden/<class>/<name>.
// Tests live in the same package, so the path is the working-dir-relative
// fixture root rather than the cmd-package detour.
func readTestdata(t *testing.T, rel string) []byte {
	t.Helper()
	path := filepath.Join("testdata", "golden", rel)
	b, err := os.ReadFile(path) //nolint:gosec // test-only path constant
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return b
}

func TestDecodeSalesTSV_DailyBasic(t *testing.T) {
	t.Parallel()
	b := readTestdata(t, "sales/daily_basic.tsv")
	rows, err := DecodeSalesTSV(b)
	if err != nil {
		t.Fatalf("DecodeSalesTSV: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}

	r0 := rows[0]
	if r0.Provider != "APPLE" {
		t.Errorf("rows[0].Provider = %q, want APPLE", r0.Provider)
	}
	if r0.SKU != "com.example.testapp.sku1" {
		t.Errorf("rows[0].SKU = %q", r0.SKU)
	}
	if r0.Units != 5 {
		t.Errorf("rows[0].Units = %d, want 5", r0.Units)
	}
	if r0.DeveloperProceeds != 2.10 {
		t.Errorf("rows[0].DeveloperProceeds = %v, want 2.10", r0.DeveloperProceeds)
	}
	if r0.CountryCode != "US" {
		t.Errorf("rows[0].CountryCode = %q", r0.CountryCode)
	}
	if r0.AppleIdentifier != "1234567890" {
		t.Errorf("rows[0].AppleIdentifier = %q", r0.AppleIdentifier)
	}

	// Subscription row pulls the Subscription + Period columns.
	r3 := rows[3]
	if r3.ProductTypeIdentifier != "IAY" {
		t.Errorf("rows[3].ProductTypeIdentifier = %q, want IAY", r3.ProductTypeIdentifier)
	}
	if r3.Period != "1 Month" {
		t.Errorf("rows[3].Period = %q, want 1 Month", r3.Period)
	}
}

func TestDecodeSalesTSV_EmptyHeaderOnly(t *testing.T) {
	t.Parallel()
	b := readTestdata(t, "sales/empty.tsv")
	rows, err := DecodeSalesTSV(b)
	if err != nil {
		t.Fatalf("DecodeSalesTSV: %v", err)
	}
	if rows == nil {
		t.Fatal("rows = nil, want empty slice (Apple no-sales day must not surface as nil)")
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestDecodeSalesTSV_EmptyBytes(t *testing.T) {
	t.Parallel()
	rows, err := DecodeSalesTSV(nil)
	if err != nil {
		t.Fatalf("nil bytes: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("nil bytes → %d rows, want 0", len(rows))
	}

	rows, err = DecodeSalesTSV([]byte("   \n\n  "))
	if err != nil {
		t.Fatalf("whitespace-only: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("whitespace-only → %d rows, want 0", len(rows))
	}
}

func TestDecodeSalesTSV_SubscriptionSummary(t *testing.T) {
	t.Parallel()
	// Apple's subscription summary file has 35 columns (vs the 28 in the
	// SALES file). The decoder must drop the unknown columns rather than
	// erroring — forward-compat invariant.
	b := readTestdata(t, "sales/subscription_summary.tsv")
	rows, err := DecodeSalesTSV(b)
	if err != nil {
		t.Fatalf("DecodeSalesTSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].SKU != "com.example.testapp.subscription.monthly" {
		t.Errorf("rows[0].SKU = %q", rows[0].SKU)
	}
	if rows[0].Units != 120 {
		t.Errorf("rows[0].Units = %d, want 120", rows[0].Units)
	}
	if rows[0].DeveloperProceeds != 419.40 {
		t.Errorf("rows[0].DeveloperProceeds = %v, want 419.40", rows[0].DeveloperProceeds)
	}
	// Subscription column is non-empty (the period name) — confirms the
	// IAY-row mapping holds even when the file has extra trailing columns.
	if rows[0].Subscription != "1 Month" {
		t.Errorf("rows[0].Subscription = %q, want 1 Month", rows[0].Subscription)
	}
}

func TestDecodeSalesTSV_BadIntCell(t *testing.T) {
	t.Parallel()
	bad := []byte("Provider\tProvider Country\tSKU\tUnits\nAPPLE\tUS\tx\tabc\n")
	_, err := DecodeSalesTSV(bad)
	if err == nil {
		t.Fatal("expected parse error on non-numeric Units cell")
	}
}

func TestDecodeFinanceTSV_MonthlyBasic(t *testing.T) {
	t.Parallel()
	b := readTestdata(t, "finance/monthly_basic.tsv")
	rows, err := DecodeFinanceTSV(b)
	if err != nil {
		t.Fatalf("DecodeFinanceTSV: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}

	r0 := rows[0]
	if r0.VendorIdentifier != "com.example.testapp.sku1" {
		t.Errorf("rows[0].VendorIdentifier = %q", r0.VendorIdentifier)
	}
	if r0.Quantity != 42 {
		t.Errorf("rows[0].Quantity = %d, want 42", r0.Quantity)
	}
	if r0.PartnerShare != 2.10 {
		t.Errorf("rows[0].PartnerShare = %v, want 2.10", r0.PartnerShare)
	}
	if r0.ExtendedPartnerShare != 88.20 {
		t.Errorf("rows[0].ExtendedPartnerShare = %v, want 88.20", r0.ExtendedPartnerShare)
	}
	if r0.SalesOrReturn != "S" {
		t.Errorf("rows[0].SalesOrReturn = %q, want S", r0.SalesOrReturn)
	}
	if r0.CountryOfSale != "US" {
		t.Errorf("rows[0].CountryOfSale = %q, want US", r0.CountryOfSale)
	}

	// Returns row.
	r1 := rows[1]
	if r1.SalesOrReturn != "R" {
		t.Errorf("rows[1].SalesOrReturn = %q, want R", r1.SalesOrReturn)
	}
	if r1.CountryOfSale != "GB" {
		t.Errorf("rows[1].CountryOfSale = %q", r1.CountryOfSale)
	}

	// Subscription row carries the trailing Standard Subscription Type.
	r3 := rows[3]
	if r3.StandardSubscriptionTy != "1 Month" {
		t.Errorf("rows[3].StandardSubscriptionTy = %q, want 1 Month", r3.StandardSubscriptionTy)
	}
}

func TestDecodeFinanceTSV_EmptyBytes(t *testing.T) {
	t.Parallel()
	rows, err := DecodeFinanceTSV(nil)
	if err != nil {
		t.Fatalf("nil: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("nil → %d rows, want 0", len(rows))
	}
}

func TestDecodeFinanceTSV_BadFloatCell(t *testing.T) {
	t.Parallel()
	bad := []byte("Vendor Identifier\tQuantity\tPartner Share\nx\t1\tnotafloat\n")
	_, err := DecodeFinanceTSV(bad)
	if err == nil {
		t.Fatal("expected parse error on non-numeric Partner Share cell")
	}
}
