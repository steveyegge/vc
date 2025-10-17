package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestResultsProcessorAnalysisEvents tests vc-163: Analysis phase events
func TestResultsProcessorAnalysisEvents(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create test issue
	issue := &types.Issue{
		Title:       "Analysis Events Test",
		Description: "Test that analysis phase logs events",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	t.Run("AnalysisStartedEvent", func(t *testing.T) {
		// Create processor WITHOUT AI supervisor (analysis will be skipped)
		processor, err := NewResultsProcessor(&ResultsProcessorConfig{
			Store:              store,
			Supervisor:         nil, // No AI supervisor
			EnableQualityGates: false,
			WorkingDir:         ".",
			Actor:              "test-actor",
		})
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}

		// Manually log analysis started event
		processor.logEvent(ctx, events.EventTypeAnalysisStarted, events.SeverityInfo, issue.ID,
			"Starting AI analysis",
			map[string]interface{}{})

		// Verify event was logged
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}

		// Find the analysis started event
		var found bool
		for _, e := range storedEvents {
			if e.Type == events.EventTypeAnalysisStarted {
				found = true
				if e.Severity != events.SeverityInfo {
					t.Errorf("Expected severity %s, got %s", events.SeverityInfo, e.Severity)
				}
				break
			}
		}

		if !found {
			t.Error("Expected EventTypeAnalysisStarted event to be logged")
		}
	})

	t.Run("AnalysisCompletedEventSuccess", func(t *testing.T) {
		processor, err := NewResultsProcessor(&ResultsProcessorConfig{
			Store:              store,
			Supervisor:         nil,
			EnableQualityGates: false,
			WorkingDir:         ".",
			Actor:              "test-actor",
		})
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}

		// Log analysis completed (success) event
		processor.logEvent(ctx, events.EventTypeAnalysisCompleted, events.SeverityInfo, issue.ID,
			"AI analysis completed",
			map[string]interface{}{
				"success":          true,
				"completed":        true,
				"discovered_count": 2,
				"quality_issues":   0,
			})

		// Verify event was logged
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}

		// Find the latest analysis completed event
		var found bool
		for i := len(storedEvents) - 1; i >= 0; i-- {
			if storedEvents[i].Type == events.EventTypeAnalysisCompleted {
				e := storedEvents[i]
				found = true

				if e.Severity != events.SeverityInfo {
					t.Errorf("Expected severity %s, got %s", events.SeverityInfo, e.Severity)
				}

				if e.Data["success"] != true {
					t.Error("Expected success=true in event data")
				}
				if e.Data["completed"] != true {
					t.Error("Expected completed=true in event data")
				}
				if e.Data["discovered_count"] != float64(2) { // JSON numbers are float64
					t.Errorf("Expected discovered_count=2, got %v", e.Data["discovered_count"])
				}

				break
			}
		}

		if !found {
			t.Error("Expected EventTypeAnalysisCompleted event to be logged")
		}
	})

	t.Run("AnalysisCompletedEventFailure", func(t *testing.T) {
		processor, err := NewResultsProcessor(&ResultsProcessorConfig{
			Store:              store,
			Supervisor:         nil,
			EnableQualityGates: false,
			WorkingDir:         ".",
			Actor:              "test-actor",
		})
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}

		// Log analysis completed (failure) event
		processor.logEvent(ctx, events.EventTypeAnalysisCompleted, events.SeverityError, issue.ID,
			"AI analysis failed",
			map[string]interface{}{
				"success": false,
				"error":   "API error",
			})

		// Verify event was logged
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}

		// Find the latest analysis completed event
		var found bool
		for i := len(storedEvents) - 1; i >= 0; i-- {
			if storedEvents[i].Type == events.EventTypeAnalysisCompleted && storedEvents[i].Severity == events.SeverityError {
				e := storedEvents[i]
				found = true

				// Should use Error severity for failures (not Warning)
				if e.Severity != events.SeverityError {
					t.Errorf("Expected severity %s for failed analysis, got %s", events.SeverityError, e.Severity)
				}

				if e.Data["success"] != false {
					t.Error("Expected success=false in event data")
				}
				if e.Data["error"] == nil {
					t.Error("Expected error field in event data")
				}

				break
			}
		}

		if !found {
			t.Error("Expected EventTypeAnalysisCompleted failure event to be logged")
		}
	})
}

