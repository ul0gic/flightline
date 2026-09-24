package asc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestF8B_LocalizationCreateRejectsDuplicateAndForeignResponse(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts++
			_, _ = w.Write([]byte(`{"data":{"type":"appEventLocalizations","id":"L2","attributes":{"locale":"fr-FR"},"relationships":{"appEvent":{"data":{"type":"appEvents","id":"OTHER"}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"appEventLocalizations","id":"L1","attributes":{"locale":"en-US"}}],"links":{}}`))
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	_, _, err := CreateAppEventLocalization(context.Background(), c, "E1", AppEventLocalizationAttributes{Locale: "en-US"})
	if err == nil || !strings.Contains(err.Error(), "already exists") || posts != 0 {
		t.Fatalf("duplicate err=%v posts=%d", err, posts)
	}
	_, _, err = CreateAppEventLocalization(context.Background(), c, "E1", AppEventLocalizationAttributes{Locale: "fr-FR"})
	if err == nil || !strings.Contains(err.Error(), "another event") || posts != 1 {
		t.Fatalf("foreign response err=%v posts=%d", err, posts)
	}
}
