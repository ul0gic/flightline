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

// RequestID is a typed analytics report request identifier (POST /v1/analyticsReportRequests).
type RequestID string

// ReportID is an analytics report identifier; one row under a RequestID, with many InstanceIDs.
type ReportID string

// InstanceID is an analytics report instance identifier, scoped to a granularity + processing date.
type InstanceID string

// AnalyticsAccessType is the access pattern: ONE_TIME_SNAPSHOT or ONGOING.
type AnalyticsAccessType string

const (
	// AccessTypeOneTimeSnapshot pulls a snapshot once.
	AccessTypeOneTimeSnapshot AnalyticsAccessType = "ONE_TIME_SNAPSHOT"
	// AccessTypeOngoing keeps producing reports over time.
	AccessTypeOngoing AnalyticsAccessType = "ONGOING"
)

// AnalyticsCategory is the high-level report grouping (APP_USAGE, COMMERCE, etc.).
type AnalyticsCategory string

const (
	CategoryAppUsage           AnalyticsCategory = "APP_USAGE"
	CategoryAppStoreEngagement AnalyticsCategory = "APP_STORE_ENGAGEMENT"
	CategoryCommerce           AnalyticsCategory = "COMMERCE"
	CategoryFrameworkUsage     AnalyticsCategory = "FRAMEWORK_USAGE"
	CategoryPerformance        AnalyticsCategory = "PERFORMANCE"
)

// AnalyticsGranularity is the time-bucket size of an analytics report instance (DAILY/WEEKLY/MONTHLY).
type AnalyticsGranularity string

const (
	GranularityDaily   AnalyticsGranularity = "DAILY"
	GranularityWeekly  AnalyticsGranularity = "WEEKLY"
	GranularityMonthly AnalyticsGranularity = "MONTHLY"
)

// AnalyticsReportRequestAttributes mirrors AnalyticsReportRequest.attributes from the spec.
type AnalyticsReportRequestAttributes struct {
	AccessType             AnalyticsAccessType `json:"accessType,omitempty"`
	StoppedDueToInactivity bool                `json:"stoppedDueToInactivity,omitempty"`
}

// AnalyticsReportAttributes mirrors AnalyticsReport.attributes from the spec.
type AnalyticsReportAttributes struct {
	Name     string            `json:"name,omitempty"`
	Category AnalyticsCategory `json:"category,omitempty"`
}

// AnalyticsReport is the flat view of one report row. RequestID is filled in by the wrapper;
// Apple's response doesn't echo it on the report rows.
type AnalyticsReport struct {
	ID        ReportID          `json:"id"`
	RequestID RequestID         `json:"requestId,omitempty"`
	Name      string            `json:"name,omitempty"`
	Category  AnalyticsCategory `json:"category,omitempty"`
}

// AnalyticsRequestSnapshot is one non-blocking refresh of an analytics
// request and every currently available report page.
type AnalyticsRequestSnapshot struct {
	Attributes AnalyticsReportRequestAttributes
	Reports    []AnalyticsReport
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

// AnalyticsReportSegment is the flat view of one segment. URL is a pre-signed CDN URL; omit Authorization when fetching it.
type AnalyticsReportSegment struct {
	ID          string     `json:"id"`
	InstanceID  InstanceID `json:"instanceId,omitempty"`
	URL         string     `json:"url,omitempty"`
	Checksum    string     `json:"checksum,omitempty"`
	SizeInBytes int64      `json:"sizeInBytes,omitempty"`
}

// AnalyticsReportRequestParams is the input to RequestAnalyticsReport.
type AnalyticsReportRequestParams struct {
	AppID      string
	AccessType AnalyticsAccessType
}

// PollOpts configures the analytics polling loop's exponential backoff.
// Defaults: 30s initial, 5m cap, 1.5x growth. Override conservatively: Apple caps at 500 req/hr.
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

// ErrAnalyticsRequestStopped is returned by PollAnalyticsReport when Apple sets stoppedDueToInactivity (ONGOING only).
var ErrAnalyticsRequestStopped = errors.New("asc: analytics request stopped due to inactivity")

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

// RequestAnalyticsReport submits a new analytics report request and returns its ID.
// Reports become available over minutes-to-hours; retrieve them via PollAnalyticsReport.
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

// PollAnalyticsReport yields AnalyticsReport entries as they become ready, deduped by ID.
// Polls on an exponential backoff (PollOpts); stops on ctx cancel, stoppedDueToInactivity, or MaxAttempts.
//
//	for report, err := range c.PollAnalyticsReport(ctx, id, opts) {
//	    if err != nil { return err }
//	    instances, _ := c.ListAnalyticsInstances(ctx, report.ID)
//	}
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
			done, err := c.runPollStep(ctx, id, seen, yield)
			if err != nil || done {
				return
			}

			attempts++
			if o.MaxAttempts > 0 && attempts >= o.MaxAttempts {
				return
			}

			if !sleepWithCancel(ctx, backoff, yield) {
				return
			}
			backoff = nextBackoff(backoff, o.Multiplier, o.MaxBackoff)
		}
	}
}