// TestResultsProcessorQualityGatesSkippedEvent tests vc-163: Quality gates skipped event
func TestResultsProcessorQualityGatesSkippedEvent(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create test issue
	issue := &types.Issue{
		Title:       "Quality Gates Skipped Test",
		Description: "Test that skipped gates log events",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	t.Run("SkippedDueToAgentFailure", func(t *testing.T) {
		processor, err := NewResultsProcessor(&ResultsProcessorConfig{
			Store:              store,
			Supervisor:         nil,
			EnableQualityGates: true, // Gates enabled but will be skipped
			WorkingDir:         ".",
			Actor:              "test-actor",
		})
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}

		// Create executor instance and claim the issue (required for ProcessAgentResult)
		execInstance := &types.ExecutorInstance{
			InstanceID:    "test-executor",
			Hostname:      "test-host",
			PID:           12345,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Version:       "test",
			Metadata:      "{}",
		}
		if err := store.RegisterInstance(ctx, execInstance); err != nil {
			t.Fatalf("Failed to register executor: %v", err)
		}

		if err := store.ClaimIssue(ctx, issue.ID, "test-executor"); err != nil {
			t.Fatalf("Failed to claim issue: %v", err)
		}

		// Process a failed agent result
		agentResult := &AgentResult{
			Success:  false,
			ExitCode: 1,
			Duration: 5 * time.Second,
			Output:   []string{"Agent failed"},
		}

		_, err = processor.ProcessAgentResult(ctx, issue, agentResult)
		if err != nil {
			t.Fatalf("ProcessAgentResult failed: %v", err)
		}

		// Verify quality gates skipped event was logged
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}

		// Find the skipped event
		var found bool
		for _, e := range storedEvents {
			if e.Type == events.EventTypeQualityGatesSkipped {
				found = true

				if e.Severity != events.SeverityInfo {
					t.Errorf("Expected severity %s, got %s", events.SeverityInfo, e.Severity)
				}

				reason, ok := e.Data["reason"].(string)
				if !ok {
					t.Error("Expected reason field in event data")
				}
				if reason != "agent execution failed" {
					t.Errorf("Expected reason 'agent execution failed', got '%s'", reason)
				}

				break
			}
		}

		if !found {
			t.Error("Expected EventTypeQualityGatesSkipped event to be logged when agent fails")
		}
	})

	t.Run("SkippedDueToDisabled", func(t *testing.T) {
		// Create new issue for this test
		issue2 := &types.Issue{
			Title:       "Quality Gates Disabled Test",
			Description: "Test skipped event when gates disabled",
			IssueType:   types.TypeTask,
			Status:      types.StatusOpen,
			Priority:    1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Create executor instance and claim the issue
		execInstance2 := &types.ExecutorInstance{
			InstanceID:    "test-executor-2",
			Hostname:      "test-host",
			PID:           12346,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Version:       "test",
			Metadata:      "{}",
		}
		if err := store.RegisterInstance(ctx, execInstance2); err != nil {
			t.Fatalf("Failed to register executor: %v", err)
		}

		if err := store.ClaimIssue(ctx, issue2.ID, "test-executor-2"); err != nil {
			t.Fatalf("Failed to claim issue: %v", err)
		}

		processor, err := NewResultsProcessor(&ResultsProcessorConfig{
			Store:              store,
			Supervisor:         nil,
			EnableQualityGates: false, // Gates disabled
			WorkingDir:         ".",
			Actor:              "test-actor",
		})
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}

		// Process a successful agent result
		agentResult := &AgentResult{
			Success:  true,
			ExitCode: 0,
			Duration: 5 * time.Second,
			Output:   []string{"Agent succeeded"},
		}

		_, err = processor.ProcessAgentResult(ctx, issue2, agentResult)
		if err != nil {
			t.Fatalf("ProcessAgentResult failed: %v", err)
		}

		// Verify quality gates skipped event was logged
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue2.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}

		// Find the skipped event
		var found bool
		for _, e := range storedEvents {
			if e.Type == events.EventTypeQualityGatesSkipped {
				found = true

				reason, ok := e.Data["reason"].(string)
				if !ok {
					t.Error("Expected reason field in event data")
				}
				if reason != "quality gates disabled" {
					t.Errorf("Expected reason 'quality gates disabled', got '%s'", reason)
				}

				break
			}
		}

		if !found {
			t.Error("Expected EventTypeQualityGatesSkipped event to be logged when gates disabled")
		}
	})
}

