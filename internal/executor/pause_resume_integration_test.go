package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestPauseResumeBasicCycle tests the basic pause/resume workflow
// Scenario: Pause a running task, verify context is saved, resume and verify completion
func TestPauseResumeBasicCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create a test issue
	issue := &types.Issue{
		ID:                 "vc-pause-001",
		Title:              "Test basic pause/resume",
		Description:        "Test that pause saves context and resume loads it",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Task can be paused and resumed successfully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Set as currently executing
	exec.interruptMgr.SetCurrentIssue(issue)

	// Progress to executing state
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		t.Fatalf("Failed to update to claimed: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}

	// Request pause
	resp, err := exec.interruptMgr.HandlePauseCommand(ctx, issue.ID, "test pause")
	if err != nil {
		t.Fatalf("Failed to pause task: %v", err)
	}
	if resp == nil {
		t.Fatal("Expected pause response, got nil")
	}
	if resp["status"] != "interrupt_requested" {
		t.Errorf("Expected status 'interrupt_requested', got %v", resp["status"])
	}

	// Verify interrupt flag is set
	if !exec.interruptMgr.IsInterruptRequested() {
		t.Error("Interrupt flag should be set after pause request")
	}

	// Save interrupt context (simulating checkpoint detection)
	if err := exec.interruptMgr.SaveInterruptContext(ctx, issue, "control-cli", "test pause", "executing"); err != nil {
		t.Fatalf("Failed to save interrupt context: %v", err)
	}

	// Release issue and reopen
	if err := store.ReleaseIssueAndReopen(ctx, issue.ID, "executor", "Task paused"); err != nil {
		t.Fatalf("Failed to release and reopen issue: %v", err)
	}

	// Clear interrupt flag
	exec.interruptMgr.ClearInterrupt()
	exec.interruptMgr.SetCurrentIssue(nil)

	// Verify interrupt metadata was saved
	metadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if metadata == nil {
		t.Fatal("Expected interrupt metadata, got nil")
	}
	if metadata.IssueID != issue.ID {
		t.Errorf("Expected issue ID %s, got %s", issue.ID, metadata.IssueID)
	}
	if metadata.Reason != "test pause" {
		t.Errorf("Expected reason 'test pause', got %s", metadata.Reason)
	}
	if metadata.ExecutionState != "executing" {
		t.Errorf("Expected execution state 'executing', got %s", metadata.ExecutionState)
	}
	if metadata.ResumeCount != 0 {
		t.Errorf("Expected resume count 0, got %d", metadata.ResumeCount)
	}

	// Verify issue has 'interrupted' label
	issueLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}
	hasInterruptedLabel := false
	for _, label := range issueLabels {
		if label == "interrupted" {
			hasInterruptedLabel = true
			break
		}
	}
	if !hasInterruptedLabel {
		t.Error("Issue should have 'interrupted' label")
	}

	// Verify issue status is open
	reopenedIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if reopenedIssue.Status != types.StatusOpen {
		t.Errorf("Expected status open, got %s", reopenedIssue.Status)
	}

	// Phase 2: Resume the task
	// Reclaim the issue
	if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to reclaim issue: %v", err)
	}

	// Check and load interrupt context
	resumeContext, err := exec.interruptMgr.CheckAndLoadInterruptContext(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to load interrupt context: %v", err)
	}
	if resumeContext == "" {
		t.Error("Expected resume context, got empty string")
	}

	// Verify resume context contains expected information
	if !contains(resumeContext, "interrupted") {
		t.Error("Resume context should mention interruption")
	}
	if !contains(resumeContext, "test pause") {
		t.Error("Resume context should include reason")
	}
	if !contains(resumeContext, "executing") {
		t.Error("Resume context should include execution state")
	}

	// Verify 'interrupted' label was removed
	resumedLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels after resume: %v", err)
	}
	for _, label := range resumedLabels {
		if label == "interrupted" {
			t.Error("'interrupted' label should be removed after resume")
		}
	}

	// Verify resume count was incremented
	metadataAfterResume, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata after resume: %v", err)
	}
	if metadataAfterResume == nil {
		t.Fatal("Expected interrupt metadata after resume, got nil")
	}
	if metadataAfterResume.ResumeCount != 1 {
		t.Errorf("Expected resume count 1 after resume, got %d", metadataAfterResume.ResumeCount)
	}
	if metadataAfterResume.ResumedAt == nil {
		t.Error("ResumedAt should be set")
	}

	// Complete the task
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		t.Fatalf("Failed to update to claimed: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
		t.Fatalf("Failed to update to analyzing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
		t.Fatalf("Failed to update to gates: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCommitting); err != nil {
		t.Fatalf("Failed to update to committing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCompleted); err != nil {
		t.Fatalf("Failed to complete: %v", err)
	}

	// Close the issue
	if err := store.CloseIssue(ctx, issue.ID, "completed after resume", exec.instanceID); err != nil {
		t.Fatalf("Failed to close issue: %v", err)
	}

	// Verify final state
	finalIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final issue: %v", err)
	}
	if finalIssue.Status != types.StatusClosed {
		t.Errorf("Expected final status closed, got %s", finalIssue.Status)
	}

	t.Log("Basic pause/resume cycle completed successfully")
}

// TestPauseNonExecutingIssue tests that pausing a non-executing issue returns an error
func TestPauseNonExecutingIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create an issue but don't set it as executing
	issue := &types.Issue{
		ID:                 "vc-pause-002",
		Title:              "Non-executing issue",
		Description:        "This issue is not executing",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Test criteria",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Try to pause without setting it as current
	_, err := exec.interruptMgr.HandlePauseCommand(ctx, issue.ID, "test pause")
	if err == nil {
		t.Fatal("Expected error when pausing non-executing issue, got nil")
	}
	if err.Error() != "no task currently executing" {
		t.Errorf("Expected 'no task currently executing' error, got: %v", err)
	}

	t.Log("Correctly rejected pause request for non-executing issue")
}

// TestPauseWrongIssue tests that pausing a different issue than the one executing returns an error
func TestPauseWrongIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create two issues
	issue1 := &types.Issue{
		ID:          "vc-pause-003",
		Title:       "Executing issue",
		Description: "This issue is executing",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("Failed to create issue1: %v", err)
	}

	issue2 := &types.Issue{
		ID:          "vc-pause-004",
		Title:       "Different issue",
		Description: "This issue is not executing",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("Failed to create issue2: %v", err)
	}

	// Set issue1 as currently executing
	exec.interruptMgr.SetCurrentIssue(issue1)

	// Try to pause issue2 (wrong issue)
	_, err := exec.interruptMgr.HandlePauseCommand(ctx, issue2.ID, "test pause")
	if err == nil {
		t.Fatal("Expected error when pausing wrong issue, got nil")
	}
	expectedError := "issue vc-pause-004 is not currently executing (current: vc-pause-003)"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got: %v", expectedError, err)
	}

	t.Log("Correctly rejected pause request for wrong issue")
}

