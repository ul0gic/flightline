// Package asc — async-poll wrapper.
//
// Apple's Analytics Reports API and Sales/Finance Reports API don't return
// data in the same request as the rest of the surface. Both follow a
// request → wait → poll → download lifecycle:
//
//  1. Analytics — POST /v1/analyticsReportRequests, then poll
//     /v1/analyticsReportRequests/{id}/reports until reports appear, then list
//     instances per report and download each segment from a pre-signed URL.
//     Latency: minutes to hours per request.
//  2. Sales / Finance — synchronous gzipped CSV passthrough on
//     /v1/salesReports / /v1/financeReports. Returned with content-type
//     "application/a-gzip"; we gunzip transparently.
//
// This file holds the wrapper-level public API consumed by Phase 2A.2
// (sales / finance / subscription reports) and 2A.3 (analytics reports).
// Both downstream packages depend on the shape declared here — change with
// care; renaming or removing exported symbols is a breaking change.
package asc

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

// RequestID is an analytics report request identifier returned by Apple from
// POST /v1/analyticsReportRequests. Wrapped in a typed string so it can't be
// confused at call sites with ReportID, InstanceID, or a bare app ID.
type RequestID string

// ReportID is an analytics report identifier (one row in
// /v1/analyticsReportRequests/{id}/reports). Each report has many instances.
type ReportID string

// InstanceID is an analytics report instance identifier. An instance is
// scoped to a granularity + processing date and contains 1..N segments.
type InstanceID string

// ---------------------------------------------------------------------------
// Enums (mirror the spec's accessType / category / granularity literals)
// ---------------------------------------------------------------------------

// AnalyticsAccessType is the access pattern for an analytics report request.
// Apple supports two:
//
//   - ONE_TIME_SNAPSHOT — single point-in-time pull; reports stop being
//     produced once the snapshot is done.
//   - ONGOING — reports keep being added as new data becomes available.
type AnalyticsAccessType string

const (
	// AccessTypeOneTimeSnapshot pulls a snapshot once.
	AccessTypeOneTimeSnapshot AnalyticsAccessType = "ONE_TIME_SNAPSHOT"
	// AccessTypeOngoing keeps producing reports over time.
	AccessTypeOngoing AnalyticsAccessType = "ONGOING"
)

// AnalyticsCategory is the high-level grouping of a report (e.g. APP_USAGE,
// COMMERCE). Mirrors the spec enum at AnalyticsReport.attributes.category.
type AnalyticsCategory string

// Analytics category enum literals as published in openapi.oas.json.
const (
	CategoryAppUsage           AnalyticsCategory = "APP_USAGE"
	CategoryAppStoreEngagement AnalyticsCategory = "APP_STORE_ENGAGEMENT"
	CategoryCommerce           AnalyticsCategory = "COMMERCE"
	CategoryFrameworkUsage     AnalyticsCategory = "FRAMEWORK_USAGE"
	CategoryPerformance        AnalyticsCategory = "PERFORMANCE"
)

// AnalyticsGranularity is the time-bucket size of an analytics report
// instance: DAILY / WEEKLY / MONTHLY (per spec).
type AnalyticsGranularity string

// Analytics granularity enum literals.
const (
	GranularityDaily   AnalyticsGranularity = "DAILY"
	GranularityWeekly  AnalyticsGranularity = "WEEKLY"
	GranularityMonthly AnalyticsGranularity = "MONTHLY"
)

// ---------------------------------------------------------------------------
// Typed attributes mirroring Apple's wire shape (subset Skipper actually uses)
// ---------------------------------------------------------------------------

// AnalyticsReportRequestAttributes mirrors AnalyticsReportRequest.attributes
// from the spec.
type AnalyticsReportRequestAttributes struct {
	AccessType             AnalyticsAccessType `json:"accessType,omitempty"`
	StoppedDueToInactivity bool                `json:"stoppedDueToInactivity,omitempty"`
}

// AnalyticsReportAttributes mirrors AnalyticsReport.attributes from the spec.
type AnalyticsReportAttributes struct {
	Name     string            `json:"name,omitempty"`
	Category AnalyticsCategory `json:"category,omitempty"`
}