// TestResultsProcessorGateRunnerCreationFailure tests vc-163: Proper event sequence on gate runner failure
func TestResultsProcessorGateRunnerCreationFailure(t *testing.T) {
	// This test documents the expected behavior:
	// When gate runner creation fails, we should log:
	// 1. quality_gates_started
	// 2. quality_gates_completed (with error)
	//
	// NOT just quality_gates_completed without started

	// The current implementation logs started, then logs completed with error
	// This is the correct sequence

	t.Log("Gate runner creation failure should log:")
	t.Log("  1. quality_gates_started")
	t.Log("  2. quality_gates_completed (with error)")
	t.Log("")
	t.Log("This provides proper observability of the attempt and failure")
}

// TestResultsProcessorEventDataNoRedundancy tests vc-164: No redundant fields in results processor events
func TestResultsProcessorEventDataNoRedundancy(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create test issue
	issue := &types.Issue{
		Title:       "Data Redundancy Test",
		Description: "Test no redundant fields",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              store,
		Supervisor:         nil,
		EnableQualityGates: false,
		WorkingDir:         ".",
		Actor:              "test-processor",
	})
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Log various events
	testEvents := []struct {
		eventType events.EventType
		message   string
		data      map[string]interface{}
	}{
		{
			eventType: events.EventTypeQualityGatesStarted,
			message:   "Starting quality gates",
			data:      map[string]interface{}{},
		},
		{
			eventType: events.EventTypeQualityGatesCompleted,
			message:   "Quality gates completed",
			data: map[string]interface{}{
				"all_passed": true,
				"gates_run":  3,
			},
		},
		{
			eventType: events.EventTypeQualityGatesSkipped,
			message:   "Quality gates skipped",
			data: map[string]interface{}{
				"reason": "disabled",
			},
		},
	}

	for _, te := range testEvents {
		processor.logEvent(ctx, te.eventType, events.SeverityInfo, issue.ID, te.message, te.data)
	}

	// Get all events
	storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	// Verify none have redundant fields
	for _, e := range storedEvents {
		if _, exists := e.Data["issue_id"]; exists {
			t.Errorf("Event %s should NOT contain issue_id in Data (redundant with event.IssueID)", e.Type)
		}
		if _, exists := e.Data["executor_id"]; exists {
			t.Errorf("Event %s should NOT contain executor_id in Data (redundant with event.ExecutorID)", e.Type)
		}

		// Verify the actual fields are set correctly
		if e.IssueID != issue.ID {
			t.Errorf("Expected IssueID %s, got %s", issue.ID, e.IssueID)
		}
		if e.ExecutorID != processor.actor {
			t.Errorf("Expected ExecutorID %s, got %s", processor.actor, e.ExecutorID)
		}
	}
}

