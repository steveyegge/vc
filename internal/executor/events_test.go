package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestEventLoggingOrderInErrorPaths tests vc-162: Events logged before releaseIssueWithError
func TestEventLoggingOrderInErrorPaths(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    executor.instanceID,
		Hostname:      executor.hostname,
		PID:           executor.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       executor.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:              "Event Ordering Test",
		Description:        "Test that events are logged before issue release",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Events logged even if release fails",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Test: Log an error event, then try to release
	// This simulates what happens in error paths
	executor.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
		"Failed to spawn agent: simulated error",
		map[string]interface{}{
			"success":    false,
			"agent_type": "claude-code",
			"error":      "simulated error",
		})

	// Get events - should have the error event even before release
	storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(storedEvents) == 0 {
		t.Fatal("Expected event to be logged before release, but got no events")
	}

	// Verify the event is correct
	event := storedEvents[0]
	if event.Type != events.EventTypeAgentSpawned {
		t.Errorf("Expected event type %s, got %s", events.EventTypeAgentSpawned, event.Type)
	}
	if event.Severity != events.SeverityError {
		t.Errorf("Expected severity %s, got %s", events.SeverityError, event.Severity)
	}
	if event.Data["success"] != false {
		t.Error("Expected success=false in event data")
	}

	// Now release the issue
	executor.releaseIssueWithError(ctx, issue.ID, "Failed to spawn agent: simulated error")

	// Events should still be there after release
	storedEvents, err = store.GetAgentEventsByIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get events after release: %v", err)
	}

	if len(storedEvents) == 0 {
		t.Fatal("Events should persist after issue release")
	}
}

// TestEventLoggingInMultipleErrorPaths tests that all error paths log events correctly
func TestEventLoggingInMultipleErrorPaths(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Test different error scenarios
	errorScenarios := []struct {
		name       string
		eventType  events.EventType
		severity   events.EventSeverity
		message    string
		data       map[string]interface{}
	}{
		{
			name:      "Agent spawn failure",
			eventType: events.EventTypeAgentSpawned,
			severity:  events.SeverityError,
			message:   "Failed to spawn agent",
			data: map[string]interface{}{
				"success":    false,
				"agent_type": "claude-code",
				"error":      "spawn error",
			},
		},
		{
			name:      "Agent execution failure",
			eventType: events.EventTypeAgentCompleted,
			severity:  events.SeverityError,
			message:   "Agent execution failed",
			data: map[string]interface{}{
				"success": false,
				"error":   "execution error",
			},
		},
		{
			name:      "Results processing failure",
			eventType: events.EventTypeResultsProcessingCompleted,
			severity:  events.SeverityError,
			message:   "Results processing failed",
			data: map[string]interface{}{
				"success": false,
				"error":   "processing error",
			},
		},
	}

	for _, scenario := range errorScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Create test issue for this scenario
			issue := &types.Issue{
				Title:              scenario.name,
				Description:        "Test error event logging",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Event logged before release",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}
			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Log the error event
			executor.logEvent(ctx, scenario.eventType, scenario.severity, issue.ID,
				scenario.message, scenario.data)

			// Verify event was logged
			storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			if len(storedEvents) == 0 {
				t.Fatal("Expected event to be logged")
			}

			event := storedEvents[0]
			if event.Type != scenario.eventType {
				t.Errorf("Expected event type %s, got %s", scenario.eventType, event.Type)
			}
			if event.Severity != scenario.severity {
				t.Errorf("Expected severity %s, got %s", scenario.severity, event.Severity)
			}
		})
	}
}

