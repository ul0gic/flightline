package asc

import (
	"encoding/json"
	"testing"
)

func TestG8_SubmissionReaderRejectsAmbiguousRelationships(t *testing.T) {
	rels := map[string]Relationship{
		"appEvent":        {Data: json.RawMessage(`{"type":"appEvents","id":"E"}`)},
		"appStoreVersion": {Data: json.RawMessage(`{"type":"appStoreVersions","id":"V"}`)},
	}
	got := ResolveReviewSubmissionItemReference("item", "submission", rels)
	if !got.Opaque || got.Type != "UNKNOWN(AMBIGUOUS)" {
		t.Fatalf("ambiguous resource selected: %+v", got)
	}
}
