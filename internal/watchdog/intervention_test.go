package watchdog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func TestInterventionController_PauseAgent(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory storage
	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create intervention controller
	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Create a test issue that will be "executing" (ID will be auto-generated)
	testIssue := &types.Issue{
		Title:              "Test Issue",
		Description:        "Test issue for intervention",
		Status:             types.StatusInProgress,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}
	issueID := testIssue.ID

	// Create a cancellable context to simulate agent execution
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()

	// Track whether cancel was called
	cancelCalled := false
	wrappedCancel := func() {
		cancelCalled = true
		agentCancel()
	}

	// Register the agent context
	ic.SetAgentContext(issueID, wrappedCancel)

	// Verify the agent is active
	if !ic.HasActiveAgent() {
		t.Error("Expected active agent after SetAgentContext")
	}
	if ic.GetCurrentIssueID() != issueID {
		t.Errorf("Expected current issue %s, got %s", issueID, ic.GetCurrentIssueID())
	}

	// Create an anomaly report
	report := &AnomalyReport{
		Detected:          true,
		AnomalyType:       AnomalyInfiniteLoop,
		Severity:          SeverityHigh,
		Description:       "Issue appears to be stuck in infinite loop",
		RecommendedAction: ActionStopExecution,
		Reasoning:         "Issue has been executing for 30 minutes with no progress",
		Confidence:        0.85,
		AffectedIssues:    []string{issueID},
	}

	// Call PauseAgent
	result, err := ic.PauseAgent(ctx, report)
	if err != nil {
		t.Fatalf("PauseAgent failed: %v", err)
	}

	// Verify the result
	if !result.Success {
		t.Error("Expected successful intervention")
	}
	if result.InterventionType != InterventionPauseAgent {
		t.Errorf("Expected intervention type %s, got %s", InterventionPauseAgent, result.InterventionType)
	}
	if result.EscalationIssueID == "" {
		t.Error("Expected escalation issue to be created")
	}

	// Verify cancel was called
	if !cancelCalled {
		t.Error("Expected agent cancel function to be called")
	}

	// Verify agent context was canceled
	select {
	case <-agentCtx.Done():
		// Expected - context was canceled
	case <-time.After(100 * time.Millisecond):
		t.Error("Agent context was not canceled")
	}

	// Verify escalation issue was created
	escalationIssue, err := store.GetIssue(ctx, result.EscalationIssueID)
	if err != nil {
		t.Fatalf("Failed to retrieve escalation issue: %v", err)
	}
	if escalationIssue.Status != types.StatusOpen {
		t.Errorf("Expected escalation issue to be open, got %s", escalationIssue.Status)
	}
	if escalationIssue.IssueType != types.TypeTask {
		t.Errorf("Expected escalation issue type task, got %s", escalationIssue.IssueType)
	}

	// Verify watchdog event was created (as a comment)
	events, err := store.GetEvents(ctx, issueID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	foundWatchdogEvent := false
	for _, event := range events {
		if event.EventType == types.EventCommented && event.Comment != nil {
			if event.Actor == "watchdog-test-executor" {
				foundWatchdogEvent = true
				break
			}
		}
	}
	if !foundWatchdogEvent {
		t.Error("Expected watchdog event to be created")
	}

	// Verify intervention history
	history := ic.GetInterventionHistory()
	if len(history) != 1 {
		t.Errorf("Expected 1 intervention in history, got %d", len(history))
	}
	if len(history) > 0 && history[0].InterventionType != InterventionPauseAgent {
		t.Errorf("Expected intervention type %s in history, got %s",
			InterventionPauseAgent, history[0].InterventionType)
	}
}

func TestInterventionController_KillAgent(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory storage
	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create intervention controller
	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Create a test issue (ID will be auto-generated)
	testIssue := &types.Issue{
		Title:              "Test Issue for Kill",
		Description:        "Test issue for kill intervention",
		Status:             types.StatusInProgress,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}
	issueID := testIssue.ID

	// Create a cancellable context
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()

	cancelCalled := false
	wrappedCancel := func() {
		cancelCalled = true
		agentCancel()
	}

	// Register the agent context
	ic.SetAgentContext(issueID, wrappedCancel)

	// Create a critical anomaly report
	report := &AnomalyReport{
		Detected:          true,
		AnomalyType:       AnomalyThrashing,
		Severity:          SeverityCritical,
		Description:       "Agent is thrashing - rapid state changes with no progress",
		RecommendedAction: ActionStopExecution,
		Reasoning:         "Detected 100+ state transitions in 1 minute with no successful completion",
		Confidence:        0.95,
		AffectedIssues:    []string{issueID},
	}

	// Call KillAgent
	result, err := ic.KillAgent(ctx, report)
	if err != nil {
		t.Fatalf("KillAgent failed: %v", err)
	}

	// Verify the result
	if !result.Success {
		t.Error("Expected successful intervention")
	}
	if result.InterventionType != InterventionKillAgent {
		t.Errorf("Expected intervention type %s, got %s", InterventionKillAgent, result.InterventionType)
	}
	if result.EscalationIssueID == "" {
		t.Error("Expected escalation issue to be created")
	}

	// Verify cancel was called
	if !cancelCalled {
		t.Error("Expected agent cancel function to be called")
	}

	// Verify agent context was canceled
	select {
	case <-agentCtx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Agent context was not canceled")
	}
}

func TestInterventionController_Intervene(t *testing.T) {
	ctx := context.Background()

	// Setup storage
	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Create test issue (ID will be auto-generated)
	testIssue := &types.Issue{
		Title:              "Test Issue for Intervene",
		Description:        "Test issue",
		Status:             types.StatusInProgress,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}
	issueID := testIssue.ID

	// Test different recommended actions
	testCases := []struct {
		name              string
		recommendedAction RecommendedAction
		expectKill        bool
		expectPause       bool
	}{
		{
			name:              "stop_execution triggers kill",
			recommendedAction: ActionStopExecution,
			expectKill:        true,
		},
		{
			name:              "restart_agent triggers pause",
			recommendedAction: ActionRestartAgent,
			expectPause:       true,
		},
		{
			name:              "mark_as_blocked triggers pause",
			recommendedAction: ActionMarkAsBlocked,
			expectPause:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create cancellable context
			agentCtx, agentCancel := context.WithCancel(ctx)
			defer agentCancel()

			cancelCalled := false
			wrappedCancel := func() {
				cancelCalled = true
				agentCancel()
			}

			// Register agent context
			ic.SetAgentContext(issueID, wrappedCancel)

			// Create anomaly report with the recommended action
			report := &AnomalyReport{
				Detected:          true,
				AnomalyType:       AnomalyStuckState,
				Severity:          SeverityHigh,
				Description:       "Test anomaly",
				RecommendedAction: tc.recommendedAction,
				Reasoning:         "Test reasoning",
				Confidence:        0.9,
				AffectedIssues:    []string{issueID},
			}

			// Call Intervene
			result, err := ic.Intervene(ctx, report)
			if err != nil {
				t.Fatalf("Intervene failed: %v", err)
			}

			// Verify intervention occurred
			if !result.Success {
				t.Error("Expected successful intervention")
			}
			if !cancelCalled && (tc.expectKill || tc.expectPause) {
				t.Error("Expected cancel to be called")
			}

			// Verify context was canceled
			if tc.expectKill || tc.expectPause {
				select {
				case <-agentCtx.Done():
					// Expected
				case <-time.After(100 * time.Millisecond):
					t.Error("Agent context was not canceled")
				}
			}

			// Clean up for next test
			ic.ClearAgentContext()
		})
	}
}

