package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestF8C_ExperimentCommandWritesRequireConfirm(t *testing.T) {
	root := newExperimentsCommand()
	for _, name := range []string{"create", "update", "delete", "start", "stop"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Flags().Lookup("confirm") == nil {
			t.Fatalf("%s confirm missing: %v", name, err)
		}
	}
}

func TestF8C_ExperimentSelectionRejectsWrongVersion(t *testing.T) {
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gets++; _, _ = w.Write([]byte(`{"data":[],"links":{}}`)) }))
	defer srv.Close()
	_, err := selectedProductExperiment(context.Background(), fixtureASCClient(t, srv), "A1", "V1", "IOS", "E1")
	if err == nil || gets != 1 {
		t.Fatalf("err=%v gets=%d", err, gets)
	}
}
