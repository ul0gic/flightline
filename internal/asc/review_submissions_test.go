package asc

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func encodedReviewItemID(s string) string {
	return base64.RawStdEncoding.EncodeToString([]byte(s))
}

func TestResolveReviewSubmissionItemReference(t *testing.T) {
	tests := []struct {
		name         string
		itemID       string
		submissionID string
		rels         map[string]Relationship
		want         ReviewSubmissionItemReference
	}{
		{
			name:         "relationship takes precedence over encoded native id",
			itemID:       encodedReviewItemID("sub-1|6|884414288"),
			submissionID: "sub-1",
			rels: map[string]Relationship{
				"appStoreVersion": {Data: json.RawMessage(`{"type":"appStoreVersions","id":"version-uuid"}`)},
			},
			want: ReviewSubmissionItemReference{Type: "appStoreVersions", ID: "version-uuid", Canonical: true},
		},
		{
			name:         "type 17 is canonical iap version",
			itemID:       encodedReviewItemID("sub-1|17|iap-version-uuid"),
			submissionID: "sub-1",
			want: ReviewSubmissionItemReference{
				Type: "inAppPurchaseVersions", ID: "iap-version-uuid", Discriminator: 17, Canonical: true,
			},
		},
		{
			name:         "type 6 native id is not canonical",
			itemID:       encodedReviewItemID("sub-1|6|884414288"),
			submissionID: "sub-1",
			want:         ReviewSubmissionItemReference{Type: "appStoreVersions", ID: "884414288", Discriminator: 6},
		},
		{
			name:         "unknown discriminator remains visible",
			itemID:       encodedReviewItemID("sub-1|42|native-id"),
			submissionID: "sub-1",
			want: ReviewSubmissionItemReference{
				Type: "UNKNOWN(42)", ID: "native-id", Discriminator: 42, Opaque: true,
			},
		},
		{
			name:         "wrong submission id is malformed",
			itemID:       encodedReviewItemID("sub-2|17|iap-version-uuid"),
			submissionID: "sub-1",
			want:         ReviewSubmissionItemReference{Type: "UNKNOWN(MALFORMED)", Opaque: true},
		},
		{
			name:         "extra delimiter is malformed",
			itemID:       encodedReviewItemID("sub-1|17|iap|version"),
			submissionID: "sub-1",
			want:         ReviewSubmissionItemReference{Type: "UNKNOWN(MALFORMED)", Opaque: true},
		},
		{
			name:         "invalid base64 is malformed",
			itemID:       "%%not-base64%%",
			submissionID: "sub-1",
			want:         ReviewSubmissionItemReference{Type: "UNKNOWN(MALFORMED)", Opaque: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveReviewSubmissionItemReference(tt.itemID, tt.submissionID, tt.rels)
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