// TestResumeWithoutInterruptMetadata tests that resuming an issue without interrupt metadata works normally
func TestResumeWithoutInterruptMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create an issue without interrupt metadata
	issue := &types.Issue{
		ID:          "vc-pause-005",
		Title:       "Fresh issue",
		Description: "No interrupt metadata",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Check for interrupt context (should be empty)
	resumeContext, err := exec.interruptMgr.CheckAndLoadInterruptContext(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Expected no error for missing interrupt metadata, got: %v", err)
	}
	if resumeContext != "" {
		t.Errorf("Expected empty resume context, got: %s", resumeContext)
	}

	// Verify no interrupt metadata exists
	metadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if metadata != nil {
		t.Error("Expected nil metadata for fresh issue")
	}

	t.Log("Successfully handled issue without interrupt metadata")
}

// TestMultiplePauseResumeCycles tests that an issue can be paused and resumed multiple times
func TestMultiplePauseResumeCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create a test issue
	issue := &types.Issue{
		ID:          "vc-pause-006",
		Title:       "Multiple pause/resume cycles",
		Description: "Test multiple interrupts",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Perform 3 pause/resume cycles
	for cycle := 1; cycle <= 3; cycle++ {
		t.Logf("Starting pause/resume cycle %d", cycle)

		// Claim the issue
		if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
			t.Fatalf("Cycle %d: Failed to claim issue: %v", cycle, err)
		}

		// Set as currently executing
		exec.interruptMgr.SetCurrentIssue(issue)

		// Progress to executing state
		if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
			t.Fatalf("Cycle %d: Failed to update to claimed: %v", cycle, err)
		}
		if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
			t.Fatalf("Cycle %d: Failed to update to assessing: %v", cycle, err)
		}
		if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
			t.Fatalf("Cycle %d: Failed to update to executing: %v", cycle, err)
		}

		// Request pause
		exec.interruptMgr.RequestInterrupt()

		// Save interrupt context
		reason := "test pause cycle " + string(rune('0'+cycle))
		if err := exec.interruptMgr.SaveInterruptContext(ctx, issue, "control-cli", reason, "executing"); err != nil {
			t.Fatalf("Cycle %d: Failed to save interrupt context: %v", cycle, err)
		}

		// Release and reopen
		if err := store.ReleaseIssueAndReopen(ctx, issue.ID, "executor", "Paused"); err != nil {
			t.Fatalf("Cycle %d: Failed to release and reopen: %v", cycle, err)
		}

		// Clear interrupt
		exec.interruptMgr.ClearInterrupt()
		exec.interruptMgr.SetCurrentIssue(nil)

		// Verify metadata
		metadata, err := store.GetInterruptMetadata(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to get interrupt metadata: %v", cycle, err)
		}
		if metadata == nil {
			t.Fatalf("Cycle %d: Expected interrupt metadata, got nil", cycle)
		}

		// Resume
		resumeContext, err := exec.interruptMgr.CheckAndLoadInterruptContext(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to load interrupt context: %v", cycle, err)
		}
		if resumeContext == "" {
			t.Errorf("Cycle %d: Expected resume context", cycle)
		}

		// Verify resume count increments
		metadataAfterResume, err := store.GetInterruptMetadata(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to get metadata after resume: %v", cycle, err)
		}
		expectedResumeCount := cycle
		if metadataAfterResume.ResumeCount != expectedResumeCount {
			t.Errorf("Cycle %d: Expected resume count %d, got %d", cycle, expectedResumeCount, metadataAfterResume.ResumeCount)
		}
	}

	t.Log("Multiple pause/resume cycles completed successfully")
}

