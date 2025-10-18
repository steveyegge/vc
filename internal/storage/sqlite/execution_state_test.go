package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

func TestClaimIssue(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Create an executor instance
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Create an issue
	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Verify execution state was created
	state, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state == nil {
		t.Fatal("Expected execution state to exist")
	}

	if state.IssueID != issue.ID {
		t.Errorf("Issue ID mismatch: got %s, want %s", state.IssueID, issue.ID)
	}
	if state.ExecutorInstanceID != executor.InstanceID {
		t.Errorf("Executor ID mismatch: got %s, want %s", state.ExecutorInstanceID, executor.InstanceID)
	}
	if state.State != types.ExecutionStateClaimed {
		t.Errorf("State mismatch: got %s, want %s", state.State, types.ExecutionStateClaimed)
	}

	// Verify issue status was updated to in_progress
	updatedIssue, err := db.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if updatedIssue.Status != types.StatusInProgress {
		t.Errorf("Issue status not updated: got %s, want %s", updatedIssue.Status, types.StatusInProgress)
	}
}

func TestClaimIssueDoubleClaim(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Create two executor instances
	executor1 := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host-1",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor1)
	if err != nil {
		t.Fatalf("Failed to register executor1: %v", err)
	}

	executor2 := &types.ExecutorInstance{
		InstanceID:    "executor-2",
		Hostname:      "test-host-2",
		PID:           67890,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err = db.RegisterInstance(ctx, executor2)
	if err != nil {
		t.Fatalf("Failed to register executor2: %v", err)
	}

	// Create an issue
	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// First executor claims the issue
	err = db.ClaimIssue(ctx, issue.ID, executor1.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Second executor tries to claim the same issue - should fail
	err = db.ClaimIssue(ctx, issue.ID, executor2.InstanceID)
	if err == nil {
		t.Error("Expected error when claiming already-claimed issue")
	}
	if !strings.Contains(err.Error(), "already claimed") {
		t.Errorf("Expected 'already claimed' error, got: %v", err)
	}

	// Verify first executor still has the claim
	state, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.ExecutorInstanceID != executor1.InstanceID {
		t.Errorf("Wrong executor claimed issue: got %s, want %s", state.ExecutorInstanceID, executor1.InstanceID)
	}
}

func TestClaimIssueNonExistent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Create an executor instance
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Try to claim non-existent issue
	err = db.ClaimIssue(ctx, "non-existent-issue", executor.InstanceID)
	if err == nil {
		t.Error("Expected error when claiming non-existent issue")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestClaimIssueNonOpenStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Create an executor instance
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Create a closed issue
	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusClosed,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Try to claim closed issue - should fail
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err == nil {
		t.Error("Expected error when claiming closed issue")
	}
	if !strings.Contains(err.Error(), "not open") {
		t.Errorf("Expected 'not open' error, got: %v", err)
	}
}

func TestUpdateExecutionState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Setup executor and issue
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Update state to assessing
	err = db.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing)
	if err != nil {
		t.Fatalf("Failed to update state: %v", err)
	}

	// Verify state was updated
	state, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateAssessing {
		t.Errorf("State not updated: got %s, want %s", state.State, types.ExecutionStateAssessing)
	}
}

func TestUpdateExecutionStateInvalidTransition(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Setup executor and issue
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue (state: claimed)
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Try to skip to executing (should fail - must go through assessing)
	err = db.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting)
	if err == nil {
		t.Error("Expected error for invalid state transition")
	}
	if !strings.Contains(err.Error(), "invalid state transition") {
		t.Errorf("Expected 'invalid state transition' error, got: %v", err)
	}

	// Verify state didn't change
	state, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateClaimed {
		t.Errorf("State changed unexpectedly: got %s, want %s", state.State, types.ExecutionStateClaimed)
	}
}