func (c *Client) runPollStep(
	ctx context.Context,
	id RequestID,
	seen map[ReportID]struct{},
	yield func(AnalyticsReport, error) bool,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		yield(AnalyticsReport{}, err)
		return false, err
	}

	reqInfo, err := c.getAnalyticsRequest(ctx, id)
	if err != nil {
		yield(AnalyticsReport{}, err)
		return false, err
	}
	if reqInfo.StoppedDueToInactivity {
		yield(AnalyticsReport{}, ErrAnalyticsRequestStopped)
		return false, ErrAnalyticsRequestStopped
	}

	reports, err := c.listReportsForRequest(ctx, id)
	if err != nil {
		yield(AnalyticsReport{}, err)
		return false, err
	}

	progressed, halted := yieldNewReports(reports, seen, yield)
	if halted {
		return true, nil
	}

	// ONE_TIME_SNAPSHOT terminates after the first quiet follow-up (a report exists, latest poll
	// added none); ONGOING stays alive until ctx cancels or Apple flips stoppedDueToInactivity.
	if reqInfo.AccessType == AccessTypeOneTimeSnapshot && len(seen) > 0 && !progressed {
		return true, nil
	}
	return false, nil
}

// RefreshAnalyticsRequest performs a single read of the request plus its
// currently available reports. It is the resumable counterpart to the
// blocking poller used by status and list-instances after a process restart.
func (c *Client) RefreshAnalyticsRequest(ctx context.Context, id RequestID) (AnalyticsRequestSnapshot, error) {
	if id == "" {
		return AnalyticsRequestSnapshot{}, errors.New("asc: RefreshAnalyticsRequest: empty RequestID")
	}
	attrs, err := c.getAnalyticsRequest(ctx, id)
	if err != nil {
		return AnalyticsRequestSnapshot{}, err
	}
	reports, err := c.listReportsForRequest(ctx, id)
	if err != nil {
		return AnalyticsRequestSnapshot{}, err
	}
	if reports == nil {
		reports = []AnalyticsReport{}
	}
	return AnalyticsRequestSnapshot{Attributes: attrs, Reports: reports}, nil
}

func yieldNewReports(
	reports []AnalyticsReport,
	seen map[ReportID]struct{},
	yield func(AnalyticsReport, error) bool,
) (progressed, halted bool) {
	for _, r := range reports {
		if _, dup := seen[r.ID]; dup {
			continue
		}
		seen[r.ID] = struct{}{}
		progressed = true
		if !yield(r, nil) {
			return progressed, true
		}
	}
	return progressed, false
}

func sleepWithCancel(ctx context.Context, d time.Duration, yield func(AnalyticsReport, error) bool) bool {
	select {
	case <-ctx.Done():
		yield(AnalyticsReport{}, ctx.Err())
		return false
	case <-time.After(d):
		return true
	}
}

func nextBackoff(current time.Duration, multiplier float64, maxBackoff time.Duration) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

// getAnalyticsRequest reads stoppedDueToInactivity and accessType, which the poll loop branches on.
func (c *Client) getAnalyticsRequest(ctx context.Context, id RequestID) (AnalyticsReportRequestAttributes, error) {
	resp, err := Get[Single[AnalyticsReportRequestAttributes]](
		ctx, c, "/v1/analyticsReportRequests/"+url.PathEscape(string(id)), nil,
	)
	if err != nil {
		return AnalyticsReportRequestAttributes{}, err
	}
	return resp.Data.Attributes, nil
}