// TestInterruptContextSerialization tests that agent context is properly serialized and deserialized
func TestInterruptContextSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create a test issue
	issue := &types.Issue{
		ID:          "vc-pause-007",
		Title:       "Context serialization test",
		Description: "Test that context is properly saved and loaded",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create rich interrupt metadata with full context
	metadata := &types.InterruptMetadata{
		IssueID:            issue.ID,
		InterruptedAt:      time.Now(),
		InterruptedBy:      "test-user",
		Reason:             "testing context serialization",
		ExecutorInstanceID: exec.instanceID,
		ExecutionState:     "executing",
		LastTool:           "Edit",
		WorkingNotes:       "Working on file operations",
		ProgressSummary:    "Completed 3 of 5 tasks",
		ResumeCount:        0,
	}

	// Create agent context with todos
	agentContext := types.AgentContext{
		InterruptedAt:   time.Now(),
		WorkingNotes:    "Working on pause/resume tests",
		ProgressSummary: "Completed basic tests, working on edge cases",
		CurrentPhase:    "executing",
		LastTool:        "Edit",
		LastToolResult:  "File updated successfully",
		Todos:           []string{"Write test case 5", "Write test case 6", "Run all tests"},
		CompletedTodos:  []string{"Write test case 1", "Write test case 2", "Write test case 3", "Write test case 4"},
		Observations:    []string{"Context serialization is critical", "Need to test JSON marshaling"},
		SessionDuration: 15*time.Minute + 30*time.Second,
	}

	// Serialize context
	contextJSON, err := json.Marshal(agentContext)
	if err != nil {
		t.Fatalf("Failed to serialize agent context: %v", err)
	}
	metadata.ContextSnapshot = string(contextJSON)

	// Save metadata
	if err := store.SaveInterruptMetadata(ctx, metadata); err != nil {
		t.Fatalf("Failed to save interrupt metadata: %v", err)
	}

	// Retrieve metadata
	loadedMetadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if loadedMetadata == nil {
		t.Fatal("Expected metadata, got nil")
	}

	// Verify basic fields
	if loadedMetadata.IssueID != issue.ID {
		t.Errorf("Expected issue ID %s, got %s", issue.ID, loadedMetadata.IssueID)
	}
	if loadedMetadata.Reason != "testing context serialization" {
		t.Errorf("Expected reason 'testing context serialization', got %s", loadedMetadata.Reason)
	}
	if loadedMetadata.LastTool != "Edit" {
		t.Errorf("Expected last tool 'Edit', got %s", loadedMetadata.LastTool)
	}
	if loadedMetadata.ProgressSummary != "Completed 3 of 5 tasks" {
		t.Errorf("Expected progress summary 'Completed 3 of 5 tasks', got %s", loadedMetadata.ProgressSummary)
	}

	// Verify context snapshot was saved
	if loadedMetadata.ContextSnapshot == "" {
		t.Fatal("Expected context snapshot, got empty string")
	}

	// Deserialize and verify context
	var loadedContext types.AgentContext
	if err := json.Unmarshal([]byte(loadedMetadata.ContextSnapshot), &loadedContext); err != nil {
		t.Fatalf("Failed to deserialize context: %v", err)
	}

	// Verify todos
	if len(loadedContext.Todos) != 3 {
		t.Errorf("Expected 3 todos, got %d", len(loadedContext.Todos))
	}
	if len(loadedContext.CompletedTodos) != 4 {
		t.Errorf("Expected 4 completed todos, got %d", len(loadedContext.CompletedTodos))
	}

	// Verify observations
	if len(loadedContext.Observations) != 2 {
		t.Errorf("Expected 2 observations, got %d", len(loadedContext.Observations))
	}

	// Verify last tool
	if loadedContext.LastTool != "Edit" {
		t.Errorf("Expected last tool 'Edit', got %s", loadedContext.LastTool)
	}
	if loadedContext.LastToolResult != "File updated successfully" {
		t.Errorf("Expected last tool result 'File updated successfully', got %s", loadedContext.LastToolResult)
	}

	// Verify session duration
	expectedDuration := 15*time.Minute + 30*time.Second
	if loadedContext.SessionDuration != expectedDuration {
		t.Errorf("Expected session duration %v, got %v", expectedDuration, loadedContext.SessionDuration)
	}

	t.Log("Context serialization test completed successfully")
}

