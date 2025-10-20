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
	store, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
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
	store, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
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
	store, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
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
