package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestF6B_AppEULAGetShowsCompleteTerritories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1/endUserLicenseAgreement":
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"Legal text"}}}`)
		case "/v1/endUserLicenseAgreements/E1/territories":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"territories","id":"USA"},{"type":"territories","id":"GBR"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cmd := newAppEULACommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runAppEULAGetWithClient(cmd, "com.example.app", fixtureASCClient(t, srv), "json"); err != nil {
		t.Fatal(err)
	}
	var view AppEULAView
	if err := json.Unmarshal(output.Bytes(), &view); err != nil || view.ID != "E1" || view.AgreementText != "Legal text" || !reflect.DeepEqual(view.Territories, []string{"GBR", "USA"}) {
		t.Fatalf("view=%+v err=%v output=%s", view, err, output.String())
	}
}

func TestF6B_AppEULASetNoopsOnMatchingExactState(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agreement.txt")
	if err := os.WriteFile(file, []byte("Terms"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes++
		}
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"A1","attributes":{"bundleId":"com.example.app"}}]}`)
		case "/v1/apps/A1/endUserLicenseAgreement":
			_, _ = fmt.Fprint(w, `{"data":{"type":"endUserLicenseAgreements","id":"E1","attributes":{"agreementText":"Terms"}}}`)
		case "/v1/endUserLicenseAgreements/E1/territories":
			_, _ = fmt.Fprint(w, `{"data":[{"type":"territories","id":"USA"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cmd := newAppEULACommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runAppEULASetWithClient(cmd, "com.example.app", file, []string{"USA"}, true, fixtureASCClient(t, srv), "json"); err != nil {
		t.Fatal(err)
	}
	var result AppEULAWriteResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Changed || writes != 0 {
		t.Fatalf("result=%+v err=%v writes=%d", result, err, writes)
	}
}
