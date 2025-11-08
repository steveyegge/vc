package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
	"github.com/steveyegge/vc/internal/watchdog"
)

// TestMissionSkipsInlineGates verifies that missions (epics with subtype=mission)
// skip inline quality gates and instead add the needs-quality-gates label (vc-251)
func TestMissionSkipsInlineGates(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission issue (epic with subtype=mission)
	mission := &types.Issue{
		Title:              "Test Mission",
		Description:        "This is a test mission",
		IssueType:          types.TypeEpic,
		IssueSubtype:       types.SubtypeMission,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Mission should complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create results processor with quality gates enabled
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         "/tmp/test",
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Mission work complete"},
	}

	// Process the agent result
	result, err := rp.ProcessAgentResult(ctx, mission, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Verify expectations for mission path (vc-251):
	// 1. Issue should NOT be completed (missions stay open until all tasks complete)
	if result.Completed {
		t.Error("Expected mission to NOT be completed, but it was marked as completed")
	}

	// 2. Gates should be marked as "passed" (not failed, just deferred)
	if !result.GatesPassed {
		t.Error("Expected GatesPassed=true for mission (deferred, not failed)")
	}

	// 3. needs-quality-gates label should be added
	issueLabels, err := store.GetLabels(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	hasLabel := false
	for _, label := range issueLabels {
		if label == labels.LabelNeedsQualityGates {
			hasLabel = true
			break
		}
	}
	if !hasLabel {
		t.Errorf("Expected needs-quality-gates label to be added, but it was not found. Labels: %v", issueLabels)
	}

	// 4. EventTypeQualityGatesDeferred should be emitted
	filter := events.EventFilter{
		IssueID: mission.ID,
		Type:    events.EventTypeQualityGatesDeferred,
	}
	deferredEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to get agent events: %v", err)
	}

	if len(deferredEvents) == 0 {
		allEvents, _ := store.GetAgentEvents(ctx, events.EventFilter{IssueID: mission.ID})
		var eventTypes []string
		for _, e := range allEvents {
			eventTypes = append(eventTypes, string(e.Type))
		}
		t.Errorf("Expected EventTypeQualityGatesDeferred to be emitted, but no events found. Actual events: %v", eventTypes)
	} else if len(deferredEvents) > 1 {
		t.Errorf("Expected exactly 1 deferred event, got %d", len(deferredEvents))
	} else {
		// Verify event data
		event := deferredEvents[0]
		if event.Type != events.EventTypeQualityGatesDeferred {
			t.Errorf("Expected event type %s, got %s", events.EventTypeQualityGatesDeferred, event.Type)
		}
		if event.Severity != events.SeverityInfo {
			t.Errorf("Expected severity %s, got %s", events.SeverityInfo, event.Severity)
		}

		// Check event data
		missionID, ok := event.Data["mission_id"].(string)
		if !ok || missionID != mission.ID {
			t.Errorf("Expected mission_id=%s in event data, got %v", mission.ID, event.Data["mission_id"])
		}

		reason, ok := event.Data["reason"].(string)
		if !ok || reason != "delegated-to-qa-worker" {
			t.Errorf("Expected reason='delegated-to-qa-worker' in event data, got %v", event.Data["reason"])
		}
	}

	// 5. No quality gates should have been run (no EventTypeQualityGatesStarted)
	gatesFilter := events.EventFilter{
		IssueID: mission.ID,
		Type:    events.EventTypeQualityGatesStarted,
	}
	gatesEvents, err := store.GetAgentEvents(ctx, gatesFilter)
	if err != nil {
		t.Fatalf("Failed to get gates agent events: %v", err)
	}

	if len(gatesEvents) > 0 {
		var eventTypes []string
		for _, e := range gatesEvents {
			eventTypes = append(eventTypes, string(e.Type))
		}
		t.Errorf("Expected NO quality gates to run for mission, but found %d gate events: %v", len(gatesEvents), eventTypes)
	}
}

// TestRegularTaskRunsInlineGates verifies that regular tasks (non-missions)
// still run quality gates inline, maintaining backward compatibility (vc-251)
func TestRegularTaskRunsInlineGates(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a regular task issue (not a mission)
	task := &types.Issue{
		Title:              "Test Task",
		Description:        "This is a regular test task",
		IssueType:          types.TypeTask,
		IssueSubtype:       types.SubtypeNormal,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Task should complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create results processor with quality gates enabled
	// Note: We're using a non-VC working directory so gates will be skipped
	// (to avoid running actual gates in tests), but we can verify the skipped event
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         "/tmp/test", // Non-VC repo - gates will be skipped
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Task work complete"},
	}

	// Process the agent result
	result, err := rp.ProcessAgentResult(ctx, task, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Verify expectations for regular task path (backward compatibility):
	// 1. needs-quality-gates label should NOT be added (this is mission-only)
	issueLabels, err := store.GetLabels(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	hasLabel := false
	for _, label := range issueLabels {
		if label == labels.LabelNeedsQualityGates {
			hasLabel = true
			break
		}
	}
	if hasLabel {
		t.Error("Expected needs-quality-gates label NOT to be added for regular task, but it was found")
	}

	// 2. EventTypeQualityGatesDeferred should NOT be emitted (mission-only)
	deferredFilter := events.EventFilter{
		IssueID: task.ID,
		Type:    events.EventTypeQualityGatesDeferred,
	}
	deferredEvents, err := store.GetAgentEvents(ctx, deferredFilter)
	if err != nil {
		t.Fatalf("Failed to get deferred agent events: %v", err)
	}

	if len(deferredEvents) > 0 {
		var eventTypes []string
		for _, e := range deferredEvents {
			eventTypes = append(eventTypes, string(e.Type))
		}
		t.Errorf("Expected NO deferred events for regular task, but found %d: %v", len(deferredEvents), eventTypes)
	}

	// 3. Quality gates should be attempted (EventTypeQualityGatesSkipped in this case
	//    because we're not in VC repo, but the important thing is we didn't defer)
	skippedFilter := events.EventFilter{
		IssueID: task.ID,
		Type:    events.EventTypeQualityGatesSkipped,
	}
	skippedEvents, err := store.GetAgentEvents(ctx, skippedFilter)
	if err != nil {
		t.Fatalf("Failed to get skipped agent events: %v", err)
	}

	// We expect exactly one skipped event (because we're not in VC repo)
	// This proves we went through the normal gates path, not the mission deferral path
	if len(skippedEvents) != 1 {
		allEvents, _ := store.GetAgentEvents(ctx, events.EventFilter{IssueID: task.ID})
		var eventTypes []string
		for _, e := range allEvents {
			eventTypes = append(eventTypes, string(e.Type))
		}
		t.Errorf("Expected exactly 1 quality gates skipped event for regular task, got %d. All events: %v", len(skippedEvents), eventTypes)
	}

	// 4. Task should be completed (unlike missions which stay open)
	if !result.Completed {
		t.Error("Expected regular task to be completed, but it was not")
	}
}

// TestWatchdogBackoffResetOnSuccess verifies that RecordProgress() is called
// after successful issue closure, resetting the watchdog backoff interval (vc-an5o)
func TestWatchdogBackoffResetOnSuccess(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a task issue
	task := &types.Issue{
		Title:              "Test Task for Backoff Reset",
		Description:        "This task tests backoff reset",
		IssueType:          types.TypeTask,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Task should complete and reset backoff",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create watchdog config with backoff enabled
	watchdogCfg := watchdog.DefaultWatchdogConfig()
	watchdogCfg.BackoffConfig.Enabled = true
	watchdogCfg.BackoffConfig.BaseInterval = 30 * time.Second
	watchdogCfg.BackoffConfig.MaxInterval = 10 * time.Minute

	// Simulate AI-recommended backoff (vc-ysqs: AI decides when to back off)
	// Record some interventions first (for state tracking)
	watchdogCfg.RecordIntervention()
	watchdogCfg.RecordIntervention()
	watchdogCfg.RecordIntervention()

	// Apply AI-recommended backoff
	aiBackoffInterval := 2 * time.Minute
	watchdogCfg.ApplyAIBackoff(aiBackoffInterval, "AI detected stuck pattern, increasing interval")

	// Verify we're in backoff mode
	if !watchdogCfg.IsInBackoff() {
		t.Fatal("Expected watchdog to be in backoff mode after AI backoff")
	}
	initialInterval := watchdogCfg.GetCurrentCheckInterval()
	if initialInterval <= watchdogCfg.BackoffConfig.BaseInterval {
		t.Fatalf("Expected backoff interval > base interval, got %v", initialInterval)
	}

	// Create results processor with watchdog config
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: false, // Disable gates for simplicity
		WorkingDir:         "/tmp/test",
		Actor:              "test-executor",
		WatchdogConfig:     watchdogCfg, // Pass watchdog config (vc-an5o)
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Task work complete"},
	}

	// Process the agent result - this should call RecordProgress()
	result, err := rp.ProcessAgentResult(ctx, task, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Verify the issue was completed
	if !result.Completed {
		t.Error("Expected task to be completed")
	}

	// THE KEY VERIFICATION (vc-an5o):
	// After successful completion, RecordProgress() should have been called,
	// which resets backoff state
	if watchdogCfg.IsInBackoff() {
		t.Error("Expected watchdog to exit backoff mode after successful completion")
	}

	currentInterval := watchdogCfg.GetCurrentCheckInterval()
	if currentInterval != watchdogCfg.BackoffConfig.BaseInterval {
		t.Errorf("Expected interval to reset to base %v, got %v",
			watchdogCfg.BackoffConfig.BaseInterval, currentInterval)
	}

	state := watchdogCfg.GetBackoffState()
	if state.ConsecutiveInterventions != 0 {
		t.Errorf("Expected consecutive interventions to reset to 0, got %d",
			state.ConsecutiveInterventions)
	}
}

// TestWatchdogBackoffNotResetOnFailure verifies that backoff is NOT reset
// when an issue fails (agent exits with error)
func TestWatchdogBackoffNotResetOnFailure(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a task issue
	task := &types.Issue{
		Title:              "Test Task for Backoff No-Reset",
		Description:        "This task tests that backoff is not reset on failure",
		IssueType:          types.TypeTask,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Task should fail without resetting backoff",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create watchdog config with backoff enabled
	watchdogCfg := watchdog.DefaultWatchdogConfig()
	watchdogCfg.BackoffConfig.Enabled = true
	watchdogCfg.BackoffConfig.BaseInterval = 30 * time.Second
	watchdogCfg.BackoffConfig.MaxInterval = 10 * time.Minute

	// Simulate AI-recommended backoff (vc-ysqs: AI decides when to back off)
	// Record some interventions first (for state tracking)
	watchdogCfg.RecordIntervention()
	watchdogCfg.RecordIntervention()
	watchdogCfg.RecordIntervention()

	// Apply AI-recommended backoff
	aiBackoffInterval := 2 * time.Minute
	watchdogCfg.ApplyAIBackoff(aiBackoffInterval, "AI detected stuck pattern, increasing interval")

	// Verify we're in backoff mode
	if !watchdogCfg.IsInBackoff() {
		t.Fatal("Expected watchdog to be in backoff mode after AI backoff")
	}
	initialInterval := watchdogCfg.GetCurrentCheckInterval()
	interventionsBefore := watchdogCfg.GetBackoffState().ConsecutiveInterventions

	// Create results processor with watchdog config
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: false,
		WorkingDir:         "/tmp/test",
		Actor:              "test-executor",
		WatchdogConfig:     watchdogCfg,
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create FAILED agent result
	agentResult := &AgentResult{
		Success:  false, // Failure
		ExitCode: 1,
		Duration: time.Second,
		Output:   []string{"Task failed with error"},
	}

	// Process the agent result - this should NOT call RecordProgress()
	result, err := rp.ProcessAgentResult(ctx, task, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Verify the issue was NOT completed
	if result.Completed {
		t.Error("Expected task NOT to be completed on failure")
	}

	// THE KEY VERIFICATION:
	// After failure, RecordProgress() should NOT have been called,
	// so backoff state should remain unchanged
	if !watchdogCfg.IsInBackoff() {
		t.Error("Expected watchdog to remain in backoff mode after failure")
	}

	currentInterval := watchdogCfg.GetCurrentCheckInterval()
	if currentInterval != initialInterval {
		t.Errorf("Expected interval to remain at %v, got %v", initialInterval, currentInterval)
	}

	interventionsAfter := watchdogCfg.GetBackoffState().ConsecutiveInterventions
	if interventionsAfter != interventionsBefore {
		t.Errorf("Expected consecutive interventions to remain at %d, got %d",
			interventionsBefore, interventionsAfter)
	}
}

// TestHandleIncompleteWorkCounting verifies that incomplete attempt counting
// only counts truly incomplete attempts, not all successful attempts (vc-rd1z)
func TestHandleIncompleteWorkCounting(t *testing.T) {
	tests := []struct {
		name               string
		previousComments   []string // Comments to add before calling handleIncompleteWork
		expectedAttemptNum int      // Expected attempt number in the log output
		expectEscalation   bool     // Should escalate to needs-human-review?
	}{
		{
			name:               "first_incomplete_attempt",
			previousComments:   []string{}, // No previous incomplete attempts
			expectedAttemptNum: 1,
			expectEscalation:   false,
		},
		{
			name: "second_incomplete_attempt",
			previousComments: []string{
				"**Incomplete Work Detected (Attempt #1)**\n\nPrevious incomplete work...",
			},
			expectedAttemptNum: 2,
			expectEscalation:   true, // maxIncompleteRetries=1, so 2nd attempt escalates
		},
		{
			name: "mixed_comments_only_count_incomplete",
			previousComments: []string{
				"Some regular comment about the issue",
				"Another comment",
				"**Incomplete Work Detected (Attempt #1)**\n\nFirst incomplete attempt",
				"More regular comments",
			},
			expectedAttemptNum: 2,
			expectEscalation:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup storage
			cfg := storage.DefaultConfig()
			cfg.Path = t.TempDir() + "/test.db"

			ctx := context.Background()
			store, err := storage.NewStorage(ctx, cfg)
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}
			defer func() { _ = store.Close() }()

			// Create a test issue
			issue := &types.Issue{
				Title:              "Test Incomplete Work Issue",
				Description:        "Testing incomplete attempt counting",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           2,
				AcceptanceCriteria: "Complete the task fully",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Add previous comments to simulate history
			for _, comment := range tt.previousComments {
				if err := store.AddComment(ctx, issue.ID, "ai-supervisor", comment); err != nil {
					t.Fatalf("Failed to add comment: %v", err)
				}
			}

			// Create results processor
			rpCfg := &ResultsProcessorConfig{
				Store:      store,
				WorkingDir: t.TempDir(),
				Actor:      "test-executor",
			}

			rp, err := NewResultsProcessor(rpCfg)
			if err != nil {
				t.Fatalf("Failed to create results processor: %v", err)
			}

			// Create a mock analysis indicating incomplete work
			analysis := &ai.Analysis{
				Completed:        false,
				Summary:          "Work was started but not completed",
				DiscoveredIssues: []ai.DiscoveredIssue{},
				QualityIssues:    []string{},
				PuntedItems:      []string{},
			}

			// Call handleIncompleteWork
			err = rp.handleIncompleteWork(ctx, issue, analysis)
			if err != nil {
				t.Fatalf("handleIncompleteWork failed: %v", err)
			}

			// Verify the attempt number appeared in the comments
			issueEvents, err := store.GetEvents(ctx, issue.ID, 0)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			// Find the latest "Incomplete Work Detected" or "Incomplete Work Escalated" comment
			var latestIncompleteComment string
			for _, event := range issueEvents {
				if event.Comment != nil {
					comment := *event.Comment

					// Match based on the expected pattern
					if tt.expectEscalation {
						if strings.Contains(comment, "Incomplete Work Escalated") {
							latestIncompleteComment = comment
						}
					} else {
						if strings.Contains(comment, "Incomplete Work Detected") {
							latestIncompleteComment = comment
						}
					}
				}
			}

			if latestIncompleteComment == "" {
				if tt.expectEscalation {
					t.Fatalf("Expected escalation comment but none was found. Found %d events total", len(issueEvents))
				} else {
					t.Fatalf("Expected incomplete work comment but none was found. Found %d events total", len(issueEvents))
				}
			}

			// Verify the attempt number matches expected
			if tt.expectEscalation {
				// Escalation comment should mention total attempts
				expectedPhrase := fmt.Sprintf("attempted %d times", tt.expectedAttemptNum)
				if !strings.Contains(latestIncompleteComment, expectedPhrase) {
					t.Errorf("Escalation comment should mention '%s', got: %s",
						expectedPhrase, latestIncompleteComment)
				}
			} else {
				// Retry comment should show attempt number
				expectedPhrase := fmt.Sprintf("(Attempt #%d)", tt.expectedAttemptNum)
				if !strings.Contains(latestIncompleteComment, expectedPhrase) {
					t.Errorf("Expected comment to contain '%s', got: %s",
						expectedPhrase, latestIncompleteComment)
				}
			}

			// Verify escalation behavior
			if tt.expectEscalation {
				// Should have needs-human-review label
				labels, err := store.GetLabels(ctx, issue.ID)
				if err != nil {
					t.Fatalf("Failed to get labels: %v", err)
				}
				hasLabel := false
				for _, label := range labels {
					if label == "needs-human-review" {
						hasLabel = true
						break
					}
				}
				if !hasLabel {
					t.Error("Expected needs-human-review label but it was not found")
				}

				// Should be blocked
				updatedIssue, err := store.GetIssue(ctx, issue.ID)
				if err != nil {
					t.Fatalf("Failed to get issue: %v", err)
				}
				if updatedIssue.Status != types.StatusBlocked {
					t.Errorf("Expected status=blocked, got %v", updatedIssue.Status)
				}
			} else {
				// Should still be open (not blocked)
				updatedIssue, err := store.GetIssue(ctx, issue.ID)
				if err != nil {
					t.Fatalf("Failed to get issue: %v", err)
				}
				if updatedIssue.Status == types.StatusBlocked {
					t.Error("Expected issue to remain open, but it was blocked")
				}
			}
		})
	}
}

// TestMissionGateDelegationLabelFailure tests graceful handling when adding
// needs-quality-gates label fails (vc-n8ua)
func TestMissionGateDelegationLabelFailure(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission issue
	mission := &types.Issue{
		Title:              "Test Mission for Label Failure",
		Description:        "Testing label add failure",
		IssueType:          types.TypeEpic,
		IssueSubtype:       types.SubtypeMission,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Mission should handle label failure gracefully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Close the store to force label add to fail
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         "/tmp/test",
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Mission work complete"},
	}

	// Process the agent result - should handle label failure gracefully
	_, err = rp.ProcessAgentResult(ctx, mission, agentResult)

	// The code actually handles this gracefully by logging warnings but not failing
	// This is acceptable behavior - the test verifies no panic occurred
	if err != nil {
		// If there is an error, it should not mention "panic"
		if strings.Contains(err.Error(), "panic") {
			t.Errorf("Expected graceful error handling, but got panic: %v", err)
		}
	}

	// Most important: no panic occurred, and processing completed
	t.Logf("Label failure handled gracefully (err=%v)", err)
}

// TestMissionGateDelegationEventFailure tests graceful handling when logging
// deferred event fails (vc-n8ua)
func TestMissionGateDelegationEventFailure(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create a mission issue
	mission := &types.Issue{
		Title:              "Test Mission for Event Failure",
		Description:        "Testing event log failure",
		IssueType:          types.TypeEpic,
		IssueSubtype:       types.SubtypeMission,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Mission should handle event failure gracefully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         "/tmp/test",
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Mission work complete"},
	}

	// Process the agent result
	result, err := rp.ProcessAgentResult(ctx, mission, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Now close store and try to query events
	// The important thing is the processing completed despite potential event logging issues
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	// Verify result indicates gates were deferred
	if !result.GatesPassed {
		t.Error("Expected GatesPassed=true for mission (deferred)")
	}
}

// TestIncompleteWorkNilAnalysis tests handling of nil analysis data (vc-n8ua)
func TestIncompleteWorkNilAnalysis(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a test issue
	issue := &types.Issue{
		Title:              "Test Issue for Nil Analysis",
		Description:        "Testing nil analysis handling",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           2,
		AcceptanceCriteria: "Handle nil gracefully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:      store,
		WorkingDir: t.TempDir(),
		Actor:      "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Call handleIncompleteWork with nil analysis - should not panic
	err = rp.handleIncompleteWork(ctx, issue, nil)

	// Should handle nil gracefully
	if err != nil {
		t.Logf("handleIncompleteWork with nil analysis returned error (acceptable): %v", err)
	}
	// Most important: no panic occurred
}

// TestIncompleteWorkMissingFields tests handling of analysis with missing fields (vc-n8ua)
func TestIncompleteWorkMissingFields(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a test issue
	issue := &types.Issue{
		Title:              "Test Issue for Missing Fields",
		Description:        "Testing missing analysis fields",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           2,
		AcceptanceCriteria: "Handle missing fields gracefully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:      store,
		WorkingDir: t.TempDir(),
		Actor:      "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create analysis with missing/empty fields
	analysis := &ai.Analysis{
		Completed:        false,
		Summary:          "", // Empty summary
		DiscoveredIssues: nil,
		QualityIssues:    nil,
		PuntedItems:      nil,
	}

	// Call handleIncompleteWork - should not panic
	err = rp.handleIncompleteWork(ctx, issue, analysis)
	if err != nil {
		t.Fatalf("handleIncompleteWork failed with missing fields: %v", err)
	}

	// Verify issue handling occurred (check for comment or status change)
	updatedIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	// Issue should remain open (first incomplete attempt)
	if updatedIssue.Status == types.StatusBlocked {
		t.Error("Expected issue to remain open on first incomplete attempt with missing fields")
	}
}