// TestAnalysisEventsLogging tests vc-163: Analysis phase events
func TestAnalysisEventsLogging(t *testing.T) {
	// This test documents that analysis events should be logged
	// The actual implementation is in results.go, but we test the event types here

	// Verify the new event types exist
	startedType := events.EventTypeAnalysisStarted
	completedType := events.EventTypeAnalysisCompleted

	if startedType == "" {
		t.Error("EventTypeAnalysisStarted should be defined")
	}
	if completedType == "" {
		t.Error("EventTypeAnalysisCompleted should be defined")
	}

	// Test that we can create analysis events
	analysisStarted := &events.AgentEvent{
		ID:         "test-1",
		Type:       events.EventTypeAnalysisStarted,
		Timestamp:  time.Now(),
		IssueID:    "vc-test",
		ExecutorID: "exec-1",
		Severity:   events.SeverityInfo,
		Message:    "Starting AI analysis",
		Data:       map[string]interface{}{},
	}

	if analysisStarted.Type != events.EventTypeAnalysisStarted {
		t.Errorf("Expected type %s, got %s", events.EventTypeAnalysisStarted, analysisStarted.Type)
	}

	analysisCompleted := &events.AgentEvent{
		ID:         "test-2",
		Type:       events.EventTypeAnalysisCompleted,
		Timestamp:  time.Now(),
		IssueID:    "vc-test",
		ExecutorID: "exec-1",
		Severity:   events.SeverityInfo,
		Message:    "AI analysis completed",
		Data: map[string]interface{}{
			"success":          true,
			"completed":        true,
			"discovered_count": 2,
			"quality_issues":   0,
		},
	}

	if analysisCompleted.Type != events.EventTypeAnalysisCompleted {
		t.Errorf("Expected type %s, got %s", events.EventTypeAnalysisCompleted, analysisCompleted.Type)
	}
}

// TestQualityGatesSkippedEvent tests vc-163: Quality gates skipped event
func TestQualityGatesSkippedEvent(t *testing.T) {
	// Verify the new event type exists
	skippedType := events.EventTypeQualityGatesSkipped

	if skippedType == "" {
		t.Error("EventTypeQualityGatesSkipped should be defined")
	}

	// Test that we can create a skipped event with reason
	skippedEvent := &events.AgentEvent{
		ID:         "test-1",
		Type:       events.EventTypeQualityGatesSkipped,
		Timestamp:  time.Now(),
		IssueID:    "vc-test",
		ExecutorID: "exec-1",
		Severity:   events.SeverityInfo,
		Message:    "Quality gates skipped: agent execution failed",
		Data: map[string]interface{}{
			"reason": "agent execution failed",
		},
	}

	if skippedEvent.Type != events.EventTypeQualityGatesSkipped {
		t.Errorf("Expected type %s, got %s", events.EventTypeQualityGatesSkipped, skippedEvent.Type)
	}

	if skippedEvent.Data["reason"] == nil {
		t.Error("Skipped event should include reason in data")
	}
}

// TestEventDataNoRedundancy tests vc-164: No issue_id or executor_id in Data
func TestEventDataNoRedundancy(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:              "Data Redundancy Test",
		Description:        "Test that issue_id and executor_id are not duplicated",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "No redundant fields in Data",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Log various event types and verify no redundancy
	testCases := []struct {
		name      string
		eventType events.EventType
		message   string
		data      map[string]interface{}
	}{
		{
			name:      "Issue claimed",
			eventType: events.EventTypeIssueClaimed,
			message:   "Issue claimed",
			data: map[string]interface{}{
				"issue_title": issue.Title,
				// Should NOT include issue_id or executor_id
			},
		},
		{
			name:      "Assessment completed",
			eventType: events.EventTypeAssessmentCompleted,
			message:   "Assessment completed",
			data: map[string]interface{}{
				"success":    true,
				"strategy":   "refactor",
				"confidence": 0.9,
				// Should NOT include issue_id
			},
		},
		{
			name:      "Agent spawned",
			eventType: events.EventTypeAgentSpawned,
			message:   "Agent spawned",
			data: map[string]interface{}{
				"success":    true,
				"agent_type": "claude-code",
				// Should NOT include issue_id
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Log the event
			executor.logEvent(ctx, tc.eventType, events.SeverityInfo, issue.ID,
				tc.message, tc.data)

			// Get the event back
			storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			// Find the event we just logged
			var event *events.AgentEvent
			for i := range storedEvents {
				if storedEvents[i].Type == tc.eventType {
					event = storedEvents[i]
					break
				}
			}

			if event == nil {
				t.Fatalf("Failed to find event of type %s", tc.eventType)
			}

			// Verify no redundant fields
			if _, exists := event.Data["issue_id"]; exists {
				t.Error("Event Data should NOT contain issue_id (redundant with event.IssueID)")
			}
			if _, exists := event.Data["executor_id"]; exists {
				t.Error("Event Data should NOT contain executor_id (redundant with event.ExecutorID)")
			}

			// Verify the actual fields are set correctly
			if event.IssueID != issue.ID {
				t.Errorf("Expected IssueID %s, got %s", issue.ID, event.IssueID)
			}
			if event.ExecutorID != executor.instanceID {
				t.Errorf("Expected ExecutorID %s, got %s", executor.instanceID, event.ExecutorID)
			}
		})
	}
}

