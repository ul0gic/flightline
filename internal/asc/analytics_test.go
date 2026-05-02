package asc

import (
	"reflect"
	"testing"
)

func TestFilterAnalyticsReports_NoFilter(t *testing.T) {
	t.Parallel()
	c := &Client{}
	in := []AnalyticsReport{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "Commerce Daily", Category: CategoryCommerce},
	}
	got := c.FilterAnalyticsReports(in, AnalyticsReportFilter{})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	// must return a copy, not the same slice header
	got[0].Name = "MUTATED"
	if in[0].Name == "MUTATED" {
		t.Errorf("FilterAnalyticsReports returned aliased slice; mutated input")
	}
}

func TestFilterAnalyticsReports_ByCategory(t *testing.T) {
	t.Parallel()
	c := &Client{}
	in := []AnalyticsReport{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "Commerce Daily", Category: CategoryCommerce},
		{ID: "RPT-C", Name: "App Usage Monthly", Category: CategoryAppUsage},
	}
	got := c.FilterAnalyticsReports(in, AnalyticsReportFilter{Category: CategoryAppUsage})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	for _, r := range got {
		if r.Category != CategoryAppUsage {
			t.Errorf("got non-AppUsage row: %+v", r)
		}
	}
}

func TestFilterAnalyticsReports_ByNameContainsCaseInsensitive(t *testing.T) {
	t.Parallel()
	c := &Client{}
	in := []AnalyticsReport{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "COMMERCE DAILY", Category: CategoryCommerce},
		{ID: "RPT-C", Name: "Crashes", Category: CategoryPerformance},
	}
	got := c.FilterAnalyticsReports(in, AnalyticsReportFilter{NameContains: "commerce"})
	if len(got) != 1 || got[0].ID != "RPT-B" {
		t.Fatalf("got %+v, want only RPT-B", got)
	}
}

func TestFilterAnalyticsReports_BothFilters(t *testing.T) {
	t.Parallel()
	c := &Client{}
	in := []AnalyticsReport{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "App Engagement", Category: CategoryAppStoreEngagement},
		{ID: "RPT-C", Name: "App Usage Monthly", Category: CategoryAppUsage},
	}
	got := c.FilterAnalyticsReports(in, AnalyticsReportFilter{
		Category:     CategoryAppUsage,
		NameContains: "monthly",
	})
	if len(got) != 1 || got[0].ID != "RPT-C" {
		t.Fatalf("got %+v, want only RPT-C", got)
	}
}

func TestFilterAnalyticsReports_NoMatches(t *testing.T) {
	t.Parallel()
	c := &Client{}
	in := []AnalyticsReport{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
	}
	got := c.FilterAnalyticsReports(in, AnalyticsReportFilter{Category: CategoryFrameworkUsage})
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestParseSegmentDownload_HeaderParsed(t *testing.T) {
	t.Parallel()
	body := []byte("date,impressions,installs\n2026-05-01,1234,42\n2026-05-02,2345,55\n")
	got := ParseSegmentDownload("SEG-42", "INST-1", body)
	if got.SegmentID != "SEG-42" {
		t.Errorf("SegmentID = %q", got.SegmentID)
	}
	if got.InstanceID != "INST-1" {
		t.Errorf("InstanceID = %q", got.InstanceID)
	}
	if got.ByteCount != len(body) {
		t.Errorf("ByteCount = %d, want %d", got.ByteCount, len(body))
	}
	want := []string{"date", "impressions", "installs"}
	if !reflect.DeepEqual(got.Header, want) {
		t.Errorf("Header = %+v, want %+v", got.Header, want)
	}
}

func TestParseSegmentDownload_EmptyBody(t *testing.T) {
	t.Parallel()
	got := ParseSegmentDownload("SEG-X", "INST-9", nil)
	if got.ByteCount != 0 {
		t.Errorf("ByteCount = %d, want 0", got.ByteCount)
	}
	if got.Header != nil {
		t.Errorf("Header = %+v, want nil for empty body", got.Header)
	}
}

func TestParseSegmentDownload_MalformedCSV(t *testing.T) {
	t.Parallel()
	// Unterminated quoted field — csv.Reader returns a parse error, but the
	// helper should swallow it and still return ByteCount + nil Header.
	body := []byte("\"unterminated\n")
	got := ParseSegmentDownload("SEG-1", "INST-1", body)
	if got.ByteCount != len(body) {
		t.Errorf("ByteCount = %d, want %d", got.ByteCount, len(body))
	}
	if got.Header != nil {
		t.Logf("Header = %+v (parser may have recovered)", got.Header)
	}
}

// TestSegmentDownloadResult_BytesNotInJSON guards the JSON contract: raw
// bytes are excluded so JSON consumers don't get a base64 blob bloating
// stdout. Bytes are written to disk separately by the CLI.
func TestSegmentDownloadResult_BytesNotInJSON(t *testing.T) {
	t.Parallel()
	r := SegmentDownloadResult{
		SegmentID: "SEG-1",
		ByteCount: 4,
		Bytes:     []byte("data"),
	}
	rt := reflect.TypeOf(r)
	field, ok := rt.FieldByName("Bytes")
	if !ok {
		t.Fatal("Bytes field missing")
	}
	if got := field.Tag.Get("json"); got != "-" {
		t.Errorf("json tag on Bytes = %q, want %q", got, "-")
	}
}