// AnalyticsReport is the wrapper-flat view of one analytics report row
// surfaced through the iterator. ID is canonical; Name/Category come from
// the resource attributes block. RequestID is filled in by the wrapper for
// caller convenience (Apple's response doesn't echo it on the report rows).
type AnalyticsReport struct {
	ID        ReportID          `json:"id"`
	RequestID RequestID         `json:"requestId,omitempty"`
	Name      string            `json:"name,omitempty"`
	Category  AnalyticsCategory `json:"category,omitempty"`
}

// AnalyticsReportInstanceAttributes mirrors AnalyticsReportInstance.attributes.
type AnalyticsReportInstanceAttributes struct {
	Granularity    AnalyticsGranularity `json:"granularity,omitempty"`
	ProcessingDate string               `json:"processingDate,omitempty"`
}

// AnalyticsReportInstance is the wrapper-flat view of one report instance.
type AnalyticsReportInstance struct {
	ID             InstanceID           `json:"id"`
	ReportID       ReportID             `json:"reportId,omitempty"`
	Granularity    AnalyticsGranularity `json:"granularity,omitempty"`
	ProcessingDate string               `json:"processingDate,omitempty"`
}

// AnalyticsReportSegmentAttributes mirrors AnalyticsReportSegment.attributes.
type AnalyticsReportSegmentAttributes struct {
	Checksum    string `json:"checksum,omitempty"`
	SizeInBytes int64  `json:"sizeInBytes,omitempty"`
	URL         string `json:"url,omitempty"`
}

// AnalyticsReportSegment is the wrapper-flat view of one segment. URL is
// the pre-signed CDN URL Apple expects callers to GET *without* an
// Authorization header.
type AnalyticsReportSegment struct {
	ID          string     `json:"id"`
	InstanceID  InstanceID `json:"instanceId,omitempty"`
	URL         string     `json:"url,omitempty"`
	Checksum    string     `json:"checksum,omitempty"`
	SizeInBytes int64      `json:"sizeInBytes,omitempty"`
}

// ---------------------------------------------------------------------------
// Request / poll parameters
// ---------------------------------------------------------------------------

// AnalyticsReportRequestParams is the input to RequestAnalyticsReport.
//
// AppID is Apple's numeric app ID (string per the spec, e.g. "1234567890").
// AccessType selects between ONE_TIME_SNAPSHOT and ONGOING. The spec rejects
// any other attribute on the request body (filters/granularity are applied
// at list/instance time, not at request time).
type AnalyticsReportRequestParams struct {
	AppID      string
	AccessType AnalyticsAccessType
}

// PollOpts configures the analytics polling loop's exponential backoff.
//
// Defaults (used when a field is zero) target Apple's 500 req/hr rate limit
// with comfortable headroom: 30s initial, capped at 5m, 1.5x growth, no
// attempt cap. Override conservatively — polling faster than every 30s is
// rarely worth the budget.
type PollOpts struct {
	InitialBackoff time.Duration // default 30s
	MaxBackoff     time.Duration // default 5m
	Multiplier     float64       // default 1.5
	MaxAttempts    int           // 0 = unlimited
}

// resolveDefaults fills any zero field with the documented default.
func (o PollOpts) resolveDefaults() PollOpts {
	out := o
	if out.InitialBackoff <= 0 {
		out.InitialBackoff = 30 * time.Second
	}
	if out.MaxBackoff <= 0 {
		out.MaxBackoff = 5 * time.Minute
	}
	if out.Multiplier < 1.0 {
		out.Multiplier = 1.5
	}
	return out
}

// ---------------------------------------------------------------------------
// Sentinel error: pollAnalyticsReport stops cleanly when the request reports
// stoppedDueToInactivity (ONGOING access type only).
// ---------------------------------------------------------------------------

// ErrAnalyticsRequestStopped is returned by PollAnalyticsReport when Apple
// flips the request's stoppedDueToInactivity flag to true (ONGOING access
// type only). The iterator yields the error once and terminates.
var ErrAnalyticsRequestStopped = errors.New("asc: analytics request stopped due to inactivity")

// ---------------------------------------------------------------------------
// 1. Submit a new analytics report request
// ---------------------------------------------------------------------------