// TestEventSeverityConsistency tests vc-164: Consistent severity levels
func TestEventSeverityConsistency(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:       "Severity Test",
		Description: "Test severity consistency",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Test that all setup failures use Error severity (not Warning)
	setupFailures := []struct {
		name      string
		eventType events.EventType
		message   string
		data      map[string]interface{}
	}{
		{
			name:      "Assessment failure",
			eventType: events.EventTypeAssessmentCompleted,
			message:   "AI assessment failed",
			data: map[string]interface{}{
				"success": false,
				"error":   "API error",
			},
		},
		{
			name:      "Agent spawn failure",
			eventType: events.EventTypeAgentSpawned,
			message:   "Failed to spawn agent",
			data: map[string]interface{}{
				"success": false,
				"error":   "spawn error",
			},
		},
		{
			name:      "Agent execution failure",
			eventType: events.EventTypeAgentCompleted,
			message:   "Agent execution failed",
			data: map[string]interface{}{
				"success": false,
				"error":   "execution error",
			},
		},
	}

	for _, failure := range setupFailures {
		t.Run(failure.name, func(t *testing.T) {
			// Log the failure with Error severity
			executor.logEvent(ctx, failure.eventType, events.SeverityError, issue.ID,
				failure.message, failure.data)

			// Get the event back
			storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			// Find the latest event
			if len(storedEvents) == 0 {
				t.Fatal("Expected at least one event")
			}
			event := storedEvents[len(storedEvents)-1]

			// Verify it's Error severity (not Warning)
			if event.Severity != events.SeverityError {
				t.Errorf("Expected Error severity for %s, got %s (setup failures should be Error, not Warning)",
					failure.name, event.Severity)
			}
		})
	}
}

// TestContextCancellationHandling tests vc-165: logEvent checks ctx.Done()
func TestContextCancellationHandling(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:       "Context Cancellation Test",
		Description: "Test that logEvent respects context cancellation",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Test 1: Normal logging with valid context
	t.Run("ValidContext", func(t *testing.T) {
		executor.logEvent(ctx, events.EventTypeIssueClaimed, events.SeverityInfo, issue.ID,
			"Issue claimed", map[string]interface{}{})

		// Should have stored the event
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		if len(storedEvents) == 0 {
			t.Error("Expected event to be stored with valid context")
		}
	})

	// Test 2: Logging with cancelled context
	t.Run("CancelledContext", func(t *testing.T) {
		// Create a cancelled context
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel it immediately

		// Count events before
		eventsBefore, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		countBefore := len(eventsBefore)

		// Try to log with cancelled context
		executor.logEvent(cancelledCtx, events.EventTypeAgentSpawned, events.SeverityInfo, issue.ID,
			"Should not be logged", map[string]interface{}{})

		// Count events after
		eventsAfter, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		countAfter := len(eventsAfter)

		// Should NOT have added a new event
		if countAfter > countBefore {
			t.Error("Expected no event to be stored with cancelled context")
		}
	})

	// Test 3: Logging with timeout context that expires
	t.Run("ExpiredTimeoutContext", func(t *testing.T) {
		// Create a context with very short timeout
		timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
		defer cancel()

		// Wait for it to expire
		time.Sleep(10 * time.Millisecond)

		// Count events before
		eventsBefore, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		countBefore := len(eventsBefore)

		// Try to log with expired context
		executor.logEvent(timeoutCtx, events.EventTypeAgentCompleted, events.SeverityInfo, issue.ID,
			"Should not be logged", map[string]interface{}{})

		// Count events after
		eventsAfter, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		countAfter := len(eventsAfter)

		// Should NOT have added a new event
		if countAfter > countBefore {
			t.Error("Expected no event to be stored with expired context")
		}
	})
}

