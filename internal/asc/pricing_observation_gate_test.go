package asc

import (
	"encoding/json"
	"testing"
)

func TestG1_PriceRelationshipRequiresID(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"type":"territories"}`} {
		if _, err := priceRelationshipID(Relationship{Data: json.RawMessage(raw)}); err == nil {
			t.Errorf("malformed relationship %q accepted", raw)
		}
	}
	if id, err := priceRelationshipID(Relationship{Data: json.RawMessage(`null`)}); err != nil || id != "" {
		t.Fatalf("explicit absence: %q, %v", id, err)
	}
}
