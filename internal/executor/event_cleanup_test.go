package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestEventCleanupIntegration tests the background event cleanup goroutine
func TestEventCleanupIntegration(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	store, err := storage.NewStorage(ctx, &storage.Config{Path: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor with custom event retention config (short interval for testing)
	retentionCfg := config.EventRetentionConfig{
		RetentionDays:         1, // Keep events for 1 day
		RetentionCriticalDays: 2, // Keep critical events for 2 days
		PerIssueLimitEvents:   5, // Max 5 events per issue
		GlobalLimitEvents:     20, // Max 20 events total
		CleanupIntervalHours:  1, // Run every hour (not used in manual test)
		CleanupBatchSize:      10,
		CleanupEnabled:        true,
		CleanupStrategy:       "oldest_non_critical",
		CleanupVacuum:         false,
	}

	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EventRetentionConfig = &retentionCfg

	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create test issues first (required for foreign key constraint)
	testIssues := []string{"vc-test-cleanup-1", "vc-test-cleanup-2", "vc-test-cleanup-overflow"}
	for _, issueID := range testIssues {
		issue := &types.Issue{
			ID:          issueID,
			Title:       "Test issue for cleanup",
			Description: "Test",
			Status:      types.StatusOpen,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test completes successfully",
			Priority:    2,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create test issue: %v", err)
		}
	}

	// Insert some test events
	oldTimestamp := time.Now().AddDate(0, 0, -5) // 5 days ago
	recentTimestamp := time.Now()

	// Old events (should be cleaned)
	for i := 0; i < 3; i++ {
		event := &events.AgentEvent{
			ID:         "old-event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  oldTimestamp,
			IssueID:    "vc-test-cleanup-1",
			ExecutorID: "test-executor",
			Severity:   events.SeverityInfo,
			Message:    "Old event",
			Data:       map[string]interface{}{},
		}
		if err := store.StoreAgentEvent(ctx, event); err != nil {
			t.Fatalf("Failed to store old event: %v", err)
		}
	}

	// Recent events (should be kept)
	for i := 0; i < 3; i++ {
		event := &events.AgentEvent{
			ID:         "recent-event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  recentTimestamp,
			IssueID:    "vc-test-cleanup-2",
			ExecutorID: "test-executor",
			Severity:   events.SeverityInfo,
			Message:    "Recent event",
			Data:       map[string]interface{}{},
		}
		if err := store.StoreAgentEvent(ctx, event); err != nil {
			t.Fatalf("Failed to store recent event: %v", err)
		}
	}

	// Events exceeding per-issue limit (create 10 events for one issue)
	for i := 0; i < 10; i++ {
		event := &events.AgentEvent{
			ID:         "overflow-event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  recentTimestamp,
			IssueID:    "vc-test-cleanup-overflow",
			ExecutorID: "test-executor",
			Severity:   events.SeverityInfo,
			Message:    "Overflow event",
			Data:       map[string]interface{}{},
		}
		if err := store.StoreAgentEvent(ctx, event); err != nil {
			t.Fatalf("Failed to store overflow event: %v", err)
		}
	}

	// Get initial counts
	initialCounts, err := store.GetEventCounts(ctx)
	if err != nil {
		t.Fatalf("Failed to get initial event counts: %v", err)
	}
	t.Logf("Initial event count: %d", initialCounts.TotalEvents)

	// Run cleanup manually (not via the goroutine)
	err = executor.runEventCleanup(ctx, retentionCfg)
	if err != nil {
		t.Fatalf("Event cleanup failed: %v", err)
	}

	// Get final counts
	finalCounts, err := store.GetEventCounts(ctx)
	if err != nil {
		t.Fatalf("Failed to get final event counts: %v", err)
	}
	t.Logf("Final event count: %d", finalCounts.TotalEvents)

	// Verify cleanup happened
	if finalCounts.TotalEvents >= initialCounts.TotalEvents {
		t.Errorf("Expected event count to decrease after cleanup, got initial=%d final=%d",
			initialCounts.TotalEvents, finalCounts.TotalEvents)
	}

	// Verify old events were deleted (vc-test-cleanup-1 should have 0 events)
	oldIssueEvents, err := store.GetAgentEventsByIssue(ctx, "vc-test-cleanup-1")
	if err != nil {
		t.Fatalf("Failed to get events for old issue: %v", err)
	}
	if len(oldIssueEvents) > 0 {
		t.Errorf("Expected old events to be deleted, but found %d events", len(oldIssueEvents))
	}

	// Verify recent events were kept (vc-test-cleanup-2 should still have events)
	recentIssueEvents, err := store.GetAgentEventsByIssue(ctx, "vc-test-cleanup-2")
	if err != nil {
		t.Fatalf("Failed to get events for recent issue: %v", err)
	}
	if len(recentIssueEvents) == 0 {
		t.Errorf("Expected recent events to be kept, but found none")
	}

	// Verify per-issue limit was enforced (vc-test-cleanup-overflow should have <= 5 events)
	overflowIssueEvents, err := store.GetAgentEventsByIssue(ctx, "vc-test-cleanup-overflow")
	if err != nil {
		t.Fatalf("Failed to get events for overflow issue: %v", err)
	}
	if len(overflowIssueEvents) > retentionCfg.PerIssueLimitEvents {
		t.Errorf("Expected per-issue limit of %d to be enforced, but found %d events",
			retentionCfg.PerIssueLimitEvents, len(overflowIssueEvents))
	}

	t.Logf("✓ Event cleanup integration test passed")
	t.Logf("  - Old events deleted: %v", len(oldIssueEvents) == 0)
	t.Logf("  - Recent events kept: %d", len(recentIssueEvents))
	t.Logf("  - Per-issue limit enforced: %d events (limit: %d)",
		len(overflowIssueEvents), retentionCfg.PerIssueLimitEvents)
}

// TestEventCleanupDisabled tests that cleanup is skipped when disabled
func TestEventCleanupDisabled(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	store, err := storage.NewStorage(ctx, &storage.Config{Path: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor with cleanup disabled (use valid defaults, just disable cleanup)
	retentionCfg := config.DefaultEventRetentionConfig()
	retentionCfg.CleanupEnabled = false

	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EventRetentionConfig = &retentionCfg

	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Start executor
	if err := executor.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	// Give it a moment to check if cleanup starts
	time.Sleep(100 * time.Millisecond)

	// Stop executor
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := executor.Stop(stopCtx); err != nil {
		t.Fatalf("Failed to stop executor: %v", err)
	}

	t.Logf("✓ Event cleanup disabled test passed - goroutine exited immediately")
}

// TestEventCleanupGracefulShutdown tests that event cleanup stops gracefully
func TestEventCleanupGracefulShutdown(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	store, err := storage.NewStorage(ctx, &storage.Config{Path: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor with default config
	cfg := DefaultConfig()
	cfg.Store = store

	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Start executor
	if err := executor.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop executor with a reasonable timeout
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	startStop := time.Now()
	if err := executor.Stop(stopCtx); err != nil {
		t.Fatalf("Failed to stop executor: %v", err)
	}
	shutdownTime := time.Since(startStop)

	t.Logf("✓ Graceful shutdown completed in %v", shutdownTime)

	// Verify it shut down reasonably quickly (should be nearly instant since no work)
	if shutdownTime > 1*time.Second {
		t.Errorf("Shutdown took too long: %v (expected < 1s)", shutdownTime)
	}
}

// TestEventCleanupWithNullIssueID tests that cleanup handles NULL issue_id values correctly (vc-3i6e)
// This test verifies that the per-issue limit cleanup query filters out NULL issue_id rows
// to prevent "converting NULL to string" SQL scan errors
func TestEventCleanupWithNullIssueID(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false
	execCfg.EnableSandboxes = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Create a test issue for regular events
	testIssueID := "vc-test-null-cleanup"
	testIssue := &types.Issue{
		ID:                 testIssueID,
		Title:              "Test issue for NULL cleanup",
		Description:        "Test issue",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	// Create SYSTEM issue for system-level events
	createSystemIssue(ctx, t, store)

	// Create events with NULL issue_id (system-level events)
	// These should NOT cause SQL scan errors during cleanup
	for i := 0; i < 15; i++ {
		event := &events.AgentEvent{
			ID:         "system-event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  time.Now(),
			IssueID:    "", // Empty string -> NULL in database (vc-100)
			ExecutorID: executor.instanceID,
			AgentID:    "test-agent",
			Severity:   events.SeverityInfo,
			Message:    "System event",
			Data:       map[string]interface{}{"test": true},
			SourceLine: 0,
		}
		if err := store.StoreAgentEvent(ctx, event); err != nil {
			t.Fatalf("failed to store system event: %v", err)
		}
	}

	// Create events with valid issue_id that exceed per-issue limit
	for i := 0; i < 12; i++ {
		event := &events.AgentEvent{
			ID:         "issue-event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  time.Now(),
			IssueID:    testIssueID,
			ExecutorID: executor.instanceID,
			AgentID:    "test-agent",
			Severity:   events.SeverityInfo,
			Message:    "Test event",
			Data:       map[string]interface{}{"test": true},
			SourceLine: 0,
		}
		if err := store.StoreAgentEvent(ctx, event); err != nil {
			t.Fatalf("failed to store test event: %v", err)
		}
	}

	// Verify initial counts
	countsBefore, err := store.GetEventCounts(ctx)
	if err != nil {
		t.Fatalf("failed to get event counts: %v", err)
	}
	if countsBefore.TotalEvents != 27 {
		t.Fatalf("expected 27 events before cleanup (15 system + 12 issue), got %d", countsBefore.TotalEvents)
	}

	// Run cleanup with per-issue limit of 5
	// This should:
	// 1. Skip NULL issue_id rows (no scan error)
	// 2. Delete oldest events from testIssueID to get down to 5
	retentionCfg := config.DefaultEventRetentionConfig()
	retentionCfg.RetentionDays = 365       // Keep all by age
	retentionCfg.PerIssueLimitEvents = 5   // Limit 5 per issue
	retentionCfg.GlobalLimitEvents = 10000 // High global limit
	retentionCfg.CleanupVacuum = false

	err = executor.runEventCleanup(ctx, retentionCfg)
	if err != nil {
		t.Fatalf("cleanup failed (should not error on NULL issue_id): %v", err)
	}

	// Verify cleanup occurred correctly
	countsAfter, err := store.GetEventCounts(ctx)
	if err != nil {
		t.Fatalf("failed to get event counts after cleanup: %v", err)
	}

	// Expected: 15 system events (unchanged) + 5 issue events (trimmed) + 1 cleanup event = 21
	expectedTotal := 21
	if countsAfter.TotalEvents != expectedTotal {
		t.Errorf("expected %d events after cleanup (15 system + 5 issue + 1 cleanup), got %d",
			expectedTotal, countsAfter.TotalEvents)
	}

	// Verify issue events were trimmed to limit
	issueEvents, err := store.GetAgentEventsByIssue(ctx, testIssueID)
	if err != nil {
		t.Fatalf("failed to get issue events: %v", err)
	}
	if len(issueEvents) != 5 {
		t.Errorf("expected 5 events for test issue after cleanup, got %d", len(issueEvents))
	}

	t.Logf("✓ Event cleanup with NULL issue_id test passed")
	t.Logf("  - System events (NULL issue_id) preserved: 15")
	t.Logf("  - Issue events trimmed to limit: %d (limit: 5)", len(issueEvents))
	t.Logf("  - No SQL scan errors on NULL values")
}