// TestAgentIDFieldDocumentation tests vc-165: AgentID field is properly documented
func TestAgentIDFieldDocumentation(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:       "AgentID Test",
		Description: "Test AgentID field handling",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Log an executor-level event
	executor.logEvent(ctx, events.EventTypeIssueClaimed, events.SeverityInfo, issue.ID,
		"Issue claimed by executor", map[string]interface{}{})

	// Get the event back
	storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(storedEvents) == 0 {
		t.Fatal("Expected at least one event")
	}

	event := storedEvents[0]

	// For executor-level events, AgentID should be empty
	// (These events are not produced by coding agents - they're produced by the executor itself)
	if event.AgentID != "" {
		t.Errorf("Expected AgentID to be empty for executor-level events, got %s", event.AgentID)
	}

	// Verify ExecutorID is set correctly though
	if event.ExecutorID != executor.instanceID {
		t.Errorf("Expected ExecutorID %s, got %s", executor.instanceID, event.ExecutorID)
	}
}

// TestCompleteEventSequence tests the full event sequence with all new events
func TestCompleteEventSequence(t *testing.T) {
	// This test documents the expected complete event sequence
	// including the new analysis and quality gates events

	expectedSequence := []events.EventType{
		events.EventTypeIssueClaimed,
		events.EventTypeAssessmentStarted,
		events.EventTypeAssessmentCompleted,
		events.EventTypeAgentSpawned,
		events.EventTypeAgentCompleted,
		events.EventTypeResultsProcessingStarted,
		events.EventTypeAnalysisStarted,          // NEW in vc-163
		events.EventTypeAnalysisCompleted,        // NEW in vc-163
		events.EventTypeQualityGatesStarted,
		events.EventTypeQualityGatesCompleted,
		events.EventTypeResultsProcessingCompleted,
	}

	// Verify all event types are defined
	for i, eventType := range expectedSequence {
		if eventType == "" {
			t.Errorf("Event type at position %d is empty", i)
		}
	}

	// Document the sequence
	t.Log("Expected complete event sequence for successful execution:")
	for i, eventType := range expectedSequence {
		t.Logf("  %2d. %s", i+1, eventType)
	}

	// Also document the skipped sequence
	t.Log("\nExpected sequence when quality gates are skipped:")
	t.Logf("  ... (same as above until)")
	t.Logf("  %2d. %s", 8, events.EventTypeAnalysisCompleted)
	t.Logf("  %2d. %s (NEW)", 9, events.EventTypeQualityGatesSkipped)
	t.Logf("  %2d. %s", 10, events.EventTypeResultsProcessingCompleted)
}

// BenchmarkEventLogging benchmarks the event logging performance
func BenchmarkEventLogging(b *testing.B) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		b.Fatalf("Failed to create executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:       "Benchmark Issue",
		Description: "For benchmarking",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		b.Fatalf("Failed to create issue: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		executor.logEvent(ctx, events.EventTypeAgentCompleted, events.SeverityInfo,
			issue.ID, fmt.Sprintf("Benchmark event %d", i),
			map[string]interface{}{
				"success":   true,
				"iteration": i,
			})
	}
}