// analyticsCreateRequestBody mirrors the spec's
// AnalyticsReportRequestCreateRequest (data.type, data.attributes.accessType,
// data.relationships.app.data.{type,id}). Built once per submit; not exported
// because the public API takes the typed AnalyticsReportRequestParams.
type analyticsCreateRequestBody struct {
	Data analyticsCreateRequestData `json:"data"`
}

type analyticsCreateRequestData struct {
	Type          string                              `json:"type"`
	Attributes    analyticsCreateRequestAttributes    `json:"attributes"`
	Relationships analyticsCreateRequestRelationships `json:"relationships"`
}

type analyticsCreateRequestAttributes struct {
	AccessType AnalyticsAccessType `json:"accessType"`
}

type analyticsCreateRequestRelationships struct {
	App analyticsCreateRequestApp `json:"app"`
}

type analyticsCreateRequestApp struct {
	Data analyticsCreateRequestAppData `json:"data"`
}

type analyticsCreateRequestAppData struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// RequestAnalyticsReport submits a new analytics report request and returns
// its ID. The request is queued by Apple; reports become available over
// minutes-to-hours and must be retrieved via PollAnalyticsReport.
//
// Validation:
//   - params.AppID must be non-empty (Apple's numeric app id).
//   - params.AccessType must be ONE_TIME_SNAPSHOT or ONGOING.
//
// Wire body matches AnalyticsReportRequestCreateRequest from openapi.oas.json:
//
//	{ "data": { "type": "analyticsReportRequests",
//	            "attributes": { "accessType": "..." },
//	            "relationships": { "app": { "data": { "type": "apps", "id": "..." } } } } }
func (c *Client) RequestAnalyticsReport(ctx context.Context, params AnalyticsReportRequestParams) (RequestID, error) {
	if params.AppID == "" {
		return "", errors.New("asc: RequestAnalyticsReport: AppID is required")
	}
	switch params.AccessType {
	case AccessTypeOneTimeSnapshot, AccessTypeOngoing:
	default:
		return "", fmt.Errorf("asc: RequestAnalyticsReport: AccessType must be %q or %q",
			AccessTypeOneTimeSnapshot, AccessTypeOngoing)
	}

	body := analyticsCreateRequestBody{
		Data: analyticsCreateRequestData{
			Type: "analyticsReportRequests",
			Attributes: analyticsCreateRequestAttributes{
				AccessType: params.AccessType,
			},
			Relationships: analyticsCreateRequestRelationships{
				App: analyticsCreateRequestApp{
					Data: analyticsCreateRequestAppData{
						Type: "apps",
						ID:   params.AppID,
					},
				},
			},
		},
	}

	resp, err := Post[Single[AnalyticsReportRequestAttributes]](
		ctx, c, "/v1/analyticsReportRequests", nil, body,
	)
	if err != nil {
		return "", err
	}
	if resp.Data.ID == "" {
		return "", errors.New("asc: RequestAnalyticsReport: empty id in response")
	}
	return RequestID(resp.Data.ID), nil
}

// ---------------------------------------------------------------------------
// 2. Poll the request → list of reports as they appear
// ---------------------------------------------------------------------------

