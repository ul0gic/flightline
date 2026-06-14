package asc

import (
	"context"
	"testing"
	"time"
)

// resumeRun1 seeds the fixture with RPT-A + RPT-B, polls until both appear,
// checkpoints AsyncState, then cancels. Returns the two yielded reports.
func resumeRun1(t *testing.T, f *asyncFixture) []AnalyticsReport {
	t.Helper()
	c := asyncFixtureClient(t, f)
	f.mu.Lock()
	f.reports = []reportRow{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "Commerce", Category: CategoryCommerce},
	}
	f.accessType = AccessTypeOngoing
	f.mu.Unlock()

	ctx, cancel := context.WithCancel(t.Context())
	var got []AnalyticsReport
	for r, err := range c.PollAnalyticsReport(ctx, f.requestID, fastPoll) {
		if err != nil {
			break
		}
		got = append(got, r)
		if len(got) == 2 {
			persisted := AsyncState{
				BundleID:    "com.example.testapp",
				ReportClass: ReportClassAnalytics,
				RequestID:   f.requestID,
				SubmittedAt: time.Now().UTC().Add(-time.Hour),
				LastPollAt:  time.Now().UTC(),
				Status:      "processing",
				Reports: []PersistedAnalyticsReport{
					{ID: got[0].ID, Name: got[0].Name, Category: got[0].Category},
					{ID: got[1].ID, Name: got[1].Name, Category: got[1].Category},
				},
			}
			if err := PersistAsyncState(persisted); err != nil {
				t.Fatalf("Run 1 persist: %v", err)
			}
			cancel()
			break
		}
	}
	cancel()
	if len(got) != 2 {
		t.Fatalf("Run 1 yielded %d reports, want 2", len(got))
	}
	return got
}

// resumeRun2 polls with a fresh client, deduping against loaded.Reports.
// Returns the new-only reports and the count of already-seen yields.
func resumeRun2(t *testing.T, f *asyncFixture, loaded AsyncState) (newReports []AnalyticsReport, yieldedAlready int) {
	t.Helper()
	c := asyncFixtureClient(t, f)
	already := make(map[ReportID]struct{}, len(loaded.Reports))
	for _, r := range loaded.Reports {
		already[r.ID] = struct{}{}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	for r, err := range c.PollAnalyticsReport(ctx, loaded.RequestID, fastPoll) {
		if err != nil {
			break
		}
		if _, dup := already[r.ID]; dup {
			yieldedAlready++
			continue
		}
		newReports = append(newReports, r)
	}
	return newReports, yieldedAlready
}

// TestResume_AfterCtrlC_PicksUpFromCheckpoint verifies that a second session
// deduplicates against persisted AsyncState and yields only the new report.
func TestResume_AfterCtrlC_PicksUpFromCheckpoint(t *testing.T) {
	// No t.Parallel: t.Setenv inside withStateRoot is incompatible with
	// parallel siblings in the same package.
	withStateRoot(t)
	f := newAsyncFixture(t)

	resumeRun1(t, f)

	f.mu.Lock()
	f.reports = append(f.reports,
		reportRow{ID: "RPT-C", Name: "Engagement", Category: CategoryAppStoreEngagement})
	f.mu.Unlock()

	loaded, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}

	run2, yieldedAlready := resumeRun2(t, f, loaded)

	if yieldedAlready == 0 {
		t.Errorf("Run 2 never re-yielded a checkpointed report: fixture/dedup setup is wrong")
	}
	if len(run2) != 1 {
		t.Fatalf("Run 2 yielded %d new reports after dedup, want exactly 1 (RPT-C): %+v", len(run2), run2)
	}
	if run2[0].ID != "RPT-C" {
		t.Errorf("Run 2 yielded %q, want RPT-C", run2[0].ID)
	}
	if loaded.RequestID != f.requestID {
		t.Errorf("loaded.RequestID = %q, want %q", loaded.RequestID, f.requestID)
	}
	if len(loaded.Reports) != 2 {
		t.Errorf("loaded.Reports = %d, want 2 (RPT-A + RPT-B)", len(loaded.Reports))
	}
	if loaded.Status != "processing" {
		t.Errorf("loaded.Status = %q, want processing", loaded.Status)
	}
}

// TestResume_CompletedStateSkipsHTTP verifies that a status=completed disk state loads with zero
// analytics-backend calls. reportListCalls is the canary: any reports GET increments it.
func TestResume_CompletedStateSkipsHTTP(t *testing.T) {
	withStateRoot(t)

	f := newAsyncFixture(t)

	if err := PersistAsyncState(AsyncState{
		BundleID:    "com.example.testapp",
		ReportClass: ReportClassAnalytics,
		RequestID:   f.requestID,
		SubmittedAt: time.Now().UTC().Add(-2 * time.Hour),
		LastPollAt:  time.Now().UTC().Add(-time.Hour),
		Status:      "completed",
		Reports: []PersistedAnalyticsReport{
			{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
			{ID: "RPT-B", Name: "Commerce", Category: CategoryCommerce},
		},
		DownloadedSegments: []string{"SEG-1", "SEG-2"},
	}); err != nil {
		t.Fatalf("seed completed state: %v", err)
	}

	before := f.reportListCalls.Load()

	loaded, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}
	if loaded.Status != "completed" {
		t.Fatalf("loaded.Status = %q, want completed", loaded.Status)
	}
	if len(loaded.Reports) != 2 {
		t.Errorf("loaded.Reports = %d, want 2", len(loaded.Reports))
	}
	if len(loaded.DownloadedSegments) != 2 {
		t.Errorf("loaded.DownloadedSegments = %v, want 2", loaded.DownloadedSegments)
	}

	// Canary: the load-only path must not have hit the analytics backend.
	if got := f.reportListCalls.Load(); got != before {
		t.Errorf("LoadAsyncState made %d analytics-backend calls, want 0", got-before)
	}
}

// TestResume_PreservesDownloadedSegments asserts the persisted DownloadedSegments list survives a
// round-trip; dedup is caller-side (the wrapper doesn't dedup downloads).
func TestResume_PreservesDownloadedSegments(t *testing.T) {
	withStateRoot(t)

	if err := PersistAsyncState(AsyncState{
		BundleID:           "com.example.testapp",
		ReportClass:        ReportClassAnalytics,
		RequestID:          "REQ-RESUME",
		SubmittedAt:        time.Now().UTC().Add(-time.Hour),
		Status:             "processing",
		DownloadedSegments: []string{"SEG-1"},
	}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	loaded, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	already := make(map[string]struct{}, len(loaded.DownloadedSegments))
	for _, s := range loaded.DownloadedSegments {
		already[s] = struct{}{}
	}
	if _, ok := already["SEG-1"]; !ok {
		t.Errorf("SEG-1 missing from DownloadedSegments after resume: %+v", loaded.DownloadedSegments)
	}
	if _, ok := already["SEG-2"]; ok {
		t.Error("SEG-2 falsely reported as already downloaded")
	}
}
