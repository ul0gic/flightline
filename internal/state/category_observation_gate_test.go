package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestG1_CategoryMalformedRelationshipFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	if _, err := getCategoryRelationship(context.Background(), fixtureClient(t, srv), "A1", "primaryCategory"); err == nil {
		t.Fatal("malformed category treated as unset")
	}
}