// PollAnalyticsReport yields available AnalyticsReport entries as they become
// ready, ordered by Apple's response order. The iterator:
//
//   - Calls GET /v1/analyticsReportRequests/{id}/reports on a backoff schedule
//     governed by PollOpts (defaults: 30s→5m, 1.5x).
//   - Yields each newly observed report exactly once (deduped by ReportID).
//   - Re-fetches the parent request each iteration to honour
//     stoppedDueToInactivity (ONGOING) — yields ErrAnalyticsRequestStopped
//     and terminates.
//   - Honours ctx.Done() between polls; yields ctx.Err() and terminates.
//   - Honours PollOpts.MaxAttempts (0 = unlimited).
//
// The iterator does NOT block on instance/segment readiness — it only tells
// the caller which reports exist. To download data, the caller drives:
//
//	for report, err := range c.PollAnalyticsReport(ctx, id, opts) {
//	    if err != nil { return err }
//	    instances, err := c.ListAnalyticsInstances(ctx, report.ID)
//	    ...
//	}
//
// Apple's first poll often returns an empty array; PollAnalyticsReport keeps
// polling until reports appear or the context cancels.
func (c *Client) PollAnalyticsReport(ctx context.Context, id RequestID, opts PollOpts) iter.Seq2[AnalyticsReport, error] {
	o := opts.resolveDefaults()
	return func(yield func(AnalyticsReport, error) bool) {
		if id == "" {
			yield(AnalyticsReport{}, errors.New("asc: PollAnalyticsReport: empty RequestID"))
			return
		}

		seen := make(map[ReportID]struct{})
		backoff := o.InitialBackoff
		attempts := 0

		for {
			// Check cancellation first so a ctx that's already cancelled exits
			// without firing a wasted HTTP request.
			if err := ctx.Err(); err != nil {
				yield(AnalyticsReport{}, err)
				return
			}

			// 1. Re-fetch the request so we can detect stoppedDueToInactivity.
			reqInfo, err := c.getAnalyticsRequest(ctx, id)
			if err != nil {
				yield(AnalyticsReport{}, err)
				return
			}
			if reqInfo.StoppedDueToInactivity {
				yield(AnalyticsReport{}, ErrAnalyticsRequestStopped)
				return
			}

			// 2. List reports on the request. New ones (not in `seen`) yield.
			reports, err := c.listReportsForRequest(ctx, id)
			if err != nil {
				yield(AnalyticsReport{}, err)
				return
			}
			progressed := false
			for _, r := range reports {
				if _, dup := seen[r.ID]; dup {
					continue
				}
				seen[r.ID] = struct{}{}
				progressed = true
				if !yield(r, nil) {
					return
				}
			}

			// 3. ONE_TIME_SNAPSHOT: once we have ANY reports and a follow-up
			// poll yields no new ones, stop. We can't distinguish "more
			// reports coming" from "all delivered" from the response alone,
			// so the heuristic is: after first non-empty page, one quiet
			// follow-up = done. ONGOING never auto-terminates here; caller
			// drives termination via ctx or stoppedDueToInactivity.
			if reqInfo.AccessType == AccessTypeOneTimeSnapshot && len(seen) > 0 && !progressed {
				return
			}

			attempts++
			if o.MaxAttempts > 0 && attempts >= o.MaxAttempts {
				return
			}

			// 4. Sleep with backoff, but wake on ctx cancel.
			select {
			case <-ctx.Done():
				yield(AnalyticsReport{}, ctx.Err())
				return
			case <-time.After(backoff):
			}
			next := time.Duration(float64(backoff) * o.Multiplier)
			if next > o.MaxBackoff {
				next = o.MaxBackoff
			}
			backoff = next
		}
	}
}

// getAnalyticsRequest hydrates the parent request resource so we can read
// stoppedDueToInactivity and accessType during the poll loop. Single GET.
func (c *Client) getAnalyticsRequest(ctx context.Context, id RequestID) (AnalyticsReportRequestAttributes, error) {
	resp, err := Get[Single[AnalyticsReportRequestAttributes]](
		ctx, c, "/v1/analyticsReportRequests/"+url.PathEscape(string(id)), nil,
	)
	if err != nil {
		return AnalyticsReportRequestAttributes{}, err
	}
	return resp.Data.Attributes, nil
}