// TestListInterruptedIssues tests that we can list all interrupted issues
func TestListInterruptedIssues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create multiple interrupted issues
	numIssues := 5
	issueIDs := make([]string, numIssues)

	for i := 0; i < numIssues; i++ {
		issueID := "vc-pause-" + string(rune('a'+i))
		issueIDs[i] = issueID

		issue := &types.Issue{
			ID:          issueID,
			Title:       "Interrupted issue " + string(rune('A'+i)),
			Description: "Test issue for listing",
			IssueType:   types.TypeTask,
			Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
			Priority:    1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create issue %s: %v", issueID, err)
		}

		// Save interrupt metadata
		metadata := &types.InterruptMetadata{
			IssueID:            issueID,
			InterruptedAt:      time.Now().Add(time.Duration(-i) * time.Minute),
			InterruptedBy:      "test-user",
			Reason:             "test pause " + string(rune('A'+i)),
			ExecutorInstanceID: exec.instanceID,
			ExecutionState:     "executing",
			ResumeCount:        0,
		}
		if err := store.SaveInterruptMetadata(ctx, metadata); err != nil {
			t.Fatalf("Failed to save metadata for %s: %v", issueID, err)
		}
	}

	// List all interrupted issues
	interrupted, err := store.ListInterruptedIssues(ctx)
	if err != nil {
		t.Fatalf("Failed to list interrupted issues: %v", err)
	}

	if len(interrupted) != numIssues {
		t.Errorf("Expected %d interrupted issues, got %d", numIssues, len(interrupted))
	}

	// Verify they're ordered by interrupted_at DESC (most recent first)
	for i := 0; i < len(interrupted)-1; i++ {
		if interrupted[i].InterruptedAt.Before(interrupted[i+1].InterruptedAt) {
			t.Errorf("Interrupted issues not sorted by interrupted_at DESC")
		}
	}

	// Verify all our issues are in the list
	foundIssues := make(map[string]bool)
	for _, meta := range interrupted {
		foundIssues[meta.IssueID] = true
	}
	for _, expectedID := range issueIDs {
		if !foundIssues[expectedID] {
			t.Errorf("Expected to find issue %s in interrupted list", expectedID)
		}
	}

	t.Log("List interrupted issues test completed successfully")
}