func TestSaveAndGetCheckpoint(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Setup executor and issue
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Save checkpoint data
	checkpointData := map[string]interface{}{
		"step":       3,
		"last_file":  "main.go",
		"files_done": []string{"a.go", "b.go", "c.go"},
	}
	err = db.SaveCheckpoint(ctx, issue.ID, checkpointData)
	if err != nil {
		t.Fatalf("Failed to save checkpoint: %v", err)
	}

	// Retrieve checkpoint data
	retrievedData, err := db.GetCheckpoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get checkpoint: %v", err)
	}

	// Verify checkpoint data contains expected values
	if !strings.Contains(retrievedData, `"step":3`) {
		t.Errorf("Checkpoint data doesn't contain expected step: %s", retrievedData)
	}
	if !strings.Contains(retrievedData, `"last_file":"main.go"`) {
		t.Errorf("Checkpoint data doesn't contain expected last_file: %s", retrievedData)
	}
}

func TestReleaseIssue(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Setup executor and issue
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Release the issue
	err = db.ReleaseIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to release issue: %v", err)
	}

	// Verify execution state was removed
	state, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state != nil {
		t.Error("Expected execution state to be removed")
	}

	// Verify issue can be claimed again
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err == nil {
		t.Error("Expected error when claiming non-open issue (status should still be in_progress)")
	}
}

func TestReleaseIssueAndReopen(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Setup executor and issue
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Verify issue is in_progress
	retrievedIssue, err := db.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if retrievedIssue.Status != types.StatusInProgress {
		t.Errorf("Expected status in_progress, got %s", retrievedIssue.Status)
	}

	// Release and reopen the issue
	errorComment := "Test error: agent execution failed"
	err = db.ReleaseIssueAndReopen(ctx, issue.ID, executor.InstanceID, errorComment)
	if err != nil {
		t.Fatalf("Failed to release and reopen issue: %v", err)
	}

	// Verify execution state was removed
	state, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state != nil {
		t.Error("Expected execution state to be removed")
	}

	// Verify issue status was reset to open
	retrievedIssue, err = db.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if retrievedIssue.Status != types.StatusOpen {
		t.Errorf("Expected status open, got %s", retrievedIssue.Status)
	}

	// Verify error comment was added
	events, err := db.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	foundErrorComment := false
	foundStatusChange := false
	for _, event := range events {
		if event.EventType == types.EventCommented && event.Comment != nil && *event.Comment == errorComment {
			foundErrorComment = true
		}
		if event.EventType == types.EventStatusChanged && event.Comment != nil && *event.Comment == "Issue released due to error and reopened for retry" {
			foundStatusChange = true
		}
	}
	if !foundErrorComment {
		t.Error("Expected error comment to be added")
	}
	if !foundStatusChange {
		t.Error("Expected status change event to be recorded")
	}

	// Verify issue can be claimed again
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Errorf("Expected to be able to claim issue again, got error: %v", err)
	}
}

func TestStateTransitionFlow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	// Setup executor and issue
	executor := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}
	err := db.RegisterInstance(ctx, executor)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	issue := &types.Issue{
		ID:          "test-issue-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	err = db.ClaimIssue(ctx, issue.ID, executor.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Test valid state transitions: claimed -> assessing -> executing -> analyzing -> gates -> completed
	transitions := []types.ExecutionState{
		types.ExecutionStateAssessing,
		types.ExecutionStateExecuting,
		types.ExecutionStateAnalyzing,
		types.ExecutionStateGates,
		types.ExecutionStateCompleted,
	}

	for _, nextState := range transitions {
		err = db.UpdateExecutionState(ctx, issue.ID, nextState)
		if err != nil {
			t.Fatalf("Failed to transition to %s: %v", nextState, err)
		}

		// Verify state was updated
		state, err := db.GetExecutionState(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get execution state: %v", err)
		}
		if state.State != nextState {
			t.Errorf("State not updated to %s: got %s", nextState, state.State)
		}
	}
}