func TestInterventionController_NoActiveAgent(t *testing.T) {
	ctx := context.Background()

	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Try to pause when no agent is active
	report := &AnomalyReport{
		Detected:          true,
		AnomalyType:       AnomalyInfiniteLoop,
		Severity:          SeverityHigh,
		Description:       "Test",
		RecommendedAction: ActionStopExecution,
		Reasoning:         "Test",
		Confidence:        0.9,
	}

	_, err = ic.PauseAgent(ctx, report)
	if err == nil {
		t.Error("Expected error when pausing with no active agent")
	}

	_, err = ic.KillAgent(ctx, report)
	if err == nil {
		t.Error("Expected error when killing with no active agent")
	}
}

func TestInterventionController_ClearAgentContext(t *testing.T) {
	ctx := context.Background()

	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Set agent context
	_, cancel := context.WithCancel(ctx)
	ic.SetAgentContext("vc-test", cancel)

	if !ic.HasActiveAgent() {
		t.Error("Expected active agent")
	}

	// Clear agent context
	ic.ClearAgentContext()

	if ic.HasActiveAgent() {
		t.Error("Expected no active agent after clear")
	}
	if ic.GetCurrentIssueID() != "" {
		t.Error("Expected empty current issue ID after clear")
	}
}

func TestInterventionController_InterventionHistory(t *testing.T) {
	ctx := context.Background()

	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create controller with small history size for testing
	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
		MaxHistorySize:     3, // Small size to test pruning
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Call PauseAgent 5 times to test history pruning
	// This now works correctly with Beads v0.22.0 which fixes the N+1 query bug (bd-5ots)
	for i := 1; i <= 5; i++ {
		// Create a test issue for this iteration
		testIssue := &types.Issue{
			Title:              fmt.Sprintf("Test Issue %d", i),
			Description:        "Test issue for intervention history",
			Status:             types.StatusInProgress,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
			t.Fatalf("Failed to create test issue %d: %v", i, err)
		}
		issueID := testIssue.ID

		// Register agent context
		_, agentCancel := context.WithCancel(ctx)
		defer agentCancel()
		ic.SetAgentContext(issueID, agentCancel)

		// Create anomaly report
		report := &AnomalyReport{
			Detected:          true,
			AnomalyType:       AnomalyInfiniteLoop,
			Severity:          SeverityHigh,
			Description:       fmt.Sprintf("Test anomaly %d", i),
			RecommendedAction: ActionStopExecution,
			Reasoning:         "Test",
			Confidence:        0.9,
			AffectedIssues:    []string{issueID},
		}

		// Call PauseAgent
		_, err := ic.PauseAgent(ctx, report)
		if err != nil {
			t.Fatalf("PauseAgent failed for iteration %d: %v", i, err)
		}

		// Clear agent context for next iteration
		ic.ClearAgentContext()
	}

	// Verify history is limited to max size
	history := ic.GetInterventionHistory()
	if len(history) != 3 {
		t.Errorf("Expected history size 3, got %d", len(history))
	}

	// Verify we kept the most recent interventions
	if len(history) > 0 {
		lastIntervention := history[len(history)-1]
		if lastIntervention.AnomalyReport.Description != "Test anomaly 5" {
			t.Error("Expected most recent intervention to be last in history")
		}
	}
}

