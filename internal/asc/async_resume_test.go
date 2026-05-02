package asc

// Resume-after-Ctrl-C integration test.
//
// Contract under test: when an operator interrupts a long-running analytics
// poll, the persisted AsyncState plus a fresh *Client must continue from the
// checkpoint — not re-yield reports that were already produced, not refetch
// segments that were already downloaded, and not even hit Apple at all when
// the persisted state already records status=completed.
//
// The test exercises three concrete scenarios:
//
//  1. TestResume_AfterCtrlC_PicksUpFromCheckpoint
//     Run 1 yields RPT-A and RPT-B, persists state, cancels mid-poll. Run 2
//     spins a fresh *Client against the same backend. The fixture serves
//     RPT-A, RPT-B, RPT-C — the resume path filters out the two checkpointed
//     IDs (the contract is caller-side dedup against AsyncState.Reports) and
//     only yields RPT-C.
//
//  2. TestResume_CompletedStateSkipsHTTP
//     Run 2 finds status=completed on disk and loads it without making any
//     HTTP calls to the analytics backend. Counted via an atomic.Int32 on
//     the fixture's request-list handler.
//
//  3. TestResume_PreservesDownloadedSegments
//     Validates that the segment-dedup half of the contract works the same
//     way: a SEG-1 already on disk before Run 2 starts is recognised as
//     already-downloaded and skipped.

import (
	"context"
	"testing"
	"time"
)

// TestResume_AfterCtrlC_PicksUpFromCheckpoint is the keystone integration
// test for the resume-after-Ctrl-C contract. It threads a single httptest
// backend across two distinct *Client instances — the second simulating a
// fresh process after the operator hits Ctrl-C — and asserts the second
// session does not duplicate work the first session already published to
// AsyncState.
func TestResume_AfterCtrlC_PicksUpFromCheckpoint(t *testing.T) {
	// No t.Parallel — t.Setenv inside withStateRoot is incompatible with
	// parallel siblings in the same package.
	withStateRoot(t)

	f := newAsyncFixture(t)

	// ---------- Run 1: poll, checkpoint, cancel. ----------
	c1 := asyncFixtureClient(t, f)

	// Seed RPT-A + RPT-B; the first session yields these and stops.
	f.mu.Lock()
	f.reports = []reportRow{
		{ID: "RPT-A", Name: "App Usage", Category: CategoryAppUsage},
		{ID: "RPT-B", Name: "Commerce", Category: CategoryCommerce},
	}
	f.accessType = AccessTypeOngoing // never auto-terminate from len(reports)
	f.mu.Unlock()

	ctx1, cancel1 := context.WithCancel(t.Context())
	var run1 []AnalyticsReport
	for r, err := range c1.PollAnalyticsReport(ctx1, f.requestID, fastPoll) {
		if err != nil {
			break
		}
		run1 = append(run1, r)
		if len(run1) == 2 {
			// Checkpoint: persist what we have so far, then simulate
			// Ctrl-C by cancelling.
			persisted := AsyncState{
				BundleID:    "com.example.testapp",
				ReportClass: ReportClassAnalytics,
				RequestID:   f.requestID,
				SubmittedAt: time.Now().UTC().Add(-time.Hour),
				LastPollAt:  time.Now().UTC(),
				Status:      "processing",
				Reports: []PersistedAnalyticsReport{
					{ID: run1[0].ID, Name: run1[0].Name, Category: run1[0].Category},
					{ID: run1[1].ID, Name: run1[1].Name, Category: run1[1].Category},
				},
			}
			if err := PersistAsyncState(persisted); err != nil {
				t.Fatalf("Run 1 persist: %v", err)
			}
			cancel1()
			break
		}
	}
	cancel1()
	if len(run1) != 2 {
		t.Fatalf("Run 1 yielded %d reports, want 2", len(run1))
	}

	// ---------- Run 2: fresh client + fresh ctx, same backend, same state. ----------
	c2 := asyncFixtureClient(t, f)

	// New report appears between sessions.
	f.mu.Lock()
	f.reports = append(f.reports,
		reportRow{ID: "RPT-C", Name: "Engagement", Category: CategoryAppStoreEngagement})
	f.mu.Unlock()

	loaded, err := LoadAsyncState("com.example.testapp", ReportClassAnalytics)
	if err != nil {
		t.Fatalf("LoadAsyncState: %v", err)
	}

	// Caller-side dedup contract: build a set from the persisted reports.
	already := make(map[ReportID]struct{}, len(loaded.Reports))
	for _, r := range loaded.Reports {
		already[r.ID] = struct{}{}
	}

	// Time-bounded second session — fixture is ONGOING so PollAnalyticsReport
	// will not self-terminate; we cap it at a few poll cycles.
	ctx2, cancel2 := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel2()

	// The wrapper does not dedup against AsyncState; the resume contract
	// is caller-side: the caller filters yields against the persisted
	// Reports set. Run 2 thus iterates RPT-A + RPT-B + RPT-C, but the
	// dedup filter must collapse the output to RPT-C only.
	var (
		run2           []AnalyticsReport
		yieldedTotal   int
		yieldedAlready int
	)
	for r, err := range c2.PollAnalyticsReport(ctx2, loaded.RequestID, fastPoll) {
		if err != nil {
			break
		}
		yieldedTotal++
		if _, dup := already[r.ID]; dup {
			yieldedAlready++
			continue
		}
		run2 = append(run2, r)
	}

	if yieldedAlready == 0 {
		t.Errorf("Run 2 never re-yielded a checkpointed report — fixture/dedup setup is wrong (yieldedTotal=%d)", yieldedTotal)
	}
	if len(run2) != 1 {
		t.Fatalf("Run 2 yielded %d new reports after dedup, want exactly 1 (RPT-C): %+v", len(run2), run2)
	}
	if run2[0].ID != "RPT-C" {
		t.Errorf("Run 2 yielded %q, want RPT-C", run2[0].ID)
	}

	// Persisted state preserved across the resume. RequestID, len(Reports),
	// SubmittedAt all carried through.
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

// TestResume_CompletedStateSkipsHTTP verifies that when the on-disk state
// records status=completed, the load-only path returns it without making any
// HTTP request to the analytics backend. The fixture's reportListCalls is
// the canary: any GET /v1/analyticsReportRequests/{id}/reports increments it.
func TestResume_CompletedStateSkipsHTTP(t *testing.T) {
	withStateRoot(t)

	f := newAsyncFixture(t)

	// Seed disk state — status=completed, two reports already produced.
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

	// Snapshot the canary before the load-only path.
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

// TestResume_PreservesDownloadedSegments validates the segment-dedup half of
// the resume contract: a SEG already in DownloadedSegments must be skipped
// by the caller (the wrapper itself doesn't dedup downloads — caller-side
// contract documented in async_state.go). Asserts the persistence path
// preserves the list and that membership lookup is the obvious set test.
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
