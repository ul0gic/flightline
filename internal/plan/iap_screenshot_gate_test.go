package plan

import (
	"testing"

	"github.com/ul0gic/flightline/internal/config"
)

func TestG3_IAPScreenshotReplacementEmitsOneChange(t *testing.T) {
	live := iapScreenshotState("before.png")
	desired := iapScreenshotState("after.png")
	changes := Diff(desired, live)
	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want one screenshot replacement", changes)
	}
	change := changes[0]
	if change.Op != OpUpdate || change.Path != "/spec/iap/products/com.example.lifetime/reviewScreenshot" {
		t.Fatalf("change = %+v, want screenshot update", change)
	}

	if unchanged := Diff(desired, desired); len(unchanged) != 0 {
		t.Fatalf("unchanged screenshot produced changes: %+v", unchanged)
	}
}

func iapScreenshotState(path string) *config.State {
	return &config.State{Spec: config.StateSpec{IAP: &config.IAPSpec{Products: map[string]config.IAPProduct{
		"com.example.lifetime": {ReviewScreenshot: &config.IAPReviewScreenshot{Path: path}},
	}}}}
}