// TestRepeatedDetectionIntervention tests the behavior when the same issue
// is detected multiple times with the same anomaly type (vc-tbyn).
//
// This test addresses the pattern seen in vc-hpcl where 58 separate detections
// occurred over 30 minutes, all with intervention=pause_agent. This test verifies:
// 1. Detection system behavior when same issue is detected multiple times
// 2. Escalation issue deduplication works correctly
// 3. Intervention count is properly tracked
// 4. State persistence: intervention state is recorded correctly
func TestRepeatedDetectionIntervention(t *testing.T) {
	ctx := context.Background()

	store, err := storage.NewStorage(ctx, &storage.Config{
		Path:    ":memory:",
	})
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	ic, err := NewInterventionController(&InterventionControllerConfig{
		Store:              store,
		ExecutorInstanceID: "test-executor",
		MaxHistorySize:     100,
	})
	if err != nil {
		t.Fatalf("Failed to create intervention controller: %v", err)
	}

	// Create a test issue that will be repeatedly detected
	testIssue := &types.Issue{
		Title:              "Repeatedly Detected Issue",
		Description:        "This issue triggers multiple detections",
		Status:             types.StatusInProgress,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, testIssue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}
	issueID := testIssue.ID

	// Simulate 10 repeated detections of the same anomaly type on the same issue
	// This simulates what happened in vc-hpcl (though scaled down from 58)
	const detectionCount = 10
	var escalationIssueIDs []string

	for i := 1; i <= detectionCount; i++ {
		// Simulate agent re-starting after each pause
		agentCtx, agentCancel := context.WithCancel(ctx)
		ic.SetAgentContext(issueID, agentCancel)

		// Create anomaly report with same anomaly type each time
		report := &AnomalyReport{
			Detected:          true,
			AnomalyType:       AnomalyInfiniteLoop, // Same type each time
			Severity:          SeverityHigh,
			Description:       fmt.Sprintf("Iteration %d: Issue appears to be stuck in infinite loop", i),
			RecommendedAction: ActionRestartAgent,
			Reasoning:         fmt.Sprintf("Detection #%d: Issue has been executing with no progress", i),
			Confidence:        0.92 + float64(i)*0.01, // Confidence varies slightly (0.92-0.95 range)
			AffectedIssues:    []string{issueID},
		}

		// Pause the agent
		result, err := ic.PauseAgent(ctx, report)
		if err != nil {
			t.Fatalf("PauseAgent failed for iteration %d: %v", i, err)
		}

		// Verify intervention succeeded
		if !result.Success {
			t.Errorf("Iteration %d: Expected successful intervention", i)
		}

		// Track escalation issue IDs
		if result.EscalationIssueID != "" {
			escalationIssueIDs = append(escalationIssueIDs, result.EscalationIssueID)
		}

		// Verify agent was actually paused (context canceled)
		select {
		case <-agentCtx.Done():
			// Expected - agent was paused
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Iteration %d: Agent context was not canceled", i)
		}

		// Clear agent context to simulate agent stopping
		ic.ClearAgentContext()

		// Small delay to simulate time passing between detections
		time.Sleep(10 * time.Millisecond)
	}

	// TEST 1: Verify deduplication - should only create ONE escalation issue
	// All 10 detections of the same anomaly type should update the same escalation
	uniqueEscalationIDs := make(map[string]bool)
	for _, id := range escalationIssueIDs {
		uniqueEscalationIDs[id] = true
	}

	if len(uniqueEscalationIDs) != 1 {
		t.Errorf("Expected 1 unique escalation issue (deduplication), got %d: %v",
			len(uniqueEscalationIDs), escalationIssueIDs)
	}

	// TEST 2: Verify the escalation issue was updated with all detections
	if len(escalationIssueIDs) > 0 {
		escalationID := escalationIssueIDs[0]
		escalation, err := store.GetIssue(ctx, escalationID)
		if err != nil {
			t.Fatalf("Failed to get escalation issue: %v", err)
		}

		// The description should contain multiple detection entries (one per iteration)
		// Each detection adds a new line like: "- 2025-11-04 18:33:43: Detected..."
		detectionLines := 0
		lines := splitLines(escalation.Description)
		for _, line := range lines {
			if containsSubstring(line, "Detected (severity=") {
				detectionLines++
			}
		}

		// Should have detectionCount entries in the history
		if detectionLines != detectionCount {
			t.Errorf("Expected %d detection entries in escalation description, got %d",
				detectionCount, detectionLines)
		}
	}

	// TEST 3: Verify intervention history tracks all interventions
	history := ic.GetInterventionHistory()
	if len(history) != detectionCount {
		t.Errorf("Expected %d interventions in history, got %d", detectionCount, len(history))
	}

	// TEST 4: Verify all interventions were of type pause_agent
	for i, intervention := range history {
		if intervention.InterventionType != InterventionPauseAgent {
			t.Errorf("Intervention %d: Expected type %s, got %s",
				i, InterventionPauseAgent, intervention.InterventionType)
		}
	}

	// TEST 5: Verify intervention count was recorded in database (vc-165b)
	// GetExecutionState should show the intervention count
	execState, err := store.GetExecutionState(ctx, issueID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}

	if execState != nil {
		// intervention_count should match the number of times we paused
		if execState.InterventionCount != detectionCount {
			t.Errorf("Expected intervention_count=%d in execution state, got %d",
				detectionCount, execState.InterventionCount)
		}

		// last_intervention_time should be set
		if execState.LastInterventionTime == nil {
			t.Error("Expected last_intervention_time to be set")
		}
	}
}

// Helper functions for test
func splitLines(s string) []string {
	result := []string{}
	current := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, current)
			current = ""
		} else {
			current += string(s[i])
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
