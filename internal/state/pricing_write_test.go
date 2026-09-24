package state

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
)

func TestC2B_PriceApplyRefetchConverges(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"APP1"}],"links":{}}`))
		case "/v1/apps/APP1/appPriceSchedule":
			if posts.Load() == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"id":"S1","relationships":{"baseTerritory":{"data":{"id":"USA"}}}}}`))
		case "/v1/appPriceSchedules/S1/manualPrices":
			_, _ = w.Write([]byte(`{"data":[{"id":"P1","attributes":{"manual":true},"relationships":{"territory":{"data":{"id":"USA"}},"appPricePoint":{"data":{"id":"PP1"}}}}],"links":{}}`))
		case "/v3/appPricePoints/PP1":
			_, _ = w.Write([]byte(`{"data":{"id":"PP1","relationships":{"territory":{"data":{"id":"USA"}}}}}`))
		case "/v1/appPriceSchedules":
			posts.Add(1)
			_, _ = w.Write([]byte(`{"data":{"id":"S1"}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := fixtureClient(t, srv)
	str := func(s string) *string { return &s }
	desired := &config.State{Spec: config.StateSpec{Pricing: &config.PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("PP1")}}}
	changes := plan.Diff(desired, &config.State{})
	if len(changes) != 1 {
		t.Fatalf("initial changes=%+v", changes)
	}
	if err := applyPricingField(context.Background(), c, ApplyContext{BundleID: "com.example.app"}, changes[0]); err != nil {
		t.Fatal(err)
	}
	livePricing, err := fetchPricing(context.Background(), c, "APP1")
	if err != nil {
		t.Fatal(err)
	}
	if remaining := plan.Diff(desired, &config.State{Spec: config.StateSpec{Pricing: livePricing}}); len(remaining) != 0 {
		t.Fatalf("remaining=%+v", remaining)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts=%d", posts.Load())
	}
}

func TestC2B_PricingWriteAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			_, _ = w.Write([]byte(`{"data":[{"id":"APP1"}],"links":{}}`))
		case "/v1/apps/APP1/appPriceSchedule":
			w.WriteHeader(http.StatusNotFound)
		case "/v3/appPricePoints/PP1":
			_, _ = w.Write([]byte(`{"data":{"id":"PP1","relationships":{"territory":{"data":{"id":"USA"}}}}}`))
		case "/v1/appPriceSchedules":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	str := func(s string) *string { return &s }
	change := plan.Change{Op: plan.OpCreate, Path: "/spec/pricing", To: config.PricingSpec{BaseTerritory: str("USA"), AppPricePointID: str("PP1")}}
	if err := applyPricingField(context.Background(), fixtureClient(t, srv), ApplyContext{BundleID: "com.example.app"}, change); err == nil {
		t.Fatal("write failure accepted")
	}
}