// TestDeleteInterruptMetadata tests that interrupt metadata can be deleted
func TestDeleteInterruptMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create an issue with interrupt metadata
	issue := &types.Issue{
		ID:          "vc-pause-delete",
		Title:       "Delete metadata test",
		Description: "Test metadata deletion",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Save interrupt metadata
	metadata := &types.InterruptMetadata{
		IssueID:            issue.ID,
		InterruptedAt:      time.Now(),
		InterruptedBy:      "test-user",
		Reason:             "test deletion",
		ExecutorInstanceID: exec.instanceID,
		ExecutionState:     "executing",
		ResumeCount:        0,
	}
	if err := store.SaveInterruptMetadata(ctx, metadata); err != nil {
		t.Fatalf("Failed to save interrupt metadata: %v", err)
	}

	// Verify it exists
	loadedMetadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if loadedMetadata == nil {
		t.Fatal("Expected metadata before deletion, got nil")
	}

	// Delete the metadata
	if err := store.DeleteInterruptMetadata(ctx, issue.ID); err != nil {
		t.Fatalf("Failed to delete interrupt metadata: %v", err)
	}

	// Verify it's gone
	deletedMetadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata after deletion: %v", err)
	}
	if deletedMetadata != nil {
		t.Error("Expected nil metadata after deletion, got metadata")
	}

	t.Log("Delete interrupt metadata test completed successfully")
}

// TestBudgetTriggeredPause tests that a task is auto-paused when budget is exceeded
// Note: This is a simplified test that simulates budget exceeded condition
// In a real scenario, the budget monitor would trigger the pause
func TestBudgetTriggeredPause(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create a test issue
	issue := &types.Issue{
		ID:          "vc-pause-budget",
		Title:       "Budget-triggered pause test",
		Description: "Test automatic pause when budget exceeded",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim and set as executing
	if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}
	exec.interruptMgr.SetCurrentIssue(issue)

	// Progress to executing state
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		t.Fatalf("Failed to update to claimed: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}

	// Simulate budget exceeded - budget monitor would call this
	// In production, this would be triggered by cost tracking
	exec.interruptMgr.RequestInterrupt()

	// Verify interrupt flag is set
	if !exec.interruptMgr.IsInterruptRequested() {
		t.Fatal("Interrupt flag should be set for budget exceeded")
	}

	// Save interrupt context with budget-specific reason
	reason := "Budget exceeded: $10.00 spent, limit is $8.00"
	if err := exec.interruptMgr.SaveInterruptContext(ctx, issue, "budget-monitor", reason, "executing"); err != nil {
		t.Fatalf("Failed to save interrupt context: %v", err)
	}

	// Release and reopen
	if err := store.ReleaseIssueAndReopen(ctx, issue.ID, "executor", "Paused due to budget exceeded"); err != nil {
		t.Fatalf("Failed to release and reopen: %v", err)
	}

	// Verify interrupt metadata
	metadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if metadata == nil {
		t.Fatal("Expected interrupt metadata for budget pause")
	}
	if metadata.InterruptedBy != "budget-monitor" {
		t.Errorf("Expected interrupted_by 'budget-monitor', got %s", metadata.InterruptedBy)
	}
	if metadata.Reason != reason {
		t.Errorf("Expected reason '%s', got %s", reason, metadata.Reason)
	}

	// Verify issue is open and has interrupted label
	pausedIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if pausedIssue.Status != types.StatusOpen {
		t.Errorf("Expected status open after budget pause, got %s", pausedIssue.Status)
	}

	// Verify resume context includes budget information
	exec.interruptMgr.ClearInterrupt()
	exec.interruptMgr.SetCurrentIssue(nil)

	// Later, when budget is increased, the task can be resumed
	if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to reclaim issue: %v", err)
	}

	resumeContext, err := exec.interruptMgr.CheckAndLoadInterruptContext(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to load resume context: %v", err)
	}
	if !contains(resumeContext, "Budget exceeded") {
		t.Error("Resume context should mention budget exceeded")
	}

	t.Log("Budget-triggered pause test completed successfully")
}

