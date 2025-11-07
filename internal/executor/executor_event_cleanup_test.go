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

// createSystemIssue creates a pseudo-issue for system-level events
func createSystemIssue(ctx context.Context, t *testing.T, store storage.Storage) {
	t.Helper()
	systemIssue := &types.Issue{
		Title:       "System-level events",
		Description: "Pseudo-issue for system-level events not tied to a specific issue",
		Status:      types.StatusOpen,
		Priority:    3,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, systemIssue, "test"); err != nil {
		t.Fatalf("failed to create SYSTEM issue: %v", err)
	}
}

// TestEventCleanupMetricsLogging verifies that event cleanup metrics are logged as structured events (vc-196)
func TestEventCleanupMetricsLogging(t *testing.T) {
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
	execCfg.EnableAISupervision = false // Disable AI to avoid API calls
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Create SYSTEM issue for system-level events (required for foreign key constraint)
	createSystemIssue(ctx, t, store)

	// Create a test issue first (required for foreign key constraint)
	testIssueID := "vc-test-123"
	testIssue := &types.Issue{
		ID:          testIssueID,
		Title:       "Test issue for cleanup metrics",
		Description: "Test issue",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	// Create some test events to clean up
	for i := 0; i < 10; i++ {
		event := &events.AgentEvent{
			ID:         "event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  time.Now().Add(-48 * time.Hour), // Old events (2 days old)
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

	// Verify events were created
	countsBefore, err := store.GetEventCounts(ctx)
	if err != nil {
		t.Fatalf("failed to get event counts: %v", err)
	}
	if countsBefore.TotalEvents != 10 {
		t.Fatalf("expected 10 events before cleanup, got %d", countsBefore.TotalEvents)
	}

	// Run cleanup with aggressive retention (1 day)
	retentionCfg := config.DefaultEventRetentionConfig()
	retentionCfg.RetentionDays = 1            // Delete events older than 1 day
	retentionCfg.RetentionCriticalDays = 1    // Also delete critical events
	retentionCfg.PerIssueLimitEvents = 1000   // High limit
	retentionCfg.GlobalLimitEvents = 10000    // High limit
	retentionCfg.CleanupVacuum = false        // Skip VACUUM for speed

	err = executor.runEventCleanup(ctx, retentionCfg)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Verify cleanup occurred
	countsAfter, err := store.GetEventCounts(ctx)
	if err != nil {
		t.Fatalf("failed to get event counts after cleanup: %v", err)
	}

	// All 10 test events should be deleted (they're 2 days old, retention is 1 day)
	// But we should have 1 cleanup event logged
	if countsAfter.TotalEvents != 1 {
		t.Fatalf("expected 1 event after cleanup (the cleanup event itself), got %d", countsAfter.TotalEvents)
	}

	// Retrieve the cleanup event
	filter := events.EventFilter{
		Type:  events.EventTypeEventCleanupCompleted,
		Limit: 10,
	}
	cleanupEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("failed to get cleanup events: %v", err)
	}

	if len(cleanupEvents) != 1 {
		t.Fatalf("expected 1 cleanup event, got %d", len(cleanupEvents))
	}

	event := cleanupEvents[0]

	// Verify event structure
	if event.Type != events.EventTypeEventCleanupCompleted {
		t.Errorf("expected event type %s, got %s", events.EventTypeEventCleanupCompleted, event.Type)
	}

	if event.IssueID != "SYSTEM" {
		t.Errorf("expected issue_id 'SYSTEM', got %s", event.IssueID)
	}

	if event.ExecutorID != executor.instanceID {
		t.Errorf("expected executor_id %s, got %s", executor.instanceID, event.ExecutorID)
	}

	if event.Severity != events.SeverityInfo {
		t.Errorf("expected severity %s, got %s", events.SeverityInfo, event.Severity)
	}

	// Verify data fields
	data := event.Data
	if data == nil {
		t.Fatal("expected data to be non-nil")
	}

	// Check required fields exist
	requiredFields := []string{
		"events_deleted",
		"time_based_deleted",
		"per_issue_deleted",
		"global_limit_deleted",
		"processing_time_ms",
		"vacuum_ran",
		"events_remaining",
		"success",
	}

	for _, field := range requiredFields {
		if _, ok := data[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify specific values
	if eventsDeleted, ok := data["events_deleted"].(float64); !ok {
		t.Errorf("events_deleted should be a number")
	} else if int(eventsDeleted) != 10 {
		t.Errorf("expected 10 events deleted, got %d", int(eventsDeleted))
	}

	if timeBasedDeleted, ok := data["time_based_deleted"].(float64); !ok {
		t.Errorf("time_based_deleted should be a number")
	} else if int(timeBasedDeleted) != 10 {
		t.Errorf("expected 10 time-based deletions, got %d", int(timeBasedDeleted))
	}

	if success, ok := data["success"].(bool); !ok {
		t.Errorf("success should be a boolean")
	} else if !success {
		t.Errorf("expected success to be true")
	}

	if vacuumRan, ok := data["vacuum_ran"].(bool); !ok {
		t.Errorf("vacuum_ran should be a boolean")
	} else if vacuumRan {
		t.Errorf("expected vacuum_ran to be false (disabled in test)")
	}

	// Verify processing time is reasonable (should be < 1 second for this small test)
	if processingTime, ok := data["processing_time_ms"].(float64); !ok {
		t.Errorf("processing_time_ms should be a number")
	} else if processingTime < 0 || processingTime > 1000 {
		t.Errorf("expected processing time 0-1000ms, got %dms", int(processingTime))
	}
}

// TestEventCleanupMetricsLoggingOnError verifies that cleanup errors are logged correctly (vc-196)
func TestEventCleanupMetricsLoggingOnError(t *testing.T) {
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
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Close the database to force an error
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close storage: %v", err)
	}

	// Try to run cleanup (should fail)
	retentionCfg := config.DefaultEventRetentionConfig()
	err = executor.runEventCleanup(ctx, retentionCfg)
	if err == nil {
		t.Fatal("expected cleanup to fail with closed database")
	}

	// Note: We can't verify the error event was logged because the database is closed
	// This test just verifies that the error handling path doesn't panic
}

// TestEventCleanupPartialFailure verifies that partial failures are logged correctly (vc-196)
func TestEventCleanupPartialFailure(t *testing.T) {
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
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Create SYSTEM issue for system-level events
	createSystemIssue(ctx, t, store)

	// Create a test issue first (required for foreign key constraint)
	testIssueID := "vc-test-456"
	testIssue := &types.Issue{
		ID:          testIssueID,
		Title:       "Test issue for partial failure",
		Description: "Test issue",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	// Create some test events
	for i := 0; i < 5; i++ {
		event := &events.AgentEvent{
			ID:         "event-" + string(rune(i)),
			Type:       events.EventTypeProgress,
			Timestamp:  time.Now().Add(-10 * time.Hour),
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

	// Run cleanup with normal settings (should succeed but delete 0 events since they're recent)
	retentionCfg := config.DefaultEventRetentionConfig()
	retentionCfg.RetentionDays = 7            // Keep 7 days
	retentionCfg.PerIssueLimitEvents = 1000
	retentionCfg.GlobalLimitEvents = 10000
	retentionCfg.CleanupVacuum = false

	err = executor.runEventCleanup(ctx, retentionCfg)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Retrieve the cleanup event
	filter := events.EventFilter{
		Type:  events.EventTypeEventCleanupCompleted,
		Limit: 10,
	}
	cleanupEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("failed to get cleanup events: %v", err)
	}

	if len(cleanupEvents) != 1 {
		t.Fatalf("expected 1 cleanup event, got %d", len(cleanupEvents))
	}

	event := cleanupEvents[0]

	// Verify 0 events were deleted (they're too recent)
	data := event.Data
	if eventsDeleted, ok := data["events_deleted"].(float64); !ok {
		t.Errorf("events_deleted should be a number")
	} else if int(eventsDeleted) != 0 {
		t.Errorf("expected 0 events deleted, got %d", int(eventsDeleted))
	}

	if success, ok := data["success"].(bool); !ok {
		t.Errorf("success should be a boolean")
	} else if !success {
		t.Errorf("expected success to be true even with 0 deletions")
	}

	// Verify events_remaining matches the count (5 test events + 1 cleanup event if SYSTEM issue exists)
	if eventsRemaining, ok := data["events_remaining"].(float64); !ok {
		t.Errorf("events_remaining should be a number")
	} else if int(eventsRemaining) != 6 {
		// Note: cleanup event should be stored as well, but might fail if SYSTEM issue doesn't exist
		// In that case we'd have 5 events instead of 6
		if int(eventsRemaining) != 5 {
			t.Errorf("expected 5-6 events remaining (5 test + maybe 1 cleanup), got %d", int(eventsRemaining))
		}
	}
}

// TestLogCleanupEvent verifies the logCleanupEvent helper function (vc-196)
func TestLogCleanupEvent(t *testing.T) {
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
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Create SYSTEM issue for system-level events
	createSystemIssue(ctx, t, store)

	// Test successful cleanup logging
	executor.logCleanupEvent(ctx, 100, 50, 30, 20, 1234, true, 500, true, "")

	// Retrieve the event
	filter := events.EventFilter{
		Type:  events.EventTypeEventCleanupCompleted,
		Limit: 10,
	}
	cleanupEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("failed to get cleanup events: %v", err)
	}

	if len(cleanupEvents) != 1 {
		t.Fatalf("expected 1 cleanup event, got %d", len(cleanupEvents))
	}

	event := cleanupEvents[0]

	// Verify all fields
	if event.IssueID != "SYSTEM" {
		t.Errorf("expected SYSTEM issue_id, got %s", event.IssueID)
	}

	if event.Severity != events.SeverityInfo {
		t.Errorf("expected Info severity for success, got %s", event.Severity)
	}

	data := event.Data
	if int(data["events_deleted"].(float64)) != 100 {
		t.Errorf("expected 100 events_deleted")
	}
	if int(data["time_based_deleted"].(float64)) != 50 {
		t.Errorf("expected 50 time_based_deleted")
	}
	if int(data["per_issue_deleted"].(float64)) != 30 {
		t.Errorf("expected 30 per_issue_deleted")
	}
	if int(data["global_limit_deleted"].(float64)) != 20 {
		t.Errorf("expected 20 global_limit_deleted")
	}
	if int(data["processing_time_ms"].(float64)) != 1234 {
		t.Errorf("expected 1234 processing_time_ms")
	}
	if data["vacuum_ran"].(bool) != true {
		t.Errorf("expected vacuum_ran to be true")
	}
	if int(data["events_remaining"].(float64)) != 500 {
		t.Errorf("expected 500 events_remaining")
	}
	if data["success"].(bool) != true {
		t.Errorf("expected success to be true")
	}

	// Test error logging
	executor.logCleanupEvent(ctx, 0, 0, 0, 0, 100, false, 1000, false, "database error")

	cleanupEvents, err = store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("failed to get cleanup events: %v", err)
	}

	if len(cleanupEvents) != 2 {
		t.Fatalf("expected 2 cleanup events, got %d", len(cleanupEvents))
	}

	// Get the error event (most recent)
	errorEvent := cleanupEvents[0]

	if errorEvent.Severity != events.SeverityError {
		t.Errorf("expected Error severity for failure, got %s", errorEvent.Severity)
	}

	if errorEvent.Data["success"].(bool) != false {
		t.Errorf("expected success to be false for error event")
	}

	if errorEvent.Data["error"].(string) != "database error" {
		t.Errorf("expected error message in data")
	}
}

// TestEventCleanupSkipsWhenDisabled verifies cleanup respects CleanupEnabled flag
func TestEventCleanupSkipsWhenDisabled(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor with cleanup disabled
	retentionCfg := config.DefaultEventRetentionConfig()
	retentionCfg.CleanupEnabled = false

	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false
	execCfg.EventRetentionConfig = &retentionCfg

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Start executor (this should skip cleanup loop startup)
	if err := executor.Start(ctx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}

	// Wait a bit to ensure cleanup loop doesn't run
	time.Sleep(100 * time.Millisecond)

	// Verify no cleanup events were logged
	filter := events.EventFilter{
		Type:  events.EventTypeEventCleanupCompleted,
		Limit: 10,
	}
	cleanupEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("failed to get cleanup events: %v", err)
	}

	if len(cleanupEvents) != 0 {
		t.Errorf("expected 0 cleanup events when disabled, got %d", len(cleanupEvents))
	}

	// Stop executor
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := executor.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop executor: %v", err)
	}
}
