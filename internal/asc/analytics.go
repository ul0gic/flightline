package asc

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
)

// AnalyticsReportFilter selects AnalyticsReport rows client-side.
// Apple's /reports endpoint accepts no query parameters, so filtering is local.
type AnalyticsReportFilter struct {
	Category AnalyticsCategory

	// Case-insensitive: Apple mixes Title Case and ALLCAPS across report names.
	NameContains string
}

// FilterAnalyticsReports returns reports matching every non-zero filter field.
// A method for API symmetry only; it uses no client state.
func (c *Client) FilterAnalyticsReports(reports []AnalyticsReport, filter AnalyticsReportFilter) []AnalyticsReport {
	if filter.Category == "" && filter.NameContains == "" {
		out := make([]AnalyticsReport, len(reports))
		copy(out, reports)
		return out
	}
	needle := strings.ToLower(filter.NameContains)
	out := make([]AnalyticsReport, 0, len(reports))
	for _, r := range reports {
		if filter.Category != "" && r.Category != filter.Category {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.Name), needle) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// SegmentDownloadResult is the post-download view emitted in JSON mode.
type SegmentDownloadResult struct {
	SegmentID  string   `json:"segmentId"`
	InstanceID string   `json:"instanceId,omitempty"`
	Header     []string `json:"header,omitempty"`
	ByteCount  int      `json:"byteCount"`
	Bytes      []byte   `json:"-"` // kept off JSON; written to disk separately
}

// ParseSegmentDownload builds a SegmentDownloadResult from gunzipped bytes.
// An empty or unparseable CSV returns Header=nil rather than an error; Apple ships zero-row segments.
func ParseSegmentDownload(segmentID string, instanceID InstanceID, body []byte) SegmentDownloadResult {
	out := SegmentDownloadResult{
		SegmentID:  segmentID,
		InstanceID: string(instanceID),
		ByteCount:  len(body),
		Bytes:      body,
	}
	if len(body) == 0 {
		return out
	}
	// Analytics segments are comma-separated (sales/finance TSV doesn't flow through here).
	r := csv.NewReader(bytes.NewReader(body))
	row, err := r.Read()
	if errors.Is(err, io.EOF) {
		return out
	}
	if err != nil {
		// Header parse failure isn't fatal: raw bytes still write to disk, Header stays nil.
		return out
	}
	out.Header = row
	return out
}