// TestExecutorRestartWithInterruptedIssue tests that interrupt metadata persists across executor restarts
func TestExecutorRestartWithInterruptedIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	// Phase 1: First executor pauses a task
	exec1 := setupTestExecutor(t, store)

	issue := &types.Issue{
		ID:          "vc-pause-restart",
		Title:       "Executor restart test",
		Description: "Test metadata persists across executor restarts",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim and execute
	if err := store.ClaimIssue(ctx, issue.ID, exec1.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}
	exec1.interruptMgr.SetCurrentIssue(issue)

	// Progress to executing state
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		t.Fatalf("Failed to update to claimed: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}

	// Pause the task
	exec1.interruptMgr.RequestInterrupt()
	if err := exec1.interruptMgr.SaveInterruptContext(ctx, issue, "user", "testing restart", "executing"); err != nil {
		t.Fatalf("Failed to save interrupt context: %v", err)
	}
	if err := store.ReleaseIssueAndReopen(ctx, issue.ID, "executor", "Paused"); err != nil {
		t.Fatalf("Failed to release and reopen: %v", err)
	}

	// Verify metadata exists
	metadataBeforeRestart, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get metadata before restart: %v", err)
	}
	if metadataBeforeRestart == nil {
		t.Fatal("Expected metadata before restart")
	}

	// Simulate executor shutdown by marking instance as stopped
	if err := store.MarkInstanceStopped(ctx, exec1.instanceID); err != nil {
		t.Fatalf("Failed to mark instance as stopped: %v", err)
	}

	// Phase 2: New executor instance starts and finds the interrupted issue
	exec2 := setupTestExecutor(t, store)

	// Verify interrupt metadata still exists after "restart"
	metadataAfterRestart, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get metadata after restart: %v", err)
	}
	if metadataAfterRestart == nil {
		t.Fatal("Expected metadata to persist after executor restart")
	}

	// Verify metadata fields match
	if metadataAfterRestart.IssueID != metadataBeforeRestart.IssueID {
		t.Error("Issue ID should match after restart")
	}
	if metadataAfterRestart.Reason != metadataBeforeRestart.Reason {
		t.Error("Reason should match after restart")
	}
	if metadataAfterRestart.ExecutionState != metadataBeforeRestart.ExecutionState {
		t.Error("Execution state should match after restart")
	}

	// New executor claims and resumes the interrupted issue
	if err := store.ClaimIssue(ctx, issue.ID, exec2.instanceID); err != nil {
		t.Fatalf("Failed to claim issue with new executor: %v", err)
	}

	resumeContext, err := exec2.interruptMgr.CheckAndLoadInterruptContext(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to load resume context: %v", err)
	}
	if resumeContext == "" {
		t.Error("Expected resume context after executor restart")
	}

	// Verify resume count was incremented
	metadataAfterResume, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get metadata after resume: %v", err)
	}
	if metadataAfterResume.ResumeCount != 1 {
		t.Errorf("Expected resume count 1, got %d", metadataAfterResume.ResumeCount)
	}

	// Complete the task
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		t.Fatalf("Failed to update to claimed: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
		t.Fatalf("Failed to update to analyzing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
		t.Fatalf("Failed to update to gates: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCommitting); err != nil {
		t.Fatalf("Failed to update to committing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCompleted); err != nil {
		t.Fatalf("Failed to complete: %v", err)
	}

	if err := store.CloseIssue(ctx, issue.ID, "completed after restart and resume", exec2.instanceID); err != nil {
		t.Fatalf("Failed to close issue: %v", err)
	}

	// Verify final state
	finalIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final issue: %v", err)
	}
	if finalIssue.Status != types.StatusClosed {
		t.Errorf("Expected final status closed, got %s", finalIssue.Status)
	}

	t.Log("Executor restart with interrupted issue test completed successfully")
}

// TestInterruptCheckpointTiming tests that interrupts are checked at proper checkpoints
func TestInterruptCheckpointTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Test that interrupt flag can be checked at any time
	issue := &types.Issue{
		ID:          "vc-pause-checkpoint",
		Title:       "Checkpoint timing test",
		Description: "Test interrupt checkpoints",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Set as current and request interrupt before any execution
	exec.interruptMgr.SetCurrentIssue(issue)
	exec.interruptMgr.RequestInterrupt()

	// Verify we can check interrupt flag
	if !exec.interruptMgr.IsInterruptRequested() {
		t.Error("Interrupt flag should be set")
	}

	// Verify we can save context even before claiming
	// This tests that the checkpoint mechanism doesn't depend on execution state
	metadata := &types.InterruptMetadata{
		IssueID:            issue.ID,
		InterruptedAt:      time.Now(),
		InterruptedBy:      "test",
		Reason:             "checkpoint timing test",
		ExecutorInstanceID: exec.instanceID,
		ExecutionState:     "before_claim",
		ResumeCount:        0,
	}
	if err := store.SaveInterruptMetadata(ctx, metadata); err != nil {
		t.Fatalf("Failed to save interrupt metadata at early checkpoint: %v", err)
	}

	// Verify metadata was saved
	savedMetadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if savedMetadata == nil {
		t.Fatal("Expected metadata to be saved at early checkpoint")
	}
	if savedMetadata.ExecutionState != "before_claim" {
		t.Errorf("Expected execution state 'before_claim', got %s", savedMetadata.ExecutionState)
	}

	// Clear interrupt
	exec.interruptMgr.ClearInterrupt()
	if exec.interruptMgr.IsInterruptRequested() {
		t.Error("Interrupt flag should be cleared")
	}

	t.Log("Checkpoint timing test completed successfully")
}

// TestSocketCommunicationPause tests pause/resume via control socket RPC
func TestSocketCommunicationPause(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer func() { _ = store.Close() }()

	exec := setupTestExecutor(t, store)

	// Create a test issue
	issue := &types.Issue{
		ID:          "vc-pause-socket",
		Title:       "Socket communication test",
		Description: "Test pause via control socket",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim and set as executing
	if err := store.ClaimIssue(ctx, issue.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}
	exec.interruptMgr.SetCurrentIssue(issue)

	// Progress to executing state
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		t.Fatalf("Failed to update to claimed: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}

	// Note: Full socket communication test would require:
	// 1. Starting a control server with exec.StartControlServer()
	// 2. Creating a control.Client
	// 3. Sending pause command via client.Pause()
	// 4. Verifying the response
	//
	// For this integration test, we'll verify the handler function directly
	// since the socket communication is tested in the control package

	// Simulate what the control server would call when receiving a pause command
	resp, err := exec.interruptMgr.HandlePauseCommand(ctx, issue.ID, "socket pause test")
	if err != nil {
		t.Fatalf("HandlePauseCommand failed: %v", err)
	}

	// Verify response structure (what would be sent over socket)
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
	if resp["status"] != "interrupt_requested" {
		t.Errorf("Expected status 'interrupt_requested', got %v", resp["status"])
	}
	if resp["issue_id"] != issue.ID {
		t.Errorf("Expected issue_id %s, got %v", issue.ID, resp["issue_id"])
	}
	if resp["reason"] != "socket pause test" {
		t.Errorf("Expected reason 'socket pause test', got %v", resp["reason"])
	}

	// Verify interrupt flag was set
	if !exec.interruptMgr.IsInterruptRequested() {
		t.Error("Interrupt flag should be set after pause command")
	}

	// Save interrupt context (would happen at next checkpoint)
	if err := exec.interruptMgr.SaveInterruptContext(ctx, issue, "control-cli", "socket pause test", "executing"); err != nil {
		t.Fatalf("Failed to save interrupt context: %v", err)
	}

	// Verify metadata was saved
	metadata, err := store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get interrupt metadata: %v", err)
	}
	if metadata == nil {
		t.Fatal("Expected interrupt metadata after socket pause")
	}
	if metadata.Reason != "socket pause test" {
		t.Errorf("Expected reason 'socket pause test', got %s", metadata.Reason)
	}

	// Test error case: pause non-executing issue via socket
	exec.interruptMgr.SetCurrentIssue(nil)
	exec.interruptMgr.ClearInterrupt()

	_, err = exec.interruptMgr.HandlePauseCommand(ctx, issue.ID, "should fail")
	if err == nil {
		t.Fatal("Expected error when pausing non-executing issue via socket")
	}
	expectedError := "no task currently executing"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got: %v", expectedError, err)
	}

	// Test error case: pause wrong issue via socket
	otherIssue := &types.Issue{
		ID:          "vc-pause-other",
		Title:       "Other issue",
		Description: "Not executing",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Test criteria",
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, otherIssue, "test"); err != nil {
		t.Fatalf("Failed to create other issue: %v", err)
	}

	exec.interruptMgr.SetCurrentIssue(issue)
	_, err = exec.interruptMgr.HandlePauseCommand(ctx, otherIssue.ID, "should fail")
	if err == nil {
		t.Fatal("Expected error when pausing wrong issue via socket")
	}
	if !contains(err.Error(), "not currently executing") {
		t.Errorf("Expected 'not currently executing' error, got: %v", err)
	}

	t.Log("Socket communication pause test completed successfully")
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
