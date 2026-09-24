package state

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestC2C_FetchExportDeclaration_ProjectsCurrentBuildRelationship(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/builds/BUILD1/appEncryptionDeclaration" {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"type":"appEncryptionDeclarations","id":"DECL1","attributes":{"appDescription":"Uses TLS","containsProprietaryCryptography":true,"containsThirdPartyCryptography":false,"availableOnFrenchStore":true,"appEncryptionDeclarationState":"REJECTED"}}}`)
	}))
	defer srv.Close()

	got, err := fetchExportDeclaration(context.Background(), fixtureClient(t, srv), "BUILD1")
	if err != nil {
		t.Fatalf("fetchExportDeclaration: %v", err)
	}
	if got == nil || got.AppDescription == nil || *got.AppDescription != "Uses TLS" ||
		got.ContainsProprietaryCryptography == nil || !*got.ContainsProprietaryCryptography ||
		got.ContainsThirdPartyCryptography == nil || *got.ContainsThirdPartyCryptography ||
		got.AvailableOnFrenchStore == nil || !*got.AvailableOnFrenchStore {
		t.Fatalf("projection=%+v", got)
	}
}

func TestC2C_FetchExportDeclaration_OnlyOptionalAbsenceIsSuppressed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"null", http.StatusOK, `{"data":null}`, false},
		{"not found", http.StatusNotFound, `{"errors":[{"status":"404","title":"not found"}]}`, false},
		{"forbidden", http.StatusForbidden, `{"errors":[{"status":"403","title":"forbidden"}]}`, true},
		{"incomplete", http.StatusOK, `{"data":{"type":"appEncryptionDeclarations","id":"DECL1","attributes":{"appDescription":"Uses TLS"}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			got, err := fetchExportDeclaration(context.Background(), fixtureClient(t, srv), "BUILD1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if !tc.wantErr && got != nil {
				t.Fatalf("result=%+v, want nil", got)
			}
		})
	}
}

func TestC2C_ApplyExportDeclarationCreatesAndAssociates(t *testing.T) {
	var createBody, associationBody map[string]any
	var creates, associations int
	srv := declarationApplyServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appEncryptionDeclarations":
			if got := r.URL.Query().Get("filter[app]"); got != "APP1" {
				t.Fatalf("filter[app]=%q", got)
			}
			_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
			return true
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appEncryptionDeclarations":
			creates++
			defer func() { _ = r.Body.Close() }()
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"appEncryptionDeclarations","id":"DECL1"}}`)
			return true
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/builds/BUILD1/relationships/appEncryptionDeclaration":
			associations++
			defer func() { _ = r.Body.Close() }()
			if err := json.NewDecoder(r.Body).Decode(&associationBody); err != nil {
				t.Fatalf("decode association: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
			return true
		default:
			return false
		}
	})
	defer srv.Close()

	err := applyEncryptionDeclaration(context.Background(), fixtureClient(t, srv), declarationApplyContext(), declarationChange())
	if err != nil {
		t.Fatalf("applyEncryptionDeclaration: %v", err)
	}
	if creates != 1 || associations != 1 {
		t.Fatalf("creates=%d associations=%d", creates, associations)
	}
	assertC2CCreateRequest(t, createBody, associationBody)
}

func assertC2CCreateRequest(t *testing.T, createBody, associationBody map[string]any) {
	t.Helper()
	data := requiredObject(t, createBody, "data")
	attrs := requiredObject(t, data, "attributes")
	if attrs["appDescription"] != "Uses TLS" || attrs["containsProprietaryCryptography"] != true ||
		attrs["containsThirdPartyCryptography"] != false || attrs["availableOnFrenchStore"] != true {
		t.Fatalf("create attributes=%v", attrs)
	}
	encoded := stringifyJSON(t, createBody)
	if strings.Contains(encoded, "document") || strings.Contains(encoded, "eccn") {
		t.Fatalf("create body carried unsupported lifecycle data: %s", encoded)
	}
	if got := requiredObject(t, associationBody, "data")["id"]; got != "DECL1" {
		t.Fatalf("association body=%v", associationBody)
	}
}

