// Package asc — analytics command-side helpers.
//
// internal/asc/async.go owns the request/poll/list/download primitives. This
// file is the thin layer the analytics CLI consumes on top: client-side
// filtering of report rows, plus a typed download result the CLI emits in
// JSON mode.
//
// Nothing here makes additional HTTP calls of its own — every helper
// operates on values returned by the async.go primitives. Keeping the I/O
// boundary in async.go means tests against the analytics surface can stub
// the HTTP layer once and reuse it across both packages.
package asc

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
)

// AnalyticsReportFilter selects a subset of AnalyticsReport rows by their
// attributes. All fields are optional; the zero value is "no filter".
//
// Filtering happens client-side because Apple's
// /v1/analyticsReportRequests/{id}/reports endpoint does not accept query
// parameters — the wire response is the full list and we narrow it locally.
type AnalyticsReportFilter struct {
	// Category, when non-empty, retains only reports whose category matches.
	// Use one of the Category* constants (CategoryAppUsage, CategoryCommerce,
	// etc.) or any literal Apple has published.
	Category AnalyticsCategory

	// NameContains, when non-empty, retains only reports whose Name contains
	// this substring. Match is case-insensitive — Apple uses Title Case for
	// some report names and ALLCAPS for others, and CLI users shouldn't have
	// to remember which.
	NameContains string
}

// FilterAnalyticsReports returns a new slice containing only the reports
// that match every non-zero field on filter. Pure helper — no I/O. The
// input slice is not modified.
//
// The function is on *Client only for symmetry with the rest of the
// analytics surface (so callers don't have to remember which helpers are
// methods and which are package-level). It uses no client state.
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

// SegmentDownloadResult is the post-download view the analytics download
// command emits in JSON mode.
//
// Bytes carries the gunzipped CSV payload. Header is the parsed first line
// (CSV-decoded into column names) so consumers don't have to re-parse to
// learn the schema. ByteCount mirrors len(Bytes) so JSON consumers see the
// count without base64-decoding.
type SegmentDownloadResult struct {
	SegmentID  string   `json:"segmentId"`
	InstanceID string   `json:"instanceId,omitempty"`
	Header     []string `json:"header,omitempty"`
	ByteCount  int      `json:"byteCount"`
	Bytes      []byte   `json:"-"` // not in JSON output; written to disk separately
}

// ParseSegmentDownload builds a SegmentDownloadResult from the gunzipped
// bytes returned by DownloadAnalyticsSegment. It parses the first CSV line
// for the header; if the body is empty or the parse fails it returns the
// result with Header=nil rather than erroring (an empty CSV is still a
// valid download — Apple sometimes ships zero-row segments for app-days
// with no data).
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
	// csv.Reader auto-detects the comma delimiter; analytics segments are
	// comma-separated (sales/finance use TSV but those don't flow through
	// here).
	r := csv.NewReader(bytes.NewReader(body))
	row, err := r.Read()
	if errors.Is(err, io.EOF) {
		return out
	}
	if err != nil {
		// Header parse failure isn't fatal — the raw bytes still write to
		// disk and JSON consumers see ByteCount. Leave Header nil.
		return out
	}
	out.Header = row
	return out
}
