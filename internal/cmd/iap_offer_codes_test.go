package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/asc"
)

func TestF5A_OfferCommandTreeAndConfirmation(t *testing.T) {
	root := newIAPOfferCodesCommand()
	want := map[string]bool{"list": false, "get": false, "create-definition": false, "create-custom": false, "create-one-time": false, "download": false}
	for _, command := range root.Commands() {
		if _, ok := want[command.Name()]; !ok {
			t.Fatalf("unexpected command %s", command.Name())
		}
		want[command.Name()] = true
		if command.Flags().Lookup("product") == nil {
			t.Errorf("%s lacks product selector", command.Name())
		}
		if strings.HasPrefix(command.Name(), "create-") || command.Name() == "download" {
			if err := requireOfferConfirm(command); err == nil {
				t.Errorf("%s runs without confirmation", command.Name())
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing %s", name)
		}
	}
}

func TestF5A_OfferPriceFlagsAndPrivateCustomCodeFile(t *testing.T) {
	choices, err := parseIAPOfferPriceChoices([]string{"USA=P1", "CAN=P2"})
	if err != nil || len(choices) != 2 || choices[1].TerritoryID != "CAN" {
		t.Fatalf("choices=%+v err=%v", choices, err)
	}
	if _, err := parseIAPOfferPriceChoices([]string{"P1"}); err == nil {
		t.Fatal("malformed price accepted")
	}
	path := filepath.Join(t.TempDir(), "code")
	if err := os.WriteFile(path, []byte("private-code\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, err := readIAPCustomCodeFile(path)
	if err != nil || code != "private-code" {
		t.Fatalf("code read err=%v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readIAPCustomCodeFile(path); err == nil {
		t.Fatal("world-readable code file accepted")
	}
}

func TestF5A_OfferMutationOutputDoesNotRevealCode(t *testing.T) {
	buf, err := json.Marshal(IAPOfferActionResult{Action: "create-custom", ID: "B1", Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buf), "customCode") || strings.Contains(string(buf), "private-code") {
		t.Fatalf("mutation result leaked code: %s", buf)
	}
}

func TestF5A_OfferDetailJSONOmitsCustomCodeValue(t *testing.T) {
	result := IAPOfferCodeDetailResult{ProductID: "P1", Detail: asc.IAPOfferCodeDetail{
		IAPOfferCode:  asc.IAPOfferCode{ID: "O1", Name: "Fall"},
		CustomBatches: []asc.IAPOfferCustomBatch{{ID: "B1", CustomCode: "private-code", NumberOfCodes: 10}},
	}}
	buf, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buf), "private-code") || strings.Contains(string(buf), "customCode") {
		t.Fatalf("offer detail leaked custom code: %s", buf)
	}
}

func TestF5A_OfferValuesDownloadCreatesPrivateFileOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/inAppPurchases/I1/offerCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodes","id":"O1","attributes":{"name":"Fall"}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodes/O1/oneTimeUseCodes":
			_, _ = w.Write([]byte(`{"data":[{"type":"inAppPurchaseOfferCodeOneTimeUseCodes","id":"B1","attributes":{"numberOfCodes":1,"environment":"SANDBOX"}}],"links":{}}`))
		case "/v1/inAppPurchaseOfferCodeOneTimeUseCodes/B1/values":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("code\nprivate-value\n"))
		default:
			t.Errorf("request=%s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cmd := newIAPOfferValuesDownloadCommand()
	cmd.SetContext(context.Background())
	path := filepath.Join(t.TempDir(), "codes.csv")
	count, err := downloadIAPOfferValues(cmd, fixtureASCClient(t, srv), "I1", "O1", "B1", path)
	if err != nil || count == 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file=%v err=%v", info, err)
	}
	if _, err := downloadIAPOfferValues(cmd, fixtureASCClient(t, srv), "I1", "O1", "B1", path); err == nil {
		t.Fatal("existing destination was overwritten")
	}
}