// listReportsForRequest walks every page; Apple paginates the reports collection.
func (c *Client) listReportsForRequest(ctx context.Context, id RequestID) ([]AnalyticsReport, error) {
	out := make([]AnalyticsReport, 0)
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

// ListAnalyticsInstances returns every instance of a report across all granularities and dates.
func (c *Client) ListAnalyticsInstances(ctx context.Context, reportID ReportID) ([]AnalyticsReportInstance, error) {
	if reportID == "" {
		return nil, errors.New("asc: ListAnalyticsInstances: empty ReportID")
	}
	out := make([]AnalyticsReportInstance, 0)
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

// ListAnalyticsSegments returns every segment for one instance. Each carries a pre-signed URL
// that DownloadAnalyticsSegment fetches without an Authorization header (Apple's CDN rejects it).
func (c *Client) ListAnalyticsSegments(ctx context.Context, instanceID InstanceID) ([]AnalyticsReportSegment, error) {
	if instanceID == "" {
		return nil, errors.New("asc: ListAnalyticsSegments: empty InstanceID")
	}
	out := make([]AnalyticsReportSegment, 0)
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

// downloadCapBytes caps a single segment download. Apple's segments run a few MiB;
// 256 MiB is defense against a runaway response, ten-fold the largest seen in practice.
const downloadCapBytes = 256 << 20

// DownloadAnalyticsSegment resolves the pre-signed CDN URL for a segment and returns the gunzipped CSV bytes.
// The CDN GET omits Authorization: Apple's signed URLs reject bearer tokens.
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

// fetchSignedGzip GETs a pre-signed URL without Authorization and gunzips the response.
// http.DefaultClient is intentional: the JWT middleware in *Client.do would break Apple's CDN signature.
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

// SalesFrequency is the time bucket of a sales report (DAILY/WEEKLY/MONTHLY/YEARLY).
type SalesFrequency string

const (
	SalesFrequencyDaily   SalesFrequency = "DAILY"
	SalesFrequencyWeekly  SalesFrequency = "WEEKLY"
	SalesFrequencyMonthly SalesFrequency = "MONTHLY"
	SalesFrequencyYearly  SalesFrequency = "YEARLY"
)

// SalesReportType mirrors the spec enum at filter[reportType].
type SalesReportType string

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

const (
	SalesReportSubTypeSummary            SalesReportSubType = "SUMMARY"
	SalesReportSubTypeDetailed           SalesReportSubType = "DETAILED"
	SalesReportSubTypeSummaryInstallType SalesReportSubType = "SUMMARY_INSTALL_TYPE"
	SalesReportSubTypeSummaryTerritory   SalesReportSubType = "SUMMARY_TERRITORY"
	SalesReportSubTypeSummaryChannel     SalesReportSubType = "SUMMARY_CHANNEL"
)

// SalesReportParams holds the filter[...] parameters for /v1/salesReports.
// VendorNumber, ReportType, ReportSubType, Frequency are required; ReportDate and Version are optional.
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

const (
	FinanceReportTypeFinancial     FinanceReportType = "FINANCIAL"
	FinanceReportTypeFinanceDetail FinanceReportType = "FINANCE_DETAIL"
)

// FinanceReportParams holds the filter[...] parameters for /v1/financeReports. All four fields are required.
type FinanceReportParams struct {
	VendorNumber string
	ReportType   FinanceReportType
	RegionCode   string // e.g. "US", "ZZ" (consolidated), or "Z1" (finance detail)
	ReportDate   string // YYYY-MM (Apple's finance reports are monthly)
}

// FetchSalesReport hits /v1/salesReports and returns gunzipped CSV bytes. Returns *APIError on non-2xx.
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

// FetchFinanceReport hits /v1/financeReports and returns gunzipped CSV bytes.
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

// fetchGzipReport runs an authenticated GET against an "application/a-gzip" endpoint and gunzips.
// Unlike DownloadAnalyticsSegment, this path uses JWT auth: Apple's report endpoints require it.
func (c *Client) fetchGzipReport(ctx context.Context, path string, query url.Values) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, path, query, nil, "application/a-gzip")
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

// validateSalesParams names the empty required field locally rather than letting Apple return a generic 400.
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

// validateFinanceParams names the empty required field locally rather than letting Apple return a generic 400.
func validateFinanceParams(p FinanceReportParams) error {
	switch {
	case p.VendorNumber == "":
		return errors.New("asc: FinanceReportParams.VendorNumber is required (set APP_STORE_CONNECT_VENDOR_NUMBER)")
	case p.ReportType == "":
		return errors.New("asc: FinanceReportParams.ReportType is required (FINANCIAL or FINANCE_DETAIL)")
	case p.RegionCode == "":
		return errors.New("asc: FinanceReportParams.RegionCode is required (e.g. ZZ for consolidated or Z1 for finance detail)")
	case p.ReportDate == "":
		return errors.New("asc: FinanceReportParams.ReportDate is required (YYYY-MM)")
	}
	return nil
}