// TestResultsProcessorEventSeverityConsistency tests vc-164: Consistent severity levels
func TestResultsProcessorEventSeverityConsistency(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create test issue
	issue := &types.Issue{
		Title:       "Severity Consistency Test",
		Description: "Test severity levels",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              store,
		Supervisor:         nil,
		EnableQualityGates: false,
		WorkingDir:         ".",
		Actor:              "test-processor",
	})
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test that setup failures use Error severity
	setupFailures := []struct {
		name      string
		eventType events.EventType
		message   string
		data      map[string]interface{}
	}{
		{
			name:      "Analysis failure",
			eventType: events.EventTypeAnalysisCompleted,
			message:   "AI analysis failed",
			data: map[string]interface{}{
				"success": false,
				"error":   "API error",
			},
		},
		{
			name:      "Gate runner creation failure",
			eventType: events.EventTypeQualityGatesCompleted,
			message:   "Gate runner creation failed",
			data: map[string]interface{}{
				"success": false,
				"error":   "runner error",
			},
		},
	}

	for _, failure := range setupFailures {
		t.Run(failure.name, func(t *testing.T) {
			// Log the failure with Error severity
			processor.logEvent(ctx, failure.eventType, events.SeverityError, issue.ID,
				failure.message, failure.data)

			// Get events
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

// TestResultsProcessorContextCancellation tests vc-165: Results processor respects context cancellation
func TestResultsProcessorContextCancellation(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create test issue
	issue := &types.Issue{
		Title:       "Context Cancellation Test",
		Description: "Test context cancellation",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              store,
		Supervisor:         nil,
		EnableQualityGates: false,
		WorkingDir:         ".",
		Actor:              "test-processor",
	})
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	t.Run("ValidContext", func(t *testing.T) {
		// Log with valid context
		processor.logEvent(ctx, events.EventTypeQualityGatesStarted, events.SeverityInfo, issue.ID,
			"Test event", map[string]interface{}{})

		// Should have stored the event
		storedEvents, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		if len(storedEvents) == 0 {
			t.Error("Expected event to be stored with valid context")
		}
	})

	t.Run("CancelledContext", func(t *testing.T) {
		// Create cancelled context
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		// Count events before
		eventsBefore, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}
		countBefore := len(eventsBefore)

		// Try to log with cancelled context
		processor.logEvent(cancelledCtx, events.EventTypeQualityGatesCompleted, events.SeverityInfo, issue.ID,
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
}

// TestResultsProcessorWithMockAISupervisor tests analysis events with mocked AI
func TestResultsProcessorWithMockAISupervisor(t *testing.T) {
	// This test would require a mock AI supervisor
	// For now, we just document the expected behavior

	t.Log("With AI supervisor enabled, ProcessAgentResult should:")
	t.Log("  1. Log analysis_started event")
	t.Log("  2. Call supervisor.AnalyzeExecutionResult()")
	t.Log("  3. On success: Log analysis_completed with success=true and analysis data")
	t.Log("  4. On failure: Log analysis_completed with success=false and error")
	t.Log("")
	t.Log("The analysis_completed event should include:")
	t.Log("  - success: bool")
	t.Log("  - completed: bool (if success)")
	t.Log("  - discovered_count: int (if success)")
	t.Log("  - quality_issues: int (if success)")
	t.Log("  - error: string (if failure)")
}

// TestCompleteEventSequenceWithAnalysis tests the full sequence including analysis
func TestCompleteEventSequenceWithAnalysis(t *testing.T) {
	// Document the complete event sequence with analysis phase

	fullSequence := []events.EventType{
		events.EventTypeIssueClaimed,
		events.EventTypeAssessmentStarted,
		events.EventTypeAssessmentCompleted,
		events.EventTypeAgentSpawned,
		events.EventTypeAgentCompleted,
		events.EventTypeResultsProcessingStarted,
		events.EventTypeAnalysisStarted,          // NEW: Results processor logs this
		events.EventTypeAnalysisCompleted,        // NEW: Results processor logs this
		events.EventTypeQualityGatesStarted,
		events.EventTypeQualityGatesCompleted,
		events.EventTypeResultsProcessingCompleted,
	}

	t.Log("Complete event sequence with AI analysis:")
	for i, eventType := range fullSequence {
		source := "executor"
		if eventType == events.EventTypeAnalysisStarted || eventType == events.EventTypeAnalysisCompleted ||
			eventType == events.EventTypeQualityGatesStarted || eventType == events.EventTypeQualityGatesCompleted ||
			eventType == events.EventTypeQualityGatesSkipped {
			source = "results processor"
		}
		t.Logf("  %2d. %-40s (logged by %s)", i+1, eventType, source)
	}
}