func TestC2C_ApplyExportDeclarationReusesDeterministicMatchingDeclaration(t *testing.T) {
	var creates int
	var associated string
	srv := declarationApplyServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appEncryptionDeclarations":
			_, _ = io.WriteString(w, `{"data":[{"type":"appEncryptionDeclarations","id":"DECL-B","attributes":{"appDescription":"Uses TLS","containsProprietaryCryptography":true,"containsThirdPartyCryptography":false,"availableOnFrenchStore":true,"appEncryptionDeclarationState":"APPROVED"}},{"type":"appEncryptionDeclarations","id":"DECL-A","attributes":{"appDescription":"Uses TLS","containsProprietaryCryptography":true,"containsThirdPartyCryptography":false,"availableOnFrenchStore":true,"appEncryptionDeclarationState":"CREATED"}}],"links":{}}`)
			return true
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appEncryptionDeclarations":
			creates++
			t.Fatalf("unexpected declaration create")
			return true
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/builds/BUILD1/relationships/appEncryptionDeclaration":
			body := map[string]map[string]string{}
			defer func() { _ = r.Body.Close() }()
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode association: %v", err)
			}
			associated = body["data"]["id"]
			w.WriteHeader(http.StatusNoContent)
			return true
		default:
			return false
		}
	})
	defer srv.Close()

	if err := applyEncryptionDeclaration(context.Background(), fixtureClient(t, srv), declarationApplyContext(), declarationChange()); err != nil {
		t.Fatalf("applyEncryptionDeclaration: %v", err)
	}
	if creates != 0 || associated != "DECL-A" {
		t.Fatalf("creates=%d associated=%q", creates, associated)
	}
}

func TestC2C_ApplyExportDeclarationAssociationFailureReusesOnNextApply(t *testing.T) {
	var creates, associations int
	srv := declarationApplyServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appEncryptionDeclarations":
			if creates == 0 {
				_, _ = io.WriteString(w, `{"data":[],"links":{}}`)
			} else {
				_, _ = io.WriteString(w, `{"data":[{"type":"appEncryptionDeclarations","id":"DECL1","attributes":{"appDescription":"Uses TLS","containsProprietaryCryptography":true,"containsThirdPartyCryptography":false,"availableOnFrenchStore":true,"appEncryptionDeclarationState":"CREATED"}}],"links":{}}`)
			}
			return true
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appEncryptionDeclarations":
			creates++
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"type":"appEncryptionDeclarations","id":"DECL1"}}`)
			return true
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/builds/BUILD1/relationships/appEncryptionDeclaration":
			associations++
			if associations == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"errors":[{"status":"409","title":"conflict"}]}`)
				return true
			}
			w.WriteHeader(http.StatusNoContent)
			return true
		default:
			return false
		}
	})
	defer srv.Close()

	c := fixtureClient(t, srv)
	if err := applyEncryptionDeclaration(context.Background(), c, declarationApplyContext(), declarationChange()); err == nil {
		t.Fatal("first association failure should be returned")
	}
	if err := applyEncryptionDeclaration(context.Background(), c, declarationApplyContext(), declarationChange()); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if creates != 1 || associations != 2 {
		t.Fatalf("creates=%d associations=%d", creates, associations)
	}
}

func TestC2C_ApplyExportDeclarationRejectsUnsupportedIntentBeforeHTTP(t *testing.T) {
	change := declarationChange()
	declaration, ok := change.To.(config.ExportComplianceDeclaration)
	if !ok {
		t.Fatalf("declaration type=%T", change.To)
	}
	legacy := true
	declaration.UsesEncryption = &legacy
	change.To = declaration
	err := applyEncryptionDeclaration(context.Background(), nil, declarationApplyContext(), change)
	if err == nil || !strings.Contains(err.Error(), "legacy declaration attributes") {
		t.Fatalf("error=%v", err)
	}
}

func declarationApplyServer(t *testing.T, extra func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			_, _ = io.WriteString(w, `{"data":[{"type":"apps","id":"APP1"}],"links":{}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = io.WriteString(w, `{"data":[{"type":"appStoreVersions","id":"VER1","attributes":{"versionString":"1.0"}}],"links":{}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/appStoreVersions/VER1/build":
			_, _ = io.WriteString(w, `{"data":{"type":"builds","id":"BUILD1","attributes":{"version":"42"}}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/builds/BUILD1/appEncryptionDeclaration":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"errors":[{"status":"404","title":"not found"}]}`)
		default:
			if !extra(w, r) {
				http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
				return
			}
		}
	}))
}

func declarationApplyContext() ApplyContext {
	return ApplyContext{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}
}

func declarationChange() plan.Change {
	description := "Uses TLS"
	proprietary := true
	thirdParty := false
	frenchStore := true
	return plan.Change{
		Op:       plan.OpCreate,
		Resource: "exportCompliance",
		Path:     "/spec/exportCompliance/declaration",
		To: config.ExportComplianceDeclaration{
			AppDescription:                  &description,
			ContainsProprietaryCryptography: &proprietary,
			ContainsThirdPartyCryptography:  &thirdParty,
			AvailableOnFrenchStore:          &frenchStore,
		},
	}
}

func stringifyJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

func requiredObject(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	object, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("%s=%T, want object", key, value[key])
	}
	return object
}