// listReportsForRequest fetches the current set of analyticsReports for a
// request. Apple paginates; we walk every page and flatten to []AnalyticsReport.
func (c *Client) listReportsForRequest(ctx context.Context, id RequestID) ([]AnalyticsReport, error) {
	var out []AnalyticsReport
	path := "/v1/analyticsReportRequests/" + url.PathEscape(string(id)) + "/reports"
	for {
		resp, err := Get[Collection[AnalyticsReportAttributes]](ctx, c, path, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range resp.Data {
			out = append(out, AnalyticsReport{
				ID:        ReportID(row.ID),
				RequestID: id,
				Name:      row.Attributes.Name,
				Category:  row.Attributes.Category,
			})
		}
		if resp.Links.Next == "" {
			return out, nil
		}
		path = resp.Links.Next
	}
}

// ListAnalyticsInstances returns every instance of a report (across all
// granularities and processing dates). Synchronous; walks all pages.
func (c *Client) ListAnalyticsInstances(ctx context.Context, reportID ReportID) ([]AnalyticsReportInstance, error) {
	if reportID == "" {
		return nil, errors.New("asc: ListAnalyticsInstances: empty ReportID")
	}
	var out []AnalyticsReportInstance
	path := "/v1/analyticsReports/" + url.PathEscape(string(reportID)) + "/instances"
	for {
		resp, err := Get[Collection[AnalyticsReportInstanceAttributes]](ctx, c, path, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range resp.Data {
			out = append(out, AnalyticsReportInstance{
				ID:             InstanceID(row.ID),
				ReportID:       reportID,
				Granularity:    row.Attributes.Granularity,
				ProcessingDate: row.Attributes.ProcessingDate,
			})
		}
		if resp.Links.Next == "" {
			return out, nil
		}
		path = resp.Links.Next
	}
}

// ListAnalyticsSegments returns every segment for one instance. Each segment
// carries a pre-signed URL that DownloadAnalyticsSegment fetches without
// injecting an Authorization header.
func (c *Client) ListAnalyticsSegments(ctx context.Context, instanceID InstanceID) ([]AnalyticsReportSegment, error) {
	if instanceID == "" {
		return nil, errors.New("asc: ListAnalyticsSegments: empty InstanceID")
	}
	var out []AnalyticsReportSegment
	path := "/v1/analyticsReportInstances/" + url.PathEscape(string(instanceID)) + "/segments"
	for {
		resp, err := Get[Collection[AnalyticsReportSegmentAttributes]](ctx, c, path, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range resp.Data {
			out = append(out, AnalyticsReportSegment{
				ID:          row.ID,
				InstanceID:  instanceID,
				URL:         row.Attributes.URL,
				Checksum:    row.Attributes.Checksum,
				SizeInBytes: row.Attributes.SizeInBytes,
			})
		}
		if resp.Links.Next == "" {
			return out, nil
		}
		path = resp.Links.Next
	}
}

// ---------------------------------------------------------------------------
// 3. Download a segment (pre-signed CDN URL — NO Authorization header).
// ---------------------------------------------------------------------------

// downloadCapBytes is the upper bound on a single segment download. Apple's
// segments are typically a few MiB; 256 MiB is generous defense against a
// runaway response and ten-fold the largest segment seen in practice.
const downloadCapBytes = 256 << 20

// DownloadAnalyticsSegment downloads a segment by ID and returns the
// gunzipped bytes. The data path is two GETs:
//
//  1. GET /v1/analyticsReportSegments/{id} → resolve the pre-signed CDN URL
//     (using the JWT-injecting *Client.do path).
//  2. GET <pre-signed URL> with NO Authorization header — Apple's CDN rejects
//     bearer tokens on signed URLs (same pattern the beta-feedback screenshot
//     downloader uses; see internal/cmd/beta_feedback.go).
//
// The CDN response is gzipped; we transparently gunzip and return raw CSV
// bytes.
func (c *Client) DownloadAnalyticsSegment(ctx context.Context, segmentID string) ([]byte, error) {
	if segmentID == "" {
		return nil, errors.New("asc: DownloadAnalyticsSegment: empty segmentID")
	}

	resp, err := Get[Single[AnalyticsReportSegmentAttributes]](
		ctx, c, "/v1/analyticsReportSegments/"+url.PathEscape(segmentID), nil,
	)
	if err != nil {
		return nil, err
	}
	signedURL := resp.Data.Attributes.URL
	if signedURL == "" {
		return nil, fmt.Errorf("asc: DownloadAnalyticsSegment: segment %q has empty url", segmentID)
	}

	body, err := fetchSignedGzip(ctx, signedURL)
	if err != nil {
		return nil, fmt.Errorf("asc: DownloadAnalyticsSegment %q: %w", segmentID, err)
	}
	return body, nil
}

// fetchSignedGzip GETs a pre-signed URL with no Authorization header,
// reads up to downloadCapBytes, and gunzips. Used by both the analytics
// segment download and (transparently — same shape) the sales/finance
// helpers below since Apple returns "application/a-gzip" for those too.
//
// The "no Authorization header" rule is critical: Apple's CDN signs the URL
// with a query-string SHA, and any extra header at the wrong layer flips the
// signature. http.DefaultClient is correct here precisely because it does
// not run the JWT middleware that the *Client.do path injects.
func fetchSignedGzip(ctx context.Context, signedURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, downloadCapBytes)

	// gzip.NewReader returns io.EOF cleanly when the body is well-formed.
	// If Apple ever serves an already-decompressed body, fall back to raw.
	gz, err := gzip.NewReader(limited)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	out, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 4. Synchronous sales / finance reports (gzipped CSV passthrough)
// ---------------------------------------------------------------------------

// SalesFrequency is the time bucket of a sales report (DAILY / WEEKLY /
// MONTHLY / YEARLY).
type SalesFrequency string

// Sales frequency literals.
const (
	SalesFrequencyDaily   SalesFrequency = "DAILY"
	SalesFrequencyWeekly  SalesFrequency = "WEEKLY"
	SalesFrequencyMonthly SalesFrequency = "MONTHLY"
	SalesFrequencyYearly  SalesFrequency = "YEARLY"
)

// SalesReportType mirrors the spec enum at filter[reportType].
type SalesReportType string

// Sales report type literals.
const (
	SalesReportTypeSales                   SalesReportType = "SALES"
	SalesReportTypePreOrder                SalesReportType = "PRE_ORDER"
	SalesReportTypeNewsstand               SalesReportType = "NEWSSTAND"
	SalesReportTypeSubscription            SalesReportType = "SUBSCRIPTION"
	SalesReportTypeSubscriptionEvent       SalesReportType = "SUBSCRIPTION_EVENT"
	SalesReportTypeSubscriber              SalesReportType = "SUBSCRIBER"
	SalesReportTypeSubscriptionOfferRedeem SalesReportType = "SUBSCRIPTION_OFFER_CODE_REDEMPTION"
	SalesReportTypeInstalls                SalesReportType = "INSTALLS"
	SalesReportTypeFirstAnnual             SalesReportType = "FIRST_ANNUAL"
	SalesReportTypeWinBackEligibility      SalesReportType = "WIN_BACK_ELIGIBILITY"
)

// SalesReportSubType mirrors the spec enum at filter[reportSubType].
type SalesReportSubType string

// Sales report sub-type literals.
const (
	SalesReportSubTypeSummary            SalesReportSubType = "SUMMARY"
	SalesReportSubTypeDetailed           SalesReportSubType = "DETAILED"
	SalesReportSubTypeSummaryInstallType SalesReportSubType = "SUMMARY_INSTALL_TYPE"
	SalesReportSubTypeSummaryTerritory   SalesReportSubType = "SUMMARY_TERRITORY"
	SalesReportSubTypeSummaryChannel     SalesReportSubType = "SUMMARY_CHANNEL"
)

// SalesReportParams holds the filter[…] query parameters for /v1/salesReports.
//
// VendorNumber, ReportType, ReportSubType, Frequency are required by Apple.
// ReportDate is optional for some sub-types (Apple defaults to "latest").
// Version (optional) pins to a specific Sales report schema version.
type SalesReportParams struct {
	VendorNumber  string
	ReportType    SalesReportType
	ReportSubType SalesReportSubType
	Frequency     SalesFrequency
	ReportDate    string // YYYY-MM-DD / YYYY-MM / YYYY-Www / YYYY (per frequency); optional
	Version       string // optional
}

// FinanceReportType mirrors the spec enum at filter[reportType] for finance.
type FinanceReportType string

// Finance report type literals.
const (
	FinanceReportTypeFinancial     FinanceReportType = "FINANCIAL"
	FinanceReportTypeFinanceDetail FinanceReportType = "FINANCE_DETAIL"
)

// FinanceReportParams holds the filter[…] params for /v1/financeReports.
//
// VendorNumber, ReportType, RegionCode, ReportDate are all required.
type FinanceReportParams struct {
	VendorNumber string
	ReportType   FinanceReportType
	RegionCode   string // ISO region code, e.g. "US", "Z1" (worldwide)
	ReportDate   string // YYYY-MM (Apple's finance reports are monthly)
}

// FetchSalesReport hits /v1/salesReports synchronously and returns the
// gunzipped CSV bytes. Apple responds with content-type "application/a-gzip";
// we always gunzip transparently.
//
// Returns an *APIError on Apple non-2xx (typed, with errors[] payload).
func (c *Client) FetchSalesReport(ctx context.Context, params SalesReportParams) ([]byte, error) {
	if err := validateSalesParams(params); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("filter[vendorNumber]", params.VendorNumber)
	q.Set("filter[reportType]", string(params.ReportType))
	q.Set("filter[reportSubType]", string(params.ReportSubType))
	q.Set("filter[frequency]", string(params.Frequency))
	if params.ReportDate != "" {
		q.Set("filter[reportDate]", params.ReportDate)
	}
	if params.Version != "" {
		q.Set("filter[version]", params.Version)
	}
	return c.fetchGzipReport(ctx, "/v1/salesReports", q)
}

// FetchFinanceReport hits /v1/financeReports synchronously and returns the
// gunzipped CSV bytes.
func (c *Client) FetchFinanceReport(ctx context.Context, params FinanceReportParams) ([]byte, error) {
	if err := validateFinanceParams(params); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("filter[vendorNumber]", params.VendorNumber)
	q.Set("filter[reportType]", string(params.ReportType))
	q.Set("filter[regionCode]", params.RegionCode)
	q.Set("filter[reportDate]", params.ReportDate)
	return c.fetchGzipReport(ctx, "/v1/financeReports", q)
}

// fetchGzipReport runs an Apple-authenticated GET against an endpoint that
// returns "application/a-gzip", then gunzips. Unlike DownloadAnalyticsSegment
// (which fetches a pre-signed URL with no Authorization header), this path
// uses the JWT-injecting *Client.do — Apple's report endpoints are
// authenticated and live on api.appstoreconnect.apple.com.
func (c *Client) fetchGzipReport(ctx context.Context, path string, query url.Values) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.errorFromResponse(resp)
	}

	limited := io.LimitReader(resp.Body, downloadCapBytes)
	gz, err := gzip.NewReader(limited)
	if err != nil {
		return nil, fmt.Errorf("asc: gunzip %s: %w", strings.TrimPrefix(path, "/v1/"), err)
	}
	defer func() { _ = gz.Close() }()
	out, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("asc: read %s body: %w", strings.TrimPrefix(path, "/v1/"), err)
	}
	return out, nil
}

// validateSalesParams enforces Apple's required filter[...] set. Returns an
// actionable error naming the empty field rather than letting Apple respond
// with a generic 400.
func validateSalesParams(p SalesReportParams) error {
	switch {
	case p.VendorNumber == "":
		return errors.New("asc: SalesReportParams.VendorNumber is required (set APP_STORE_CONNECT_VENDOR_NUMBER)")
	case p.ReportType == "":
		return errors.New("asc: SalesReportParams.ReportType is required (e.g. SALES, SUBSCRIPTION)")
	case p.ReportSubType == "":
		return errors.New("asc: SalesReportParams.ReportSubType is required (e.g. SUMMARY, DETAILED)")
	case p.Frequency == "":
		return errors.New("asc: SalesReportParams.Frequency is required (DAILY/WEEKLY/MONTHLY/YEARLY)")
	}
	return nil
}

// validateFinanceParams enforces Apple's required filter[...] set for
// /v1/financeReports.
func validateFinanceParams(p FinanceReportParams) error {
	switch {
	case p.VendorNumber == "":
		return errors.New("asc: FinanceReportParams.VendorNumber is required (set APP_STORE_CONNECT_VENDOR_NUMBER)")
	case p.ReportType == "":
		return errors.New("asc: FinanceReportParams.ReportType is required (FINANCIAL or FINANCE_DETAIL)")
	case p.RegionCode == "":
		return errors.New("asc: FinanceReportParams.RegionCode is required (ISO region, e.g. US or Z1 for worldwide)")
	case p.ReportDate == "":
		return errors.New("asc: FinanceReportParams.ReportDate is required (YYYY-MM)")
	}
	return nil
}
